package hardware

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMachineDefaultsExplicitnessAndOldPayloads(t *testing.T) {
	p, err := Normalize(Profile{ID: "new", Machine: &Machine{}})
	if err != nil || p.Machine.Type != "auto" || p.Machine.Acceleration != "hardware" || p.Machine.TypeExplicit || p.Machine.AccelerationExplicit {
		t.Fatalf("defaults: %+v %v", p.Machine, err)
	}
	for _, acceleration := range []string{"hardware", "kvm", "hvf", "qemu"} {
		m, err := NormalizeMachine(Machine{Type: "pc-q35-9.2", Acceleration: acceleration, TypeExplicit: true, AccelerationExplicit: true})
		if err != nil || !m.TypeExplicit || !m.AccelerationExplicit {
			t.Fatal(m, err)
		}
	}
	if _, err := NormalizeMachine(Machine{Acceleration: "auto"}); err == nil {
		t.Fatal("acceleration auto accepted")
	}
	old, _ := Resolve(Profile{ID: "old"}, "/saved/qemu")
	old.Machine, old.DomainType = "pc-i440fx-8.2", "qemu"
	raw, _ := json.Marshal(old)
	restored, err := DecodePlan(raw)
	if err != nil || restored.Profile.Machine != nil || !reflect.DeepEqual(old, restored) {
		t.Fatalf("old plan reinterpreted: %+v %v", restored, err)
	}
	policy, _ := Encode(Profile{ID: "old"})
	decoded, err := DecodePolicy(policy)
	if err != nil || decoded.Machine != nil {
		t.Fatal("legacy policy gained new defaults", err)
	}
}
func TestAdoptionMachineConstraints(t *testing.T) {
	existing := PlatformBinding{Machine: "pc-q35-8.2", DomainType: "qemu", Emulator: "/saved/qemu"}
	for _, policy := range []*Machine{nil, {Type: "auto", Acceleration: "hardware"}, {Type: "auto", TypeExplicit: true, Acceleration: "hardware"}} {
		if err := ValidateAdoptionMachine(policy, existing, "pc-q35-9.2", ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, policy := range []*Machine{
		{Type: "q35", TypeExplicit: true},
		{Type: "auto", Acceleration: "hardware", AccelerationExplicit: true},
		{Type: "auto", Acceleration: "kvm", AccelerationExplicit: true},
		{Type: "auto", Acceleration: "hvf", AccelerationExplicit: true},
	} {
		if err := ValidateAdoptionMachine(policy, existing, "pc-q35-9.2", ""); err == nil {
			t.Fatalf("adoption weakened %+v", policy)
		}
	}
	exact := &Machine{Type: existing.Machine, TypeExplicit: true, Acceleration: "qemu", AccelerationExplicit: true}
	if err := ValidateAdoptionMachine(exact, existing, existing.Machine, existing.Emulator); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAdoptionMachine(exact, existing, existing.Machine, "/replacement"); err == nil {
		t.Fatal("emulator replaced")
	}
}
func TestSavedPlatformNeverSubstitutes(t *testing.T) {
	saved := PlatformBinding{"pc-q35-8.2", "kvm", "/saved/qemu"}
	for _, observed := range []PlatformBinding{{"pc-q35-9.2", "kvm", "/saved/qemu"}, {"pc-q35-8.2", "qemu", "/saved/qemu"}, {"pc-q35-8.2", "kvm", "/new/qemu"}} {
		if ValidateSavedPlatform(saved, observed) == nil {
			t.Fatal("saved binding substituted", observed)
		}
	}
	if ValidateSavedPlatform(saved, saved) != nil {
		t.Fatal("identical binding rejected")
	}
}
func TestCanonicalMachineUsesMappingsOnly(t *testing.T) {
	for _, target := range []string{"pc-q35-9.2", "pc-q35-8.2"} {
		got, err := CanonicalMachine("q35", map[string]string{"q35": target, "pc-q35-99.0": ""})
		if err != nil || got != target {
			t.Fatal(got, err)
		}
	}
	if got, err := CanonicalMachine("unversioned", nil); err != nil || got != "unversioned" {
		t.Fatal(got, err)
	}
	if _, err := CanonicalMachine("auto", nil); err == nil {
		t.Fatal("unresolved default accepted")
	}
	if _, err := CanonicalMachine("a", map[string]string{"a": "b", "b": "a"}); err == nil {
		t.Fatal("cyclic mapping accepted")
	}
}
