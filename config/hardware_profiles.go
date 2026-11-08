package config

import (
	"fmt"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"

	"github.com/hashicorp/hcl/hcl/ast"
)

const HardwareMigrationGuide = "https://github.com/subuk/vmango/blob/master/docs/HARDWARE-PROFILES-MIGRATION.md"

type HardwareProfileConfig struct {
	CPU            *hardware.CPU             `hcl:"cpu"`
	Placement      *hardware.NativePlacement `hcl:"placement"`
	HyperV         *hardware.HyperV          `hcl:"hyperv"`
	KVM            *hardware.KVM             `hcl:"kvm"`
	SMBIOS         *hardware.SMBIOS          `hcl:"smbios"`
	Clock          HardwareClockConfig       `hcl:"clock"`
	Timers         []hardware.Timer          `hcl:"timer"`
	Machine        HardwareMachineConfig     `hcl:"machine"`
	ID             string                    `hcl:",key"`
	Name           string                    `hcl:"name"`
	Firmware       *hardware.Firmware        `hcl:"firmware"`
	DiskBus        string                    `hcl:"disk_bus"`
	DiskController string                    `hcl:"disk_controller"`
	NICModel       string                    `hcl:"nic_model"`
	Graphics       string                    `hcl:"graphics"`
	VideoModel     string                    `hcl:"video_model"`
	GuestAgent     *bool                     `hcl:"guest_agent"`
	Hugepages      bool                      `hcl:"hugepages"`
	ConfigDrive    HardwareConfigDriveConfig `hcl:"config_drive"`
}

type HardwareClockConfig struct {
	Mode     *string `hcl:"mode"`
	Timezone string  `hcl:"timezone"`
}

func (c HardwareClockConfig) policy() *hardware.Clock {
	p := &hardware.Clock{Mode: "utc", Timezone: c.Timezone}
	if c.Mode != nil {
		p.Mode = *c.Mode
		p.ModeExplicit = true
	}
	return p
}

type HardwareMachineConfig struct {
	Type         *string `hcl:"type"`
	Acceleration *string `hcl:"acceleration"`
}

func (c HardwareMachineConfig) policy() *hardware.Machine {
	m := &hardware.Machine{}
	if c.Type != nil {
		m.Type, m.TypeExplicit = *c.Type, true
	}
	if c.Acceleration != nil {
		m.Acceleration, m.AccelerationExplicit = *c.Acceleration, true
	}
	return m
}

type HardwareConfigDriveConfig struct {
	Enabled    *bool  `hcl:"enabled"`
	Format     string `hcl:"format"`
	DeviceType string `hcl:"device_type"`
	Bus        string `hcl:"bus"`
}

// HardwareProfiles returns normalized libvirt policy, never common cloud entities.
func (c *Config) HardwareProfiles() (map[string]map[string]hardware.Profile, error) {
	result := map[string]map[string]hardware.Profile{}
	for _, cloud := range c.Clouds {
		firmwares, err := firmwareCatalog(cloud)
		if err != nil {
			return nil, err
		}
		profiles := map[string]hardware.Profile{}
		for _, p := range cloud.HardwareProfiles {
			if !compute.ValidCatalogID(p.ID) {
				return nil, fmt.Errorf("cloud %s hardware_profile %q: use a stable catalog ID; see %s", cloud.ID, p.ID, HardwareMigrationGuide)
			}
			if _, exists := profiles[p.ID]; exists {
				return nil, fmt.Errorf("cloud %s: duplicate hardware_profile %q", cloud.ID, p.ID)
			}
			cpu := p.CPU
			if cpu == nil {
				cpu = &hardware.CPU{}
			} else {
				copy := *cpu
				cpu = &copy
				cpu.ModeExplicit = cpu.Mode != ""
			}
			placement := p.Placement
			if placement != nil {
				copy := *placement
				placement = &copy
				placement.ModeExplicit = placement.Mode != ""
			}
			firmware, err := resolveFirmwareConfig(p.Firmware, firmwares)
			if err != nil {
				return nil, fmt.Errorf("hardware_profile %s: %w", p.ID, err)
			}
			policy, err := hardware.Normalize(hardware.Profile{CPU: cpu, Placement: placement, HyperV: p.HyperV, KVM: p.KVM, SMBIOS: p.SMBIOS, Clock: p.Clock.policy(), Timers: p.Timers, Machine: p.Machine.policy(), ID: p.ID, Name: p.Name, FirmwarePolicy: firmware, DiskBus: p.DiskBus, DiskController: p.DiskController, NICModel: p.NICModel, Graphics: p.Graphics, VideoModel: p.VideoModel, GuestAgent: p.GuestAgent, Hugepages: p.Hugepages, ConfigDrive: hardware.ConfigDrive{Enabled: p.ConfigDrive.Enabled, Format: p.ConfigDrive.Format, DeviceType: p.ConfigDrive.DeviceType, Bus: p.ConfigDrive.Bus}})
			if err != nil {
				return nil, fmt.Errorf("cloud %s hardware_profile %s: %w; see %s", cloud.ID, p.ID, err, HardwareMigrationGuide)
			}
			profiles[p.ID] = policy
		}
		result[cloud.ID] = profiles
	}
	return result, nil
}

// Remove legacy attributes before decoding, so even invalid old values cannot
// accidentally influence new policy. AST presence also detects false/empty values.
func removeDeprecatedHardware(root *ast.ObjectList) []string {
	warnings := []string{}
	legacy := map[string]map[string]bool{
		"image":         {"efi": true, "firmware": true, "disk_bus": true, "disk_controller": true, "nic_model": true, "graphics": true, "video_model": true, "guest_agent": true, "initialization": true},
		"instance_type": {"hugepages": true, "autostart": true},
		"host":          {"graphics_listen": true, "config_drive_write_format": true},
	}
	for _, item := range root.Items {
		if len(item.Keys) < 2 || item.Keys[0].Token.Value() != "cloud" {
			continue
		}
		cloud, ok := item.Val.(*ast.ObjectType)
		if !ok {
			continue
		}
		cloudID := fmt.Sprint(item.Keys[1].Token.Value())
		for _, block := range cloud.List.Items {
			if len(block.Keys) < 2 {
				continue
			}
			owner := fmt.Sprint(block.Keys[0].Token.Value())
			attrs, ok := block.Val.(*ast.ObjectType)
			if !ok || legacy[owner] == nil {
				continue
			}
			kept := []*ast.ObjectItem{}
			for _, attribute := range attrs.List.Items {
				key := fmt.Sprint(attribute.Keys[0].Token.Value())
				if !legacy[owner][key] {
					kept = append(kept, attribute)
					continue
				}
				location := strings.Join([]string{"cloud", cloudID, owner, fmt.Sprint(block.Keys[1].Token.Value()), key}, ".")
				warnings = append(warnings, fmt.Sprintf("line %d: %s is deprecated and ignored; see %s", attribute.Pos().Line, location, HardwareMigrationGuide))
			}
			attrs.List.Items = kept
		}
	}
	return warnings
}
