package hardware

import (
	"encoding/json"
	"testing"
)

func TestFirmwareMetadataUsesCapabilitiesNotFilenames(t *testing.T) {
	raw := `{"interface-types":["uefi"],"mapping":{"device":"flash","executable":{"filename":"/custom/looks-secure.fd","format":"raw"},"nvram-template":{"filename":"/custom/keys.fd","format":"raw"}},"targets":[{"architecture":"x86_64","machines":["pc-q35-*"]}],"features":[]}`
	var d FirmwareDescriptor
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	yes := true
	f, _ := NormalizeFirmware(Firmware{Type: "efi", SecureBoot: &yes}, false)
	if ValidateFirmwareDescriptor(d, f, "x86_64", "pc-q35-9.2") == nil {
		t.Fatal("filename was accepted as secure firmware evidence")
	}
	d.Features = []string{"secure-boot", "enrolled-keys"}
	if err := ValidateFirmwareDescriptor(d, f, "x86_64", "pc-q35-9.2"); err != nil {
		t.Fatal(err)
	}
	d.Mapping.Device = "memory"
	if ValidateFirmwareDescriptor(d, f, "x86_64", "pc-q35-9.2") == nil {
		t.Fatal("non-pflash UEFI accepted")
	}
	d.Mapping.Device = "flash"
	if ValidateFirmwareDescriptor(d, f, "aarch64", "virt-9.2") == nil {
		t.Fatal("incompatible target accepted")
	}
}
func TestFirmwareDescriptorOverrideMasks(t *testing.T) {
	first := []byte("same.json\x00{\"interface-types\":[\"uefi\"]}\x00same.json\x00\x00")
	got, err := DecodeFirmwareDescriptors(first)
	if err != nil || len(got) != 0 {
		t.Fatal("system mask did not override vendor descriptor", got, err)
	}
}
