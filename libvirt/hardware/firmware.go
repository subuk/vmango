package hardware

import (
	"fmt"
	"path"
	"slices"
)

// Firmware is resolved backend policy. Profile identifies a catalog entry, not
// a lookup to perform when an accepted command or existing VM is executed.
type Firmware struct {
	Profile      string `json:"profile,omitempty" hcl:"profile"`
	Type         string `json:"type" hcl:"type"`
	SecureBoot   *bool  `json:"secure_boot,omitempty" hcl:"secure_boot"`
	EnrolledKeys *bool  `json:"enrolled_keys,omitempty" hcl:"enrolled_keys"`
	LoaderFormat string `json:"loader_format,omitempty" hcl:"loader_format"`
	NVRAMFormat  string `json:"nvram_format,omitempty" hcl:"nvram_format"`
	Explicit     bool   `json:"explicit,omitempty" hcl:"-"`
}
type FirmwareHostBinding struct {
	ID            string `json:"id" hcl:",key"`
	Loader        string `json:"loader" hcl:"loader"`
	NVRAMTemplate string `json:"nvram_template,omitempty" hcl:"nvram_template"`
}

// FirmwareBinding records path identity, not content identity. NVRAM ownership
// is affirmative only for a domain created by Vmango, never an adopted file.
type FirmwareBinding struct {
	Unsupported   string `json:"unsupported,omitempty"`
	Type          string `json:"type"`
	Loader        string `json:"loader,omitempty"`
	LoaderType    string `json:"loader_type,omitempty"`
	LoaderFormat  string `json:"loader_format,omitempty"`
	Readonly      string `json:"readonly,omitempty"`
	Secure        string `json:"secure,omitempty"`
	NVRAMTemplate string `json:"nvram_template,omitempty"`
	NVRAMFormat   string `json:"nvram_format,omitempty"`
	NVRAM         string `json:"nvram,omitempty"`
	SecureBoot    *bool  `json:"secure_boot,omitempty"`
	EnrolledKeys  *bool  `json:"enrolled_keys,omitempty"`
	Owned         bool   `json:"owned"`
	OwnerVM       string `json:"owner_vm,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
	Evidence      string `json:"evidence,omitempty"`
}

func NormalizeFirmware(f Firmware, named bool) (Firmware, error) {
	fail := func(message string) (Firmware, error) { return f, fmt.Errorf("firmware: %s", message) }
	if f.Type == "" {
		f.Type = "auto"
	}
	if !slices.Contains([]string{"auto", "bios", "efi"}, f.Type) {
		return fail("type must be auto, bios or efi")
	}
	if named && f.Type == "auto" {
		return fail("named profiles require explicit bios or efi type")
	}
	if f.Type != "efi" && (f.SecureBoot != nil || f.EnrolledKeys != nil || f.NVRAMFormat != "") {
		return fail("Secure Boot, enrolled keys and NVRAM require explicit efi")
	}
	if !named && (f.LoaderFormat != "" || f.NVRAMFormat != "") {
		return fail("formats belong only in a named firmware_profile")
	}
	if named {
		if f.LoaderFormat == "" {
			f.LoaderFormat = "raw"
		}
		if !slices.Contains([]string{"raw", "qcow2"}, f.LoaderFormat) {
			return fail("loader_format must be raw or qcow2")
		}
		if f.Type == "bios" && f.LoaderFormat != "raw" {
			return fail("BIOS ROM requires raw loader_format")
		}
	}
	if f.Type == "efi" {
		if f.SecureBoot == nil {
			v := false
			f.SecureBoot = &v
		}
		if !*f.SecureBoot && f.EnrolledKeys != nil && *f.EnrolledKeys {
			return fail("enrolled_keys=true requires secure_boot=true")
		}
		if *f.SecureBoot && f.EnrolledKeys == nil {
			v := true
			f.EnrolledKeys = &v
		}
		if named {
			if f.NVRAMFormat == "" {
				f.NVRAMFormat = "raw"
			}
			if !slices.Contains([]string{"raw", "qcow2"}, f.NVRAMFormat) {
				return fail("nvram_format must be raw or qcow2")
			}
		}
	}
	return f, nil
}
func ValidateFirmwareHost(f Firmware, b FirmwareHostBinding) error {
	if !path.IsAbs(b.Loader) || path.Clean(b.Loader) != b.Loader {
		return fmt.Errorf("firmware profile %s requires an absolute clean host loader path", b.ID)
	}
	if f.Type == "efi" {
		if !path.IsAbs(b.NVRAMTemplate) || path.Clean(b.NVRAMTemplate) != b.NVRAMTemplate || b.NVRAMTemplate == b.Loader {
			return fmt.Errorf("firmware profile %s requires a distinct absolute VARS template path", b.ID)
		}
	} else if b.NVRAMTemplate != "" {
		return fmt.Errorf("BIOS firmware profile %s cannot have an NVRAM template", b.ID)
	}
	return nil
}
