package hardware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"sort"
)

type FirmwareFile struct {
	Filename string `json:"filename"`
	Format   string `json:"format"`
}
type FirmwareDescriptor struct {
	Interfaces []string `json:"interface-types"`
	Mapping    struct {
		Device        string       `json:"device"`
		Executable    FirmwareFile `json:"executable"`
		NVRAMTemplate FirmwareFile `json:"nvram-template"`
	} `json:"mapping"`
	Targets []struct {
		Architecture string   `json:"architecture"`
		Machines     []string `json:"machines"`
	} `json:"targets"`
	Features []string `json:"features"`
}

func DecodeFirmwareDescriptors(raw []byte) ([]FirmwareDescriptor, error) {
	parts := bytes.Split(raw, []byte{0})
	byName := map[string][]byte{}
	for i := 0; i+1 < len(parts); i += 2 {
		byName[string(parts[i])] = parts[i+1]
	}
	names := []string{}
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	result := []FirmwareDescriptor{}
	for _, name := range names {
		data := byName[name]
		if len(bytes.TrimSpace(data)) == 0 {
			continue
		}
		var d FirmwareDescriptor
		if err := json.Unmarshal(data, &d); err != nil {
			return nil, fmt.Errorf("firmware descriptor %s: %w", name, err)
		}
		result = append(result, d)
	}
	return result, nil
}
func (d FirmwareDescriptor) MatchesPlatform(arch, machine string) bool {
	for _, t := range d.Targets {
		if t.Architecture != arch {
			continue
		}
		for _, pattern := range t.Machines {
			if ok, _ := path.Match(pattern, machine); ok {
				return true
			}
		}
	}
	return false
}
func (d FirmwareDescriptor) Binding() FirmwareBinding {
	t := "bios"
	lt := "rom"
	if slices.Contains(d.Interfaces, "uefi") {
		t = "efi"
		lt = "pflash"
	}
	secure := slices.Contains(d.Features, "secure-boot")
	keys := slices.Contains(d.Features, "enrolled-keys")
	b := FirmwareBinding{Type: t, Loader: d.Mapping.Executable.Filename, LoaderType: lt, LoaderFormat: d.Mapping.Executable.Format, Readonly: "yes", NVRAMTemplate: d.Mapping.NVRAMTemplate.Filename, NVRAMFormat: d.Mapping.NVRAMTemplate.Format, Evidence: "host-firmware-descriptor"}
	if t == "efi" {
		b.SecureBoot = &secure
		b.EnrolledKeys = &keys
		if secure {
			b.Secure = "yes"
		} else {
			b.Secure = "no"
		}
	}
	return b
}
func ValidateFirmwareDescriptor(d FirmwareDescriptor, f Firmware, arch, machine string) error {
	if !d.MatchesPlatform(arch, machine) {
		return fmt.Errorf("firmware metadata does not support %s/%s", arch, machine)
	}
	b := d.Binding()
	if !path.IsAbs(b.Loader) || !slices.Contains([]string{"raw", "qcow2"}, b.LoaderFormat) || b.Type == "efi" && (!path.IsAbs(b.NVRAMTemplate) || !slices.Contains([]string{"raw", "qcow2"}, b.NVRAMFormat)) {
		return fmt.Errorf("firmware metadata requires absolute file paths and raw or qcow2 formats")
	}
	if b.Type == "bios" && b.LoaderFormat != "raw" {
		return fmt.Errorf("BIOS ROM requires raw format")
	}
	if b.Secure == "yes" && arch != "x86_64" {
		return fmt.Errorf("Secure Boot pflash is supported only on x86_64")
	}
	if f.Type != "auto" && b.Type != f.Type {
		return fmt.Errorf("firmware metadata type conflicts with requested %s", f.Type)
	}
	if b.Type == "efi" && (d.Mapping.Device != "flash" || b.NVRAMTemplate == "") {
		return fmt.Errorf("only persistent pflash UEFI with a file VARS template is supported; varstore/stateless UEFI is excluded")
	}
	if b.Type == "bios" && d.Mapping.Device != "memory" {
		return fmt.Errorf("BIOS requires ROM firmware")
	}
	if f.SecureBoot != nil && *b.SecureBoot != *f.SecureBoot {
		return fmt.Errorf("firmware metadata conflicts with secure_boot requirement")
	}
	if f.EnrolledKeys != nil && f.SecureBoot != nil && *f.SecureBoot && *b.EnrolledKeys != *f.EnrolledKeys {
		return fmt.Errorf("firmware template metadata conflicts with enrolled_keys requirement")
	}
	if f.Profile != "" && (f.LoaderFormat != b.LoaderFormat || f.Type == "efi" && f.NVRAMFormat != b.NVRAMFormat) {
		return fmt.Errorf("firmware descriptor format conflicts with named policy")
	}
	return nil
}
