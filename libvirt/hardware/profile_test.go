package hardware

import (
	"encoding/json"
	"reflect"
	"testing"
)

func enabled(v bool) *bool { return &v }
func TestProfileDefaultsAndIndependentConfigDrive(t *testing.T) {
	p, err := Normalize(Profile{ID: "linux", DiskBus: "virtio-scsi"})
	if err != nil {
		t.Fatal(err)
	}
	if p.DiskBus != "scsi" || p.DiskController != "virtio-scsi" || p.ConfigDrive.Bus != "virtio" || p.ConfigDrive.Format != "nocloud" || !p.ConfigDrive.IsEnabled() || !p.GuestAgentEnabled() {
		t.Fatalf("unexpected defaults: %+v", p)
	}
	raw, err := Encode(p)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePolicy(raw)
	if err != nil || !reflect.DeepEqual(decoded, p) {
		t.Fatalf("roundtrip: %+v %v", decoded, err)
	}
	original := Profile{ID: "x", GuestAgent: enabled(false), ConfigDrive: ConfigDrive{Enabled: enabled(false)}}
	normalized, err := Normalize(original)
	if err != nil {
		t.Fatal(err)
	}
	*original.GuestAgent = true
	*original.ConfigDrive.Enabled = true
	if normalized.GuestAgentEnabled() || normalized.ConfigDrive.IsEnabled() {
		t.Fatal("normalized profile aliases mutable input")
	}
}
func TestProfileRejectsUnsupportedCombinations(t *testing.T) {
	for _, p := range []Profile{{ID: "x", DiskBus: "sata"}, {ID: "x", DiskBus: "virtio", DiskController: "virtio-scsi"}, {ID: "x", Graphics: "none", VideoModel: "qxl"}, {ID: "x", ConfigDrive: ConfigDrive{DeviceType: "cdrom", Bus: "virtio"}}, {ID: "x", ConfigDrive: ConfigDrive{Format: "guess"}}} {
		if _, err := Normalize(p); err == nil {
			t.Fatalf("invalid profile accepted: %+v", p)
		}
	}
	for _, format := range []string{"nocloud", "openstack"} {
		for _, pair := range [][2]string{{"disk", "virtio"}, {"disk", "scsi"}, {"disk", "ide"}, {"cdrom", "scsi"}, {"cdrom", "ide"}} {
			if _, err := Normalize(Profile{ID: "x", ConfigDrive: ConfigDrive{Format: format, DeviceType: pair[0], Bus: pair[1]}}); err != nil {
				t.Fatal(err)
			}
		}
	}
}
func TestDeviceDependenciesAreSharedAndValidated(t *testing.T) {
	plan, err := Resolve(Profile{ID: "linux", DiskBus: "scsi", ConfigDrive: ConfigDrive{Bus: "scsi"}}, "/accepted/qemu")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, d := range plan.Devices {
		if d.ID == "scsi-0" {
			count++
		}
		if d.ID == "guest-agent" && !reflect.DeepEqual(d.Dependencies, []string{"virtio-serial-0"}) {
			t.Fatal("guest-agent dependency missing")
		}
	}
	if count != 1 || plan.Emulator != "/accepted/qemu" || len(plan.State) != 1 || plan.State[0].Owner != "vm" {
		t.Fatalf("plan lost shared dependency or state ownership: %+v", plan)
	}
	for _, devices := range [][]Device{{{ID: "a"}, {ID: "a"}}, {{ID: "a", Dependencies: []string{"missing"}}}, {{ID: "a", Dependencies: []string{"b"}}, {ID: "b", Dependencies: []string{"a"}}}} {
		if err := ValidateDevices(devices); err == nil {
			t.Fatalf("invalid dependency graph accepted: %+v", devices)
		}
	}
}

func TestAppliedPlansNeverSynthesizeMissingPolicy(t *testing.T) {
	if _, err := DecodePlan([]byte(`{"version":1,"profile":{"id":"partial"}}`)); err == nil {
		t.Fatal("partial stored profile synthesized current defaults")
	}
	plan, err := Resolve(Profile{ID: "complete"}, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(plan)
	if _, err = DecodePlan(raw); err != nil {
		t.Fatal(err)
	}
	original := plan.Devices
	plan.Devices = nil
	raw, _ = json.Marshal(plan)
	if _, err = DecodePlan(raw); err == nil {
		t.Fatal("missing dependency graph accepted")
	}
	plan.Devices = original
	plan.Profile.Hugepages = true
	raw, _ = json.Marshal(plan)
	if _, err = DecodePlan(raw); err == nil {
		t.Fatal("missing hugepage binding synthesized at execution")
	}
	plan.HugepageSizeKiB = 2048
	raw, _ = json.Marshal(plan)
	if _, err = DecodePlan(raw); err != nil {
		t.Fatal(err)
	}
	plan.Profile.ConfigDrive.Format = ""
	raw, _ = json.Marshal(plan)
	if _, err = DecodePlan(raw); err == nil {
		t.Fatal("missing configdrive format inferred")
	}
}
