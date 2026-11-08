package config

import (
	"strings"
	"subuk/vmango/libvirt/hardware"
	"testing"
)

func TestHardwareProfilesAreRequiredAndCloudScoped(t *testing.T) {
	for _, ref := range []string{"", "missing"} {
		text := strings.Replace(cloudConfig, `hardware_profile="default"`, `hardware_profile="`+ref+`"`, 1)
		_, err := ParseBytes([]byte(text))
		if err == nil || !strings.Contains(err.Error(), "cloud test image linux") || !strings.Contains(err.Error(), HardwareMigrationGuide) {
			t.Fatalf("missing migration guidance for %q: %v", ref, err)
		}
	}
	text := strings.Replace(cloudConfig, `image "linux"`, `image "linux-two" { hardware_profile="default" path="/source2" os_arch="x86_64" } image "linux"`, 1)
	cfg, err := ParseBytes([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	images, _, err := cfg.ImageDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 2 || string(images[0].BackendPolicy) != string(images[1].BackendPolicy) {
		t.Fatal("images do not share normalized policy")
	}
	profile, err := hardware.DecodePolicy(images[0].BackendPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if profile.ConfigDrive.Format != "nocloud" || profile.ConfigDrive.Bus != "virtio" || !profile.ConfigDrive.IsEnabled() {
		t.Fatal("profile omission defaults lost", profile)
	}
}

func TestLegacyHardwareIsWarnedAndIgnoredIncludingFalseValues(t *testing.T) {
	text := strings.Replace(cloudConfig, `path="/source"`, `path="/source" firmware="invalid" efi=false disk_bus="invalid" disk_controller="bad" nic_model="bad" graphics="bad" video_model="bad" guest_agent=false initialization="bad"`, 1)
	text = strings.Replace(text, `cpu=2`, `cpu=2 hugepages=false autostart=false`, 1)
	text = strings.Replace(text, `uri="qemu:///session"`, `uri="qemu:///session" graphics_listen="not-an-address" config_drive_write_format="bad"`, 1)
	cfg, err := ParseBytes([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.MigrationWarnings) != 13 {
		t.Fatalf("expected a warning for every legacy field, got %d: %v", len(cfg.MigrationWarnings), cfg.MigrationWarnings)
	}
	for _, warning := range cfg.MigrationWarnings {
		if !strings.Contains(warning, "line ") || !strings.Contains(warning, "cloud.test.") || !strings.Contains(warning, HardwareMigrationGuide) {
			t.Fatal(warning)
		}
	}
	images, _, _ := cfg.ImageDefinitions()
	p, err := hardware.DecodePolicy(images[0].BackendPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if p.Firmware != "auto" || p.DiskBus != "virtio" || p.Graphics != "vnc" || !p.GuestAgentEnabled() || p.Hugepages {
		t.Fatal("legacy policy leaked", p)
	}
}

func TestHardwareProfileValidation(t *testing.T) {
	for _, body := range []string{`graphics="invalid"`, `disk_bus="invalid"`, `graphics="none" video_model="qxl"`, `disk_controller="virtio-scsi"`, `config_drive {format="invalid"}`, `config_drive {device_type="cdrom" bus="virtio"}`} {
		_, err := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+body+`}`, 1)))
		if err == nil || !strings.Contains(err.Error(), "cloud test hardware_profile default") {
			t.Fatalf("invalid profile accepted: %s, %v", body, err)
		}
	}
	_, err := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {} hardware_profile "default" {}`, 1)))
	if err == nil || !strings.Contains(err.Error(), "duplicate hardware_profile") {
		t.Fatal("duplicate accepted", err)
	}
}

func TestHardwareProfilesRejectOverridesAndUnknownSettings(t *testing.T) {
	for _, body := range []string{`hardware_profile { disk_bus="scsi" }`, `hardware_profile="default" config_drive {enabled=false}`} {
		text := strings.Replace(cloudConfig, `hardware_profile="default"`, body, 1)
		if _, err := ParseBytes([]byte(text)); err == nil {
			t.Fatalf("per-image override accepted: %s", body)
		}
	}
	for _, body := range []string{`autostart=true`, `graphics_listen="0.0.0.0"`, `unknown=true`, `efi=true`, `config_drive {initialization="none"}`, `config_drive {} config_drive {}`} {
		text := strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+body+`}`, 1)
		if _, err := ParseBytes([]byte(text)); err == nil {
			t.Fatalf("unsupported profile setting accepted: %s", body)
		}
	}
}

func TestMachinePolicyConfig(t *testing.T) {
	for _, body := range []string{"", `machine {}`, `machine { type="q35" }`, `machine { acceleration="qemu" }`, `machine { type="virt" acceleration="hvf" }`} {
		cfg, err := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+body+`}`, 1)))
		if err != nil {
			t.Fatal(body, err)
		}
		profiles, err := cfg.HardwareProfiles()
		if err != nil {
			t.Fatal(err)
		}
		m := profiles["test"]["default"].Machine
		if m == nil || m.Type == "" || m.Acceleration == "" || m.TypeExplicit != strings.Contains(body, "type=") || m.AccelerationExplicit != strings.Contains(body, "acceleration=") {
			t.Fatalf("explicitness lost for %s: %+v", body, m)
		}
	}
	for _, body := range []string{`machine {} machine {}`, `machine { unknown="q35" }`, `machine { acceleration="auto" }`, `machine { type="q35" type="pc" }`, `machine { acceleration="hardware" acceleration="qemu" }`, `machine { emulator="/qemu" }`, `machine { arch="aarch64" }`, `machine "named" {}`, `machine="q35"`, `machine { type="" }`, `machine { acceleration="" }`, `machine { type="q35,property=on" }`} {
		_, err := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+body+`}`, 1)))
		if err == nil || !strings.Contains(err.Error(), "cloud test") || !strings.Contains(err.Error(), "hardware_profile default") {
			t.Fatalf("missing actionable rejection for %s: %v", body, err)
		}
	}
}
