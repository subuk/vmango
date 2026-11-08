package libvirt

import (
	"fmt"
	xmlmodel "libvirt.org/go/libvirtxml"
	"subuk/vmango/libvirt/hardware"
)

func uintPointer(v uint) *uint { return &v }
func valueOrZero(v *uint) uint {
	if v == nil {
		return 0
	}
	return *v
}
func controllerIndex(config *xmlmodel.Domain, bus string, create bool) (uint, error) {
	model := ""
	if bus == "scsi" {
		model = "virtio-scsi"
	}
	if config.Devices == nil {
		config.Devices = &xmlmodel.DomainDeviceList{}
	}
	used := map[uint]bool{}
	for _, c := range config.Devices.Controllers {
		if c.Type != bus {
			continue
		}
		idx := valueOrZero(c.Index)
		used[idx] = true
		if c.Model == model || bus == "ide" {
			return idx, nil
		}
	}
	if !create {
		return 0, fmt.Errorf("no compatible %s controller; explicitly resolve hardware before adoption", bus)
	}
	index := uint(0)
	for used[index] {
		index++
	}
	config.Devices.Controllers = append(config.Devices.Controllers, xmlmodel.DomainController{Type: bus, Model: model, Index: uintPointer(index)})
	return index, nil
}
func driveAddressKey(a *xmlmodel.DomainAddressDrive) string {
	return fmt.Sprintf("%d/%d/%d/%d", valueOrZero(a.Controller), valueOrZero(a.Bus), valueOrZero(a.Target), valueOrZero(a.Unit))
}
func allocateDiskAddress(config *xmlmodel.Domain, bus string) (*xmlmodel.DomainAddress, error) {
	if bus == "virtio" {
		return nil, nil
	}
	if bus != "scsi" && bus != "ide" {
		return nil, fmt.Errorf("unsupported disk bus %q", bus)
	}
	index, err := controllerIndex(config, bus, true)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	implicit := uint(0)
	for _, disk := range config.Devices.Disks {
		if disk.Target == nil || disk.Target.Bus != bus {
			continue
		}
		if disk.Address != nil && disk.Address.Drive != nil {
			key := driveAddressKey(disk.Address.Drive)
			if used[key] {
				return nil, fmt.Errorf("existing %s disk addresses conflict at %s", bus, key)
			}
			used[key] = true
		} else {
			unit := implicit
			channel := uint(0)
			if bus == "ide" {
				channel = unit / 2
				unit %= 2
			}
			used[driveAddressKey(&xmlmodel.DomainAddressDrive{Controller: &index, Bus: &channel, Unit: &unit})] = true
			implicit++
		}
	}
	limit := uint(16384)
	if bus == "ide" {
		limit = 4
	}
	for slot := uint(0); slot < limit; slot++ {
		unit := slot
		channel := uint(0)
		if bus == "ide" {
			channel = slot / 2
			unit = slot % 2
		}
		if bus == "scsi" && unit == 7 {
			continue
		}
		a := &xmlmodel.DomainAddressDrive{Controller: uintPointer(index), Bus: uintPointer(channel), Target: uintPointer(0), Unit: uintPointer(unit)}
		if !used[driveAddressKey(a)] {
			return &xmlmodel.DomainAddress{Drive: a}, nil
		}
	}
	return nil, fmt.Errorf("%s controller %d has no free device addresses", bus, index)
}
func validateAdoptionControllers(config *xmlmodel.Domain, plan hardware.Plan) error {
	if config.Devices == nil {
		return nil
	}
	controllers := map[string]bool{}
	for _, c := range config.Devices.Controllers {
		if c.Type != "scsi" && c.Type != "ide" && c.Type != "virtio-serial" {
			continue
		}
		key := fmt.Sprintf("%s/%d", c.Type, valueOrZero(c.Index))
		if controllers[key] {
			return fmt.Errorf("duplicate existing controller identifier %s", key)
		}
		controllers[key] = true
	}
	targets := map[string]bool{}
	addresses := map[string]bool{}
	for _, d := range config.Devices.Disks {
		if d.Target != nil && d.Target.Dev != "" {
			if targets[d.Target.Dev] {
				return fmt.Errorf("duplicate existing disk target %s", d.Target.Dev)
			}
			targets[d.Target.Dev] = true
		}
		if d.Target != nil && d.Address != nil && d.Address.Drive != nil {
			key := d.Target.Bus + "/" + driveAddressKey(d.Address.Drive)
			if addresses[key] {
				return fmt.Errorf("duplicate existing disk address %s", key)
			}
			addresses[key] = true
		}
	}
	for _, disk := range config.Devices.Disks {
		if disk.Target == nil || disk.Device != "disk" && disk.Device != "cdrom" {
			continue
		}
		bus := disk.Target.Bus
		if disk.Alias != nil && disk.Alias.Name == "ua-configdrive" {
			bus = plan.Profile.ConfigDrive.Bus
		} else if disk.Device == "disk" {
			bus = plan.Profile.DiskBus
		} else {
			continue
		}
		if bus != "scsi" {
			continue
		}
		index := uint(0)
		if disk.Address != nil && disk.Address.Drive != nil {
			index = valueOrZero(disk.Address.Drive.Controller)
		}
		found := false
		for _, c := range config.Devices.Controllers {
			if c.Type == "scsi" && valueOrZero(c.Index) == index && c.Model == "virtio-scsi" {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("adoption requires disk %s to use a compatible virtio-scsi controller %d; no hardware was rewritten", disk.Target.Dev, index)
		}
	}
	return nil
}

// diskTargetIndex supports the complete alphabetic target suffix, including vdaa.
func diskTargetIndex(name, prefix string) (int, bool) {
	if len(name) <= len(prefix) || name[:len(prefix)] != prefix {
		return 0, false
	}
	index := 0
	for _, ch := range name[len(prefix):] {
		if ch < 'a' || ch > 'z' {
			return 0, false
		}
		index = index*26 + int(ch-'a') + 1
		if index > 1<<20 {
			return 0, false
		}
	}
	return index - 1, true
}
func diskTargetName(index int) string {
	out := ""
	for index >= 0 {
		out = string(rune('a'+index%26)) + out
		index = index/26 - 1
	}
	return out
}
