package libvirt

import (
	"encoding/json"
	"fmt"
	x "libvirt.org/go/libvirtxml"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
	"testing"
)

func guestProfile(t *testing.T) hardware.Profile {
	t.Helper()
	p, err := hardware.Normalize(hardware.Profile{ID: "guest", SMBIOS: &hardware.SMBIOS{BIOS: &hardware.BIOS{Vendor: "Vmango", Version: "1", Date: "01/02/2026", Release: "1.2"}, System: &hardware.SystemInfo{Manufacturer: "Vmango", Product: "Guest", Version: "1", SKU: "sku", Family: "cloud"}, Baseboard: &hardware.Baseboard{Manufacturer: "Vmango", Product: "Board", Version: "2"}}, Clock: &hardware.Clock{Mode: "timezone", Timezone: "Europe/Berlin", ModeExplicit: true}, Timers: []hardware.Timer{{Name: "rtc", TickPolicy: "catchup"}, {Name: "pit", TickPolicy: "discard"}, {Name: "hpet", Present: new(false)}, {Name: "kvmclock", Present: new(true)}, {Name: "hypervclock", Present: new(false)}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestGuestSystemCreationAndReadback(t *testing.T) {
	p := guestProfile(t)
	plan, _ := hardware.Resolve(p, "/qemu")
	raw, _ := json.Marshal(plan)
	for _, id := range []string{"dc75d4a7-b289-44c8-b4b2-d96a016329cb", "642672c2-6d92-4307-9863-1886daac5efa"} {
		c := newDomainConfig(&compute.VirtualMachine{Id: id, AppliedPolicy: raw}, &x.DomainCaps{Arch: "x86_64", Domain: "kvm", Machine: "pc-q35-8.2", Path: "/qemu"})
		xml, _ := c.Marshal()
		var read x.Domain
		if err := read.Unmarshal(xml); err != nil {
			t.Fatal(err)
		}
		observed := observeGuestSystem(&read)
		if observed.SystemUUID != id || observed.SMBIOSMode != "sysinfo" || !reflect.DeepEqual(observed.SMBIOS, p.SMBIOS) || observed.Clock.Timezone != "Europe/Berlin" || len(observed.Clock.Timers) != 5 {
			t.Fatalf("readback lost policy: %+v", observed)
		}
		if err := validateGuestSystemAdoption(&read, p); err != nil {
			t.Fatal(err)
		}
	}
	empty := newDomainConfig(&compute.VirtualMachine{Id: "legacy"}, &x.DomainCaps{Arch: "x86_64"})
	if len(empty.SysInfo) != 0 || empty.OS.SMBios != nil || empty.Clock.Offset != "utc" || len(empty.Clock.Timer) != 0 {
		t.Fatal("omission introduced preset")
	}
}
func TestGuestSystemAdoptionConstraintsAndPreservation(t *testing.T) {
	const original = `<domain type="kvm"><uuid>dc75d4a7-b289-44c8-b4b2-d96a016329cb</uuid><os><type arch="x86_64">hvm</type><smbios mode="sysinfo"/></os><sysinfo type="smbios"><system><entry name="product">Original</entry><entry name="serial">private-serial</entry><entry name="uuid">dc75d4a7-b289-44c8-b4b2-d96a016329cb</entry></system><chassis><entry name="asset">original</entry></chassis><oemStrings><entry>original-oem</entry></oemStrings></sysinfo><clock offset="variable" basis="localtime" adjustment="3600"><timer name="tsc" mode="native" frequency="12345"/><timer name="rtc" track="wall" tickpolicy="catchup"><catchup threshold="10" slew="20" limit="30"/></timer></clock><devices/></domain>`
	var c x.Domain
	if err := c.Unmarshal(original); err != nil {
		t.Fatal(err)
	}
	before, _ := c.Marshal()
	if err := validateGuestSystemAdoption(&c, hardware.Profile{ID: "p", Clock: &hardware.Clock{Mode: "utc"}}); err != nil {
		t.Fatal("omitted mode must preserve", err)
	}
	if err := validateGuestSystemAdoption(&c, hardware.Profile{ID: "p", SMBIOS: &hardware.SMBIOS{System: &hardware.SystemInfo{Product: "Original"}}, Timers: []hardware.Timer{{Name: "rtc", TickPolicy: "catchup"}}}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []hardware.Profile{{Clock: &hardware.Clock{Mode: "utc", ModeExplicit: true}}, {SMBIOS: &hardware.SMBIOS{System: &hardware.SystemInfo{Product: "changed"}}}, {Timers: []hardware.Timer{{Name: "hpet", Present: new(false)}}}, {Timers: []hardware.Timer{{Name: "rtc", TickPolicy: "delay"}}}} {
		if err := validateGuestSystemAdoption(&c, p); err == nil {
			t.Fatalf("mismatch accepted: %+v", p)
		}
	}
	after, _ := c.Marshal()
	if before != after {
		t.Fatal("validation mutated XML")
	}
	var saved x.Domain
	_ = saved.Unmarshal(before)
	applyDomainPolicy(&c, &compute.VirtualMachine{VCpus: 2, Memory: compute.NewSize(2, compute.SizeUnitG)}, &x.Caps{}, true)
	if !reflect.DeepEqual(c.SysInfo, saved.SysInfo) || !reflect.DeepEqual(c.Clock, saved.Clock) || c.UUID != saved.UUID || !reflect.DeepEqual(c.OS.SMBios, saved.OS.SMBios) {
		t.Fatal("resource change reset unrelated system information")
	}
}
func TestAdoptionDoesNotInferMissingTimerAttributes(t *testing.T) {
	var c x.Domain
	_ = c.Unmarshal(`<domain><clock offset="utc"><timer name="hpet"/><timer name="rtc"/></clock></domain>`)
	for _, timer := range []hardware.Timer{{Name: "hpet", Present: new(true)}, {Name: "hpet", Present: new(false)}, {Name: "rtc", TickPolicy: "delay"}} {
		if err := validateGuestSystemAdoption(&c, hardware.Profile{Timers: []hardware.Timer{timer}}); err == nil {
			t.Fatal("missing XML attribute guessed", timer)
		}
	}
}
func TestGuestSystemCapabilityValidationIsRequired(t *testing.T) {
	p := guestProfile(t)
	caps := &x.DomainCaps{Arch: "x86_64", Domain: "kvm", Machine: "pc-q35-8.2", Path: "/exact/qemu"}
	calls := 0
	validate := func(format, raw string, flags uint32) (string, error) {
		calls++
		var c x.Domain
		if err := c.Unmarshal(raw); err != nil {
			t.Fatal(err)
		}
		if format != "qemu-argv" || c.Devices.Emulator != "/exact/qemu" || c.OS.Type.Machine != caps.Machine || c.Type != "kvm" || len(c.Devices.Disks) > 0 || len(c.Devices.Interfaces) > 0 {
			t.Fatal("wrong capability context", raw)
		}
		return "", fmt.Errorf("backend lacks required timer capability")
	}
	if err := validateGuestSystemNative(p, caps, validate); err == nil || !strings.Contains(err.Error(), "unverified") {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	caps.Arch = "aarch64"
	caps.Domain = "hvf"
	caps.Machine = "virt-9.2"
	p.SMBIOS = nil
	p.Timers = []hardware.Timer{{Name: "armvtimer", TickPolicy: "delay"}}
	if err := validateGuestSystemNative(p, caps, validate); err == nil || calls != 1 {
		t.Fatal("HVF timer requirement not rejected before backend conversion", err)
	}
	if err := validateGuestSystemNative(hardware.Profile{ID: "old"}, caps, validate); err != nil || calls != 1 {
		t.Fatal("old policy acquired new requirement", err)
	}
}
