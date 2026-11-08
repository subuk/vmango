package config

import (
	"fmt"
	"github.com/hashicorp/hcl/hcl/ast"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
)

type FirmwareProfileConfig struct {
	ID           string `hcl:",key"`
	Type         string `hcl:"type"`
	SecureBoot   *bool  `hcl:"secure_boot"`
	EnrolledKeys *bool  `hcl:"enrolled_keys"`
	LoaderFormat string `hcl:"loader_format"`
	NVRAMFormat  string `hcl:"nvram_format"`
}

func firmwareCatalog(cloud CloudConfig) (map[string]hardware.Firmware, error) {
	result := map[string]hardware.Firmware{}
	for _, entry := range cloud.FirmwareProfiles {
		if !compute.ValidCatalogID(entry.ID) {
			return nil, fmt.Errorf("invalid firmware_profile identifier %q", entry.ID)
		}
		if _, ok := result[entry.ID]; ok {
			return nil, fmt.Errorf("duplicate firmware_profile %q", entry.ID)
		}
		p, err := hardware.NormalizeFirmware(hardware.Firmware{Profile: entry.ID, Type: entry.Type, SecureBoot: entry.SecureBoot, EnrolledKeys: entry.EnrolledKeys, LoaderFormat: entry.LoaderFormat, NVRAMFormat: entry.NVRAMFormat, Explicit: true}, true)
		if err != nil {
			return nil, fmt.Errorf("firmware_profile %s: %w", entry.ID, err)
		}
		result[entry.ID] = p
	}
	for _, host := range cloud.Hosts {
		seen := map[string]bool{}
		for _, b := range host.FirmwareProfiles {
			if seen[b.ID] {
				return nil, fmt.Errorf("host %s: duplicate firmware_profile %s", host.Node, b.ID)
			}
			seen[b.ID] = true
			p, ok := result[b.ID]
			if !ok {
				return nil, fmt.Errorf("host %s: unknown firmware_profile %s", host.Node, b.ID)
			}
			if err := hardware.ValidateFirmwareHost(p, b); err != nil {
				return nil, fmt.Errorf("host %s: %w", host.Node, err)
			}
		}
	}
	return result, nil
}
func resolveFirmwareConfig(input *hardware.Firmware, catalog map[string]hardware.Firmware) (*hardware.Firmware, error) {
	f := hardware.Firmware{}
	if input != nil {
		f = *input
		f.Explicit = f.Type != "" || f.Profile != ""
	}
	if f.Profile != "" {
		if f.Type != "" || f.SecureBoot != nil || f.EnrolledKeys != nil || f.LoaderFormat != "" || f.NVRAMFormat != "" {
			return nil, fmt.Errorf("firmware profile reference cannot contain type or local overrides")
		}
		p, ok := catalog[f.Profile]
		if !ok {
			return nil, fmt.Errorf("unknown firmware_profile %q", f.Profile)
		}
		return &p, nil
	}
	p, err := hardware.NormalizeFirmware(f, false)
	return &p, err
}
func validateFirmwareBlock(item *ast.ObjectItem, owner string) error {
	key := item.Keys[0].Token.Value().(string)
	block, ok := item.Val.(*ast.ObjectType)
	if !ok {
		return fmt.Errorf("legacy flat firmware syntax was removed: replace firmware = \"efi\" with firmware { type = \"efi\" }; see %s", HardwareMigrationGuide)
	}
	allowed := map[string]bool{}
	if key == "firmware" {
		if owner != "hardware_profile" || len(item.Keys) != 1 {
			return fmt.Errorf("firmware must be one unnamed hardware_profile block")
		}
		allowed = map[string]bool{"type": true, "profile": true, "secure_boot": true, "enrolled_keys": true}
	} else {
		if len(item.Keys) != 2 || owner != "cloud" && owner != "host" {
			return fmt.Errorf("firmware_profile must be a named cloud policy or host binding")
		}
		if owner == "cloud" {
			allowed = map[string]bool{"type": true, "secure_boot": true, "enrolled_keys": true, "loader_format": true, "nvram_format": true}
		} else {
			allowed = map[string]bool{"loader": true, "nvram_template": true}
		}
	}
	for _, field := range block.List.Items {
		name := field.Keys[0].Token.Value().(string)
		if !allowed[name] {
			return fmt.Errorf("%s.%s: unsupported setting %s", owner, key, name)
		}
		if len(block.List.Filter(name).Items) > 1 {
			return fmt.Errorf("%s.%s: duplicate %s", owner, key, name)
		}
		if _, nested := field.Val.(*ast.ObjectType); nested {
			return fmt.Errorf("%s.%s.%s must be a scalar", owner, key, name)
		}
	}
	return nil
}
