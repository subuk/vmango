package hardware

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCPUPolicyDefaultsAndLegacyPlans(t *testing.T) {
	p, err := Normalize(Profile{ID: "cpu", CPU: &CPU{}, Placement: &NativePlacement{}, HyperV: &HyperV{Spinlocks: &Spinlocks{}, STimer: &STimer{Enabled: boolValue(false)}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.CPU.Mode != "maximum" || p.CPU.ModeExplicit || p.Placement.Mode != "static" || p.HyperV.Mode != "custom" || *p.HyperV.Spinlocks.Retries != 4096 {
		t.Fatalf("defaults: %+v", p)
	}
	old, err := Resolve(Profile{ID: "old"}, "/qemu")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(old)
	got, err := DecodePlan(raw)
	if err != nil || got.Profile.CPU != nil || got.SimpleCPUTopology {
		t.Fatalf("legacy rewritten: %+v %v", got, err)
	}
}
func boolValue(v bool) *bool { return &v }
func TestCPUValidationContracts(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    Profile
	}{
		{"mode", Profile{CPU: &CPU{Mode: "auto"}}},
		{"custom model", Profile{CPU: &CPU{Mode: "custom"}}},
		{"model ownership", Profile{CPU: &CPU{Mode: "maximum", Model: "host"}}},
		{"vendor ownership", Profile{CPU: &CPU{Mode: "host-model", Vendor: "Intel"}}},
		{"feature force", Profile{CPU: &CPU{Features: []CPUFeature{{Name: "aes", Policy: "force"}}}}},
		{"duplicate feature", Profile{CPU: &CPU{Features: []CPUFeature{{Name: "aes"}, {Name: "aes"}}}}},
		{"qemu passthrough", Profile{CPU: &CPU{Features: []CPUFeature{{Name: "aes=on"}}}}},
		{"static memory", Profile{Placement: &NativePlacement{MemoryMode: "strict"}}},
		{"auto memory", Profile{Placement: &NativePlacement{Mode: "auto", MemoryMode: "preferred"}}},
		{"automatic override", Profile{HyperV: &HyperV{Mode: "host-model", Relaxed: boolValue(false)}}},
		{"synic dependency", Profile{HyperV: &HyperV{Synic: boolValue(true)}}},
		{"tlb dependency", Profile{HyperV: &HyperV{TLBFlush: boolValue(true)}}},
		{"ipi dependency", Profile{HyperV: &HyperV{IPI: boolValue(true)}}},
		{"stimer dependency", Profile{HyperV: &HyperV{STimer: &STimer{}}}},
		{"stimer clock", Profile{HyperV: &HyperV{VPIndex: boolValue(true), Synic: boolValue(true), STimer: &STimer{}}}},
		{"disabled direct", Profile{HyperV: &HyperV{STimer: &STimer{Enabled: boolValue(false), Direct: boolValue(false)}}}},
		{"automatic clock", Profile{HyperV: &HyperV{Mode: "passthrough"}, Timers: []Timer{{Name: "hypervclock", Present: boolValue(false)}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.p.ID = "bad"
			if _, err := Normalize(tc.p); err == nil {
				t.Fatal("invalid policy accepted")
			}
		})
	}
	for _, n := range []int64{4094, 4095, 4096, 4294967295, 4294967296} {
		p := Profile{ID: "spin", HyperV: &HyperV{Spinlocks: &Spinlocks{Retries: &n}}}
		_, err := Normalize(p)
		if (err == nil) != (n >= 4095 && n <= 4294967295) {
			t.Fatalf("retries %d: %v", n, err)
		}
		p.HyperV.Spinlocks.Enabled = boolValue(false)
		if _, err = Normalize(p); err == nil {
			t.Fatal("disabled retries ignored")
		}
	}
	p, err := Normalize(Profile{ID: "valid", CPU: &CPU{Mode: "custom", Model: "Future-Backend-Model", Vendor: "Intel", Features: []CPUFeature{{Name: "future-feature"}}}, Placement: &NativePlacement{Mode: "auto"}, HyperV: &HyperV{VPIndex: boolValue(true), Synic: boolValue(true), STimer: &STimer{}}, Timers: []Timer{{Name: "hypervclock", Present: boolValue(true)}}})
	if err != nil || p.CPU.Features[0].Policy != "require" || p.Placement.MemoryMode != "strict" {
		t.Fatalf("declarative backend names rejected: %+v %v", p, err)
	}
	raw, _ := Encode(p)
	if !strings.Contains(string(raw), "future-feature") {
		t.Fatal("requirement lost")
	}
}
