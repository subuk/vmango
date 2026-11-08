package libvirt

import (
	"fmt"
	vir "libvirt.org/go/libvirt"
	x "libvirt.org/go/libvirtxml"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
)

func sysinfoEntries(fields map[string]string) []x.DomainSysInfoEntry {
	keys := []string{"vendor", "manufacturer", "product", "version", "date", "release", "sku", "family"}
	var result []x.DomainSysInfoEntry
	for _, k := range keys {
		if v := fields[k]; v != "" {
			result = append(result, x.DomainSysInfoEntry{Name: k, Value: v})
		}
	}
	return result
}
func biosFields(b *hardware.BIOS) map[string]string {
	if b == nil {
		return nil
	}
	return map[string]string{"vendor": b.Vendor, "version": b.Version, "date": b.Date, "release": b.Release}
}
func systemFields(b *hardware.SystemInfo) map[string]string {
	if b == nil {
		return nil
	}
	return map[string]string{"manufacturer": b.Manufacturer, "product": b.Product, "version": b.Version, "sku": b.SKU, "family": b.Family}
}
func boardFields(b *hardware.Baseboard) map[string]string {
	if b == nil {
		return nil
	}
	return map[string]string{"manufacturer": b.Manufacturer, "product": b.Product, "version": b.Version}
}

// Called only for new domain creation. Existing XML is never reconstructed.
func applyGuestSystem(c *x.Domain, p hardware.Profile) {
	if p.SMBIOS != nil {
		s := &x.DomainSysInfoSMBIOS{}
		if p.SMBIOS.BIOS != nil {
			s.BIOS = &x.DomainSysInfoBIOS{Entry: sysinfoEntries(biosFields(p.SMBIOS.BIOS))}
		}
		s.System = &x.DomainSysInfoSystem{Entry: sysinfoEntries(systemFields(p.SMBIOS.System))}
		s.System.Entry = append(s.System.Entry, x.DomainSysInfoEntry{Name: "uuid", Value: c.UUID})
		if p.SMBIOS.Baseboard != nil {
			s.BaseBoard = []x.DomainSysInfoBaseBoard{{Entry: sysinfoEntries(boardFields(p.SMBIOS.Baseboard))}}
		}
		c.SysInfo = []x.DomainSysInfo{{SMBIOS: s}}
		c.OS.SMBios = &x.DomainSMBios{Mode: "sysinfo"}
	}
	c.Clock = &x.DomainClock{Offset: "utc"}
	if p.Clock != nil {
		c.Clock.Offset = p.Clock.Mode
		c.Clock.TimeZone = p.Clock.Timezone
	}
	for _, t := range p.Timers {
		if t.TickPolicy == "" && t.Present == nil {
			continue
		}
		timer := x.DomainTimer{Name: t.Name, TickPolicy: t.TickPolicy}
		if t.Present != nil {
			timer.Present = map[bool]string{true: "yes", false: "no"}[*t.Present]
		}
		c.Clock.Timer = append(c.Clock.Timer, timer)
	}
}

func observeGuestSystem(c *x.Domain) hardware.GuestSystemObservation {
	o := hardware.GuestSystemObservation{}
	if c.OS != nil && c.OS.SMBios != nil {
		o.SMBIOSMode = c.OS.SMBios.Mode
	}
	for _, info := range c.SysInfo {
		s := info.SMBIOS
		if s == nil {
			continue
		}
		o.SMBIOS = &hardware.SMBIOS{}
		values := func(entries []x.DomainSysInfoEntry) map[string]string {
			r := map[string]string{}
			for _, e := range entries {
				r[e.Name] = e.Value
			}
			return r
		}
		if s.BIOS != nil {
			v := values(s.BIOS.Entry)
			o.SMBIOS.BIOS = &hardware.BIOS{Vendor: v["vendor"], Version: v["version"], Date: v["date"], Release: v["release"]}
		}
		if s.System != nil {
			v := values(s.System.Entry)
			o.SMBIOS.System = &hardware.SystemInfo{Manufacturer: v["manufacturer"], Product: v["product"], Version: v["version"], SKU: v["sku"], Family: v["family"]}
			o.SystemUUID = v["uuid"]
		}
		if len(s.BaseBoard) > 0 {
			v := values(s.BaseBoard[0].Entry)
			o.SMBIOS.Baseboard = &hardware.Baseboard{Manufacturer: v["manufacturer"], Product: v["product"], Version: v["version"]}
		}
	}
	if c.Clock != nil {
		v := c.Clock
		o.Clock = &hardware.ObservedClock{Mode: v.Offset, Timezone: v.TimeZone, Adjustment: v.Adjustment, Basis: v.Basis, Start: v.Start}
		for _, t := range v.Timer {
			ot := hardware.ObservedTimer{Name: t.Name, Present: t.Present, TickPolicy: t.TickPolicy, Track: t.Track, Mode: t.Mode, Frequency: t.Frequency}
			if t.CatchUp != nil {
				ot.CatchUp = &hardware.ObservedCatchUp{Threshold: t.CatchUp.Threshold, Slew: t.CatchUp.Slew, Limit: t.CatchUp.Limit}
			}
			o.Clock.Timers = append(o.Clock.Timers, ot)
		}
	}
	return o
}

func validateGuestSystemAdoption(c *x.Domain, p hardware.Profile) error {
	fail := func(field string) error {
		return &compute.ValidationError{Field: "hardware_profile", Message: fmt.Sprintf("hardware profile %s: existing %s does not establish the explicit requirement; adoption cannot rewrite it", p.ID, field)}
	}
	if p.SMBIOS != nil {
		if c.OS == nil || c.OS.SMBios == nil || c.OS.SMBios.Mode != "sysinfo" {
			return fail("SMBIOS presentation mode")
		}
		var s *x.DomainSysInfoSMBIOS
		for _, info := range c.SysInfo {
			if info.SMBIOS != nil {
				if s != nil {
					return fail("duplicate SMBIOS information")
				}
				s = info.SMBIOS
			}
		}
		if s == nil {
			return fail("SMBIOS information")
		}
		compare := func(kind string, required map[string]string, entries []x.DomainSysInfoEntry) error {
			for k, want := range required {
				if want == "" {
					continue
				}
				got := ""
				count := 0
				for _, entry := range entries {
					if entry.Name == k {
						got = entry.Value
						count++
					}
				}
				// Libvirt treats a two-digit BIOS year as 19yy.
				if kind == "bios" && k == "date" {
					if len(want) == 8 {
						want = want[:6] + "19" + want[6:]
					}
					if len(got) == 8 {
						got = got[:6] + "19" + got[6:]
					}
				}
				if count != 1 || got != want {
					return fail("smbios." + kind + "." + k)
				}
			}
			return nil
		}
		var bios, system, board []x.DomainSysInfoEntry
		if s.BIOS != nil {
			bios = s.BIOS.Entry
		}
		if s.System != nil {
			system = s.System.Entry
		}
		if len(s.BaseBoard) == 1 {
			board = s.BaseBoard[0].Entry
		}
		if err := compare("bios", biosFields(p.SMBIOS.BIOS), bios); err != nil {
			return err
		}
		if err := compare("system", systemFields(p.SMBIOS.System), system); err != nil {
			return err
		}
		if err := compare("baseboard", boardFields(p.SMBIOS.Baseboard), board); err != nil {
			return err
		}
	}
	if p.Clock != nil && p.Clock.ModeExplicit {
		if c.Clock == nil || c.Clock.Offset != p.Clock.Mode || c.Clock.TimeZone != p.Clock.Timezone {
			return fail("clock mode/timezone")
		}
		if c.Clock.Adjustment != "" && c.Clock.Adjustment != "reset" || c.Clock.Basis != "" || c.Clock.Start != 0 {
			return fail("clock adjustment/basis/start")
		}
	}
	for _, wanted := range p.Timers {
		if wanted.TickPolicy == "" && wanted.Present == nil {
			continue
		}
		var found *x.DomainTimer
		if c.Clock != nil {
			for i := range c.Clock.Timer {
				if c.Clock.Timer[i].Name == wanted.Name {
					if found != nil {
						return fail("duplicate timer." + wanted.Name)
					}
					found = &c.Clock.Timer[i]
				}
			}
		}
		if found == nil {
			return fail("timer." + wanted.Name)
		}
		if wanted.TickPolicy != "" && (found.TickPolicy != wanted.TickPolicy || found.Present == "no") {
			return fail("timer." + wanted.Name + ".tickpolicy")
		}
		if wanted.Present != nil && found.Present != map[bool]string{true: "yes", false: "no"}[*wanted.Present] {
			return fail("timer." + wanted.Name + ".present")
		}
	}
	return nil
}

// Native conversion runs the driver's real QEMU capability validation and
// command builder. It never defines/starts a domain or executes returned argv.
func validateGuestSystemCapabilities(conn *vir.Connect, p hardware.Profile, caps *x.DomainCaps) error {
	return validateGuestSystemNative(p, caps, conn.DomainXMLToNative)
}

func validateGuestSystemNative(p hardware.Profile, caps *x.DomainCaps, convert func(string, string, uint32) (string, error)) error {
	if !p.HasGuestSystemConstraints() {
		return nil
	}
	if err := hardware.ValidateGuestSystemPlatform(p, caps.Arch, platformFromCaps(caps)); err != nil {
		return &compute.ValidationError{Field: "hardware_profile", Message: err.Error()}
	}
	c := &x.Domain{Type: caps.Domain, Name: "vmango-capability-validation", UUID: "00000000-0000-4000-8000-000000000001", Memory: &x.DomainMemory{Value: 512, Unit: "MiB"}, VCPU: &x.DomainVCPU{Value: 1}, OS: &x.DomainOS{Type: &x.DomainOSType{Arch: caps.Arch, Machine: caps.Machine, Type: "hvm"}}, Devices: &x.DomainDeviceList{Emulator: caps.Path}, CPU: &x.DomainCPU{Mode: "maximum"}}
	if p.FirmwarePolicy == nil && p.Firmware != "" && p.Firmware != "auto" {
		c.OS.Firmware = p.Firmware
	}
	applyDomainPowerPolicy(c)
	applyGuestSystem(c, p)
	raw, err := c.Marshal()
	if err != nil {
		return err
	}
	_, err = convert("qemu-argv", raw, 0)
	if err != nil {
		return &compute.ValidationError{Field: "hardware_profile", Message: fmt.Sprintf("hardware profile %s: SMBIOS/clock/timer requirement support is unverified for %s/%s/%s emulator %s: native capability validation failed: %v", p.ID, caps.Arch, caps.Machine, caps.Domain, caps.Path, err)}
	}
	return nil
}
