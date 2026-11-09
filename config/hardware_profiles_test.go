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

func TestRemovedHardwareFieldsAreRejectedIncludingEmptyValues(t *testing.T) {
	cases := []struct{ anchor, fields string }{
		{`path="/source"`, `efi=false|firmware=""|disk_bus=""|disk_controller=""|nic_model=""|graphics=""|video_model=""|guest_agent=false|initialization=""`},
		{`cpu=2`, `hugepages=false|autostart=false`},
		{`uri="qemu:///session"`, `graphics_listen=""|config_drive_write_format=""`},
		{`network="default"`, `model=""|vlan=0`},
	}
	for _, c := range cases {
		for _, field := range strings.Split(c.fields, "|") {
			_, err := ParseBytes([]byte(strings.Replace(cloudConfig, c.anchor, c.anchor+" "+field, 1)))
			if err == nil || !strings.Contains(err.Error(), "unsupported") || !strings.Contains(err.Error(), "line ") || !strings.Contains(err.Error(), "cloud.test.") || !strings.Contains(err.Error(), HardwareMigrationGuide) {
				t.Fatalf("%s: %v", field, err)
			}
		}
	}
}

func TestHardwareProfileValidation(t *testing.T) {
	for _, body := range []string{`graphics="invalid"`, `disk_bus="invalid"`, `graphics="none" video_model="qxl"`, `disk_controller="virtio-scsi"`, `config_drive {format="invalid"}`, `config_drive {device_type="cdrom" bus="virtio"}`} {
		_, err := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+body+`}`, 1)))
		if err == nil || !strings.Contains(err.Error(), "cloud test") || !strings.Contains(err.Error(), "hardware_profile default") {
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

func TestGuestSystemConfig(t *testing.T) {
	good := `smbios { bios { vendor="Vmango" version="1" date="01/02/2026" release="1.2" } system { manufacturer="Vmango" product="Guest" version="1" sku="test" family="Cloud" } baseboard { manufacturer="Vmango" product="Board" version="1" } } clock {mode="timezone" timezone="Europe/Berlin"} timer "rtc" {tickpolicy="catchup"} timer "hpet" {present=false}`
	cfg, err := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+good+`}`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	profiles, _ := cfg.HardwareProfiles()
	p := profiles["test"]["default"]
	if p.SMBIOS == nil || p.SMBIOS.System.Product != "Guest" || !p.Clock.ModeExplicit || p.Clock.Timezone != "Europe/Berlin" || len(p.Timers) != 2 || *p.Timers[1].Present {
		t.Fatalf("lost fields: %+v", p)
	}
	for _, body := range []string{
		`smbios {system {uuid="bad"}}`, `smbios {system {serial="bad"}}`, `smbios {baseboard {asset="bad"}}`, `smbios {chassis {}}`, `smbios {bios {vendor="a" vendor="b"}}`, `smbios {bios {} bios {}}`, `smbios {} smbios {}`, `smbios {system {product=""}}`, `smbios "named" {}`,
		`clock {mode="timezone"}`, `clock {mode="timezone" timezone="Nowhere/Missing"}`, `clock {mode="localtime" timezone="Europe/Berlin"}`, `clock {mode="utc" adjustment="reset"}`, `clock {} clock {}`, `clock {mode="utc" mode="localtime"}`,
		`timer "rtc" {present=false}`, `timer "armvtimer" {present=false}`, `timer "pit" {tickpolicy="merge"}`, `timer "hpet" {tickpolicy="delay"}`, `timer "tsc" {}`, `timer "rtc" {} timer "rtc" {}`, `timer "hpet" {present="false"}`, `timer "rtc" {track="guest"}`, `timer {tickpolicy="delay"}`, `timer "rtc" {tickpolicy=""}`,
	} {
		_, err := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+body+`}`, 1)))
		if err == nil || !strings.Contains(err.Error(), "cloud test") || !strings.Contains(err.Error(), "hardware_profile default") {
			t.Fatalf("invalid config lacks contextual error: %s: %v", body, err)
		}
	}
}
