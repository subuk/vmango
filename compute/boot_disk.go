package compute

// DefaultBootDisk selects a system disk without relying on backend device names.
// The root alias identifies the system disk in a multi-disk machine. With no
// root alias, only a single non-config-drive disk is unambiguous.
func DefaultBootDisk(volumes []*VirtualMachineAttachedVolume) *VirtualMachineAttachedVolume {
	var root, only *VirtualMachineAttachedVolume
	var roots, disks int
	for _, volume := range volumes {
		if volume == nil || volume.DeviceType != DeviceTypeDisk || volume.Purpose == PurposeConfigDrive || volume.GeneratedArtifact {
			continue
		}
		disks++
		only = volume
		if volume.Alias == "root" {
			roots++
			root = volume
		}
	}
	if roots == 1 {
		return root
	}
	if roots == 0 && disks == 1 {
		return only
	}
	return nil
}
