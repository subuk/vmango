package hardware

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"
	"unicode/utf8"
)

// Only descriptive constants belong in shared profiles; identity belongs to the VM.
type BIOS struct {
	Vendor  string `json:"vendor,omitempty" hcl:"vendor"`
	Version string `json:"version,omitempty" hcl:"version"`
	Date    string `json:"date,omitempty" hcl:"date"`
	Release string `json:"release,omitempty" hcl:"release"`
}
type SystemInfo struct {
	Manufacturer string `json:"manufacturer,omitempty" hcl:"manufacturer"`
	Product      string `json:"product,omitempty" hcl:"product"`
	Version      string `json:"version,omitempty" hcl:"version"`
	SKU          string `json:"sku,omitempty" hcl:"sku"`
	Family       string `json:"family,omitempty" hcl:"family"`
}
type Baseboard struct {
	Manufacturer string `json:"manufacturer,omitempty" hcl:"manufacturer"`
	Product      string `json:"product,omitempty" hcl:"product"`
	Version      string `json:"version,omitempty" hcl:"version"`
}
type SMBIOS struct {
	BIOS      *BIOS       `json:"bios,omitempty" hcl:"bios"`
	System    *SystemInfo `json:"system,omitempty" hcl:"system"`
	Baseboard *Baseboard  `json:"baseboard,omitempty" hcl:"baseboard"`
}
type Clock struct {
	Mode         string `json:"mode"`
	Timezone     string `json:"timezone,omitempty"`
	ModeExplicit bool   `json:"mode_explicit"`
}
type Timer struct {
	Name       string `json:"name" hcl:",key"`
	TickPolicy string `json:"tickpolicy,omitempty" hcl:"tickpolicy"`
	Present    *bool  `json:"present,omitempty" hcl:"present"`
}

var biosDate = regexp.MustCompile(`^\d{2}/\d{2}/(\d{2}|\d{4})$`)
var biosRelease = regexp.MustCompile(`^\d{1,3}\.\d{1,3}$`)

func NormalizeGuestSystem(p Profile) (Profile, error) {
	fail := func(field, reason string) (Profile, error) {
		return p, fmt.Errorf("hardware profile %q: %s %s", p.ID, field, reason)
	}
	if p.SMBIOS != nil {
		// Clone all optional blocks so accepted plans cannot share mutable catalog data.
		s := *p.SMBIOS
		p.SMBIOS = &s
		values := map[string]string{}
		if s.BIOS != nil {
			b := *s.BIOS
			s.BIOS = &b
			values["bios.vendor"] = b.Vendor
			values["bios.version"] = b.Version
			values["bios.date"] = b.Date
			values["bios.release"] = b.Release
			if b.Date != "" {
				date := b.Date
				if len(date) == 8 {
					date = date[:6] + "19" + date[6:]
				}
				if !biosDate.MatchString(b.Date) {
					return fail("smbios.bios.date", "must use mm/dd/yy or mm/dd/yyyy")
				}
				if _, err := time.Parse("01/02/2006", date); err != nil {
					return fail("smbios.bios.date", "must be a valid calendar date")
				}
			}
			if b.Release != "" {
				if !biosRelease.MatchString(b.Release) {
					return fail("smbios.bios.release", "must use major.minor (0..255 each)")
				}
				for _, v := range strings.Split(b.Release, ".") {
					n, _ := strconv.Atoi(v)
					if n > 255 {
						return fail("smbios.bios.release", "components must be in 0..255")
					}
				}
			}
		}
		if s.System != nil {
			b := *s.System
			s.System = &b
			values["system.manufacturer"] = b.Manufacturer
			values["system.product"] = b.Product
			values["system.version"] = b.Version
			values["system.sku"] = b.SKU
			values["system.family"] = b.Family
		}
		if s.Baseboard != nil {
			b := *s.Baseboard
			s.Baseboard = &b
			values["baseboard.manufacturer"] = b.Manufacturer
			values["baseboard.product"] = b.Product
			values["baseboard.version"] = b.Version
		}
		for field, v := range values {
			if len(v) > 255 || !utf8.ValidString(v) || strings.ContainsAny(v, "\ufffe\uffff") || strings.IndexFunc(v, unicode.IsControl) >= 0 {
				return fail("smbios."+field, "must contain at most 255 bytes and no control characters")
			}
		}
	}
	if p.Clock != nil {
		c := *p.Clock
		p.Clock = &c
		if c.Mode == "" {
			if c.ModeExplicit {
				return fail("clock.mode", "must not be empty")
			}
			c.Mode = "utc"
			p.Clock.Mode = c.Mode
		}
		switch c.Mode {
		case "utc", "localtime":
			if c.Timezone != "" {
				return fail("clock.timezone", "requires mode timezone")
			}
		case "timezone":
			if c.Timezone == "" || c.Timezone == "Local" || strings.HasPrefix(c.Timezone, "/") {
				return fail("clock.timezone", "requires an explicit IANA timezone name")
			}
			if _, err := time.LoadLocation(c.Timezone); err != nil {
				return fail("clock.timezone", "must be a valid IANA timezone name")
			}
		default:
			return fail("clock.mode", "must be utc, localtime or timezone")
		}
	}
	if p.Timers != nil {
		p.Timers = append([]Timer{}, p.Timers...)
	}
	seen := map[string]bool{}
	for i, t := range p.Timers {
		if seen[t.Name] {
			return fail("timer."+t.Name, "is declared more than once")
		}
		seen[t.Name] = true
		policies := []string{}
		switch t.Name {
		case "rtc":
			policies = []string{"delay", "catchup"}
		case "pit":
			policies = []string{"delay", "discard", "catchup"}
		case "armvtimer":
			policies = []string{"delay", "discard"}
		case "hpet", "kvmclock", "hypervclock":
			if t.TickPolicy != "" {
				return fail("timer."+t.Name, "allows present only")
			}
		default:
			return fail("timer."+t.Name, "is unsupported")
		}
		if len(policies) > 0 {
			if t.Present != nil {
				return fail("timer."+t.Name, "does not allow present")
			}
			if t.TickPolicy != "" {
				ok := false
				for _, v := range policies {
					ok = ok || t.TickPolicy == v
				}
				if !ok {
					return fail("timer."+t.Name+".tickpolicy", "is unsupported")
				}
			}
		}
		if t.Present != nil {
			v := *t.Present
			p.Timers[i].Present = &v
		}
	}
	return p, nil
}

func (p Profile) HasGuestSystemConstraints() bool {
	if p.SMBIOS != nil || p.Clock != nil && p.Clock.ModeExplicit {
		return true
	}
	for _, t := range p.Timers {
		if t.TickPolicy != "" || t.Present != nil {
			return true
		}
	}
	return false
}

// This matrix narrows native-command validation to platforms with established
// semantics. The caller must still verify the exact backend's capability flags.
func ValidateGuestSystemPlatform(p Profile, arch string, b PlatformBinding) error {
	if !p.HasGuestSystemConstraints() {
		return nil
	}
	x86 := (arch == "x86_64" || arch == "i686") && (b.Machine == "pc" || b.Machine == "q35" || strings.HasPrefix(b.Machine, "pc-")) && (b.DomainType == "kvm" || b.DomainType == "qemu")
	arm := (arch == "aarch64" || arch == "armv7l") && (b.Machine == "virt" || strings.HasPrefix(b.Machine, "virt-"))
	fail := func(field string) error {
		return fmt.Errorf("hardware profile %s: %s support is unverified for %s/%s/%s; select a supported platform or omit this constraint", p.ID, field, arch, b.Machine, b.DomainType)
	}
	if p.SMBIOS != nil && !x86 {
		return fail("smbios")
	}
	if p.Clock != nil && p.Clock.ModeExplicit && !x86 && !(arm && (b.DomainType == "kvm" || b.DomainType == "hvf" || b.DomainType == "qemu")) {
		return fail("clock")
	}
	for _, t := range p.Timers {
		if t.TickPolicy == "" && t.Present == nil {
			continue
		}
		switch t.Name {
		case "armvtimer":
			if !arm || b.DomainType != "kvm" {
				return fail("timer.armvtimer (requires ARM virt/KVM)")
			}
		case "kvmclock", "hypervclock":
			if !x86 || b.DomainType != "kvm" {
				return fail("timer." + t.Name + " (requires x86/KVM)")
			}
		default:
			if !x86 {
				return fail("timer." + t.Name)
			}
		}
	}
	return nil
}
