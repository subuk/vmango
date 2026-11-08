package config

import (
	"strings"
	"testing"
)

func TestFirmwareSelectionDefaultsAndSecurity(t *testing.T) {
	for _, tc := range []struct {
		body, kind   string
		secure, keys bool
		hasKeys      bool
	}{
		{``, "auto", false, false, false}, {`firmware {}`, "auto", false, false, false},
		{`firmware {type="bios"}`, "bios", false, false, false},
		{`firmware {type="efi"}`, "efi", false, false, false},
		{`firmware {type="efi" secure_boot=false enrolled_keys=false}`, "efi", false, false, true},
		{`firmware {type="efi" secure_boot=true}`, "efi", true, true, true},
		{`firmware {type="efi" secure_boot=true enrolled_keys=false}`, "efi", true, false, true},
	} {
		t.Run(tc.body, func(t *testing.T) {
			cfg, err := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+tc.body+`}`, 1)))
			if err != nil {
				t.Fatal(err)
			}
			profiles, _ := cfg.HardwareProfiles()
			p := profiles["test"]["default"].FirmwarePolicy
			if p == nil || p.Type != tc.kind || p.SecureBoot != nil && *p.SecureBoot != tc.secure || (p.EnrolledKeys != nil) != tc.hasKeys || p.EnrolledKeys != nil && *p.EnrolledKeys != tc.keys {
				t.Fatalf("unexpected policy %+v", p)
			}
		})
	}
}
func TestFirmwareRejectsInvalidSelection(t *testing.T) {
	for _, body := range []string{`firmware="efi"`, `firmware {} firmware {}`, `firmware {type="other"}`, `firmware {type="auto" secure_boot=false}`, `firmware {type="bios" enrolled_keys=false}`, `firmware {type="efi" secure_boot=false enrolled_keys=true}`, `firmware {profile="missing"}`, `firmware {profile="x" type="efi"}`, `firmware {profile="x" secure_boot=false}`, `firmware {loader_format="raw"}`, `firmware {loader="/code"}`, `firmware {type="efi" type="bios"}`} {
		if _, err := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+body+`}`, 1))); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestNamedFirmwareAndHostBindings(t *testing.T) {
	base := strings.Replace(cloudConfig, `hardware_profile "default" {}`, `firmware_profile "pinned" {type="efi" secure_boot=true loader_format="raw" nvram_format="qcow2"} hardware_profile "default" {firmware {profile="pinned"}}`, 1)
	base = strings.Replace(base, `uri="qemu:///session"`, `uri="qemu:///session" firmware_profile "pinned" {loader="/fw/v1/CODE" nvram_template="/fw/v1/VARS"}`, 1)
	cfg, err := ParseBytes([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	profiles, _ := cfg.HardwareProfiles()
	p := profiles["test"]["default"].FirmwarePolicy
	if p.Profile != "pinned" || p.LoaderFormat != "raw" || p.NVRAMFormat != "qcow2" || !*p.EnrolledKeys {
		t.Fatalf("not resolved %+v", p)
	}
	for _, bad := range []string{
		strings.Replace(base, `type="efi" secure_boot=true`, `type="auto"`, 1),
		strings.Replace(base, `nvram_format="qcow2"`, `nvram_format="json"`, 1),
		strings.Replace(base, `loader="/fw/v1/CODE"`, `loader="relative"`, 1),
		strings.Replace(base, `nvram_template="/fw/v1/VARS"`, `nvram_template="/fw/v1/CODE"`, 1),
		strings.Replace(base, `loader="/fw/v1/CODE"`, `loader="/fw/v1/CODE" secure_boot=true`, 1),
		strings.Replace(base, `hardware_profile "default"`, `firmware_profile "pinned" {type="bios"} hardware_profile "default"`, 1),
	} {
		if _, err := ParseBytes([]byte(bad)); err == nil {
			t.Fatalf("invalid binding accepted: %s", bad)
		}
	}
}
