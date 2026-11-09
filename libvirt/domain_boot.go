package libvirt

import (
	"subuk/vmango/compute"

	libvirtxml "libvirt.org/go/libvirtxml"
)

// ensureDomainBootDisk replaces legacy disk-class boot selection with a stable
// per-device choice. Existing bootable disks and custom firmware boot modes are
// preserved. Call after creation or attachment, once all disks are available.
func ensureDomainBootDisk(config *libvirtxml.Domain) {
	if config.OS == nil || config.Devices == nil || config.OS.Kernel != "" || config.OS.Init != "" || config.Bootloader != "" {
		return
	}
	configDrive := ""
	if m, err := readSchedulerMetadata(config); err == nil && m != nil && m.Attachments != nil {
		configDrive = m.Attachments.ConfigDrive
	}
	volumes := make([]*compute.VirtualMachineAttachedVolume, len(config.Devices.Disks))
	for i, disk := range config.Devices.Disks {
		if disk.Target != nil {
			volumes[i] = VirtualMachineAttachedVolumeFromDomainDiskConfig(disk)
			if disk.Target.Dev == configDrive {
				volumes[i].Purpose = compute.PurposeConfigDrive
			}
		}
	}
	selected := compute.DefaultBootDisk(volumes)
	if selected == nil {
		return
	}
	var root *libvirtxml.DomainDisk
	var lastOrder uint
	observe := func(boot *libvirtxml.DomainDeviceBoot) {
		if boot != nil && boot.Order > lastOrder {
			lastOrder = boot.Order
		}
	}
	for i := range config.Devices.Disks {
		disk := &config.Devices.Disks[i]
		if disk.Device == "disk" && disk.Boot != nil {
			return
		}
		observe(disk.Boot)
		if volumes[i] == selected {
			root = disk
		}
	}
	for _, device := range config.Devices.Interfaces {
		observe(device.Boot)
	}
	for _, device := range config.Devices.Hostdevs {
		observe(device.Boot)
	}
	for _, device := range config.Devices.RedirDevs {
		observe(device.Boot)
	}
	for _, device := range config.Devices.Filesystems {
		observe(device.Boot)
	}
	if lastOrder > 0 {
		// A reattached system disk follows existing optical/network boot choices.
		// Never mix per-device priorities with legacy OS boot elements.
		if len(config.OS.BootDevices) == 0 && lastOrder < ^uint(0) {
			root.Boot = &libvirtxml.DomainDeviceBoot{Order: lastOrder + 1}
		}
		return
	}
	devices := config.OS.BootDevices
	if len(devices) == 0 {
		devices = []libvirtxml.DomainBootDevice{{Dev: "cdrom"}, {Dev: "hd"}}
	}
	hasDisk := false
	for _, device := range devices {
		switch device.Dev {
		case "hd":
			hasDisk = true
		case "cdrom":
		default:
			// Preserve custom PXE/floppy policies rather than reinterpret them.
			return
		}
	}
	if !hasDisk {
		return
	}
	order := uint(1)
	for _, device := range devices {
		if device.Dev == "hd" && root.Boot == nil {
			root.Boot = &libvirtxml.DomainDeviceBoot{Order: order}
			order++
		}
		if device.Dev == "cdrom" {
			for i := range config.Devices.Disks {
				disk := &config.Devices.Disks[i]
				if disk.Device == "cdrom" && (disk.Target == nil || disk.Target.Dev != configDrive) {
					if disk.Boot == nil {
						disk.Boot = &libvirtxml.DomainDeviceBoot{Order: order}
						order++
					}
					break
				}
			}
		}
	}
	config.OS.BootDevices = nil
}
