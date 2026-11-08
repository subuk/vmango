package hardware

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestGuestSystemCompatibilityAndExplicitness(t *testing.T) {
	old, err := Resolve(Profile{ID: "legacy"}, "/qemu")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(old)
	decoded, err := DecodePlan(raw)
	if err != nil || decoded.Profile.Clock != nil || decoded.Profile.SMBIOS != nil || decoded.Profile.Timers != nil {
		t.Fatalf("old plan reinterpreted: %+v %v", decoded, err)
	}
	clock := &Clock{Mode: "utc"}
	p, err := Normalize(Profile{ID: "new", Clock: clock, Timers: []Timer{{Name: "rtc", TickPolicy: "catchup"}}})
	if err != nil || p.Clock.ModeExplicit {
		t.Fatal(p, err)
	}
	clock.Mode = "localtime"
	if p.Clock.Mode != "utc" {
		t.Fatal("mutable catalog aliased")
	}
}
func TestGuestSystemPolicyValidation(t *testing.T) {
	for _, date := range []string{"02/29/24", "02/29/2000", "12/31/1999"} {
		_, err := Normalize(Profile{ID: "p", SMBIOS: &SMBIOS{BIOS: &BIOS{Date: date}}})
		if err != nil {
			t.Fatal(date, err)
		}
	}
	for _, p := range []Profile{
		{SMBIOS: &SMBIOS{BIOS: &BIOS{Date: "02/29/1900"}}}, {SMBIOS: &SMBIOS{BIOS: &BIOS{Date: "1/01/2026"}}}, {SMBIOS: &SMBIOS{BIOS: &BIOS{Release: "256.0"}}}, {SMBIOS: &SMBIOS{BIOS: &BIOS{Release: "1"}}}, {SMBIOS: &SMBIOS{System: &SystemInfo{Product: "bad\x00value"}}},
		{Clock: &Clock{Mode: "timezone"}}, {Clock: &Clock{Mode: "utc", Timezone: "Europe/Berlin"}}, {Clock: &Clock{Mode: "timezone", Timezone: "Local"}}, {Clock: &Clock{Mode: "timezone", Timezone: "Nowhere/Missing"}}, {Clock: &Clock{Mode: "variable"}},
		{Timers: []Timer{{Name: "rtc", TickPolicy: "discard"}}}, {Timers: []Timer{{Name: "armvtimer", Present: new(false)}}}, {Timers: []Timer{{Name: "hpet", TickPolicy: "delay"}}}, {Timers: []Timer{{Name: "tsc"}}}, {Timers: []Timer{{Name: "pit"}, {Name: "pit"}}},
	} {
		p.ID = "p"
		if _, err := Normalize(p); err == nil {
			t.Fatalf("invalid policy accepted: %+v", p)
		}
	}
	p, err := Normalize(Profile{ID: "p", Clock: &Clock{Mode: "timezone", Timezone: "Europe/Berlin", ModeExplicit: true}})
	if err != nil || p.Clock.Timezone != "Europe/Berlin" {
		t.Fatal(p, err)
	}
}
func TestGuestSystemSupportMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, arch, machine, accel string
		p                          Profile
		valid                      bool
	}{
		{"x86", "x86_64", "pc-q35-8.2", "kvm", Profile{SMBIOS: &SMBIOS{}, Timers: []Timer{{Name: "pit", TickPolicy: "catchup"}}}, true},
		{"arm-kvm", "aarch64", "virt-9.2", "kvm", Profile{Timers: []Timer{{Name: "armvtimer", TickPolicy: "discard"}}}, true},
		{"arm-hvf", "aarch64", "virt-9.2", "hvf", Profile{Timers: []Timer{{Name: "armvtimer", TickPolicy: "delay"}}}, false},
		{"arm-smbios", "aarch64", "virt-9.2", "kvm", Profile{SMBIOS: &SMBIOS{}}, false},
		{"tcg-paravirtual", "x86_64", "pc-q35-9.2", "qemu", Profile{Timers: []Timer{{Name: "kvmclock", Present: new(true)}}}, false},
		{"hvf-clock", "aarch64", "virt-9.2", "hvf", Profile{Clock: &Clock{Mode: "timezone", Timezone: "Europe/Berlin", ModeExplicit: true}}, true},
		{"unverified-machine", "x86_64", "microvm", "kvm", Profile{Timers: []Timer{{Name: "rtc", TickPolicy: "delay"}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateGuestSystemPlatform(tc.p, tc.arch, PlatformBinding{Machine: tc.machine, DomainType: tc.accel})
			if (err == nil) != tc.valid {
				t.Fatal(err)
			}
			if err != nil && !strings.Contains(err.Error(), "unverified") {
				t.Fatal(err)
			}
		})
	}
}
