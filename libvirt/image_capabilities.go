package libvirt

import (
	"errors"
	"fmt"
	"subuk/vmango/compute"

	libvirt "libvirt.org/go/libvirt"
	libvirtxml "libvirt.org/go/libvirtxml"
)

func (repo *VirtualMachineRepository) Capabilities(node string, arch compute.Arch) (compute.VirtualMachineCapabilities, error) {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return compute.VirtualMachineCapabilities{}, repositoryError(err, "capabilities unavailable")
	}
	defer repo.pool.Release(node)
	xml, err := conn.GetDomainCapabilities(repo.settings[node].Emulator, arch.String(), "", "", 0)
	if err != nil {
		return compute.VirtualMachineCapabilities{}, repositoryError(err, "cannot fetch domain capabilities")
	}
	caps := &libvirtxml.DomainCaps{}
	if err := caps.Unmarshal(xml); err != nil {
		return compute.VirtualMachineCapabilities{}, repositoryError(err, "capabilities unavailable")
	}
	return imageCapabilities(caps), nil
}

func imageCapabilities(caps *libvirtxml.DomainCaps) compute.VirtualMachineCapabilities {
	result := compute.VirtualMachineCapabilities{Arch: compute.NewArch(caps.Arch), NICModels: []string{"virtio"},
		Graphics: []compute.GraphicType{compute.GraphicTypeNone}, VideoModels: []compute.VideoModel{compute.VideoModelNone},
		NetworkModels: map[string][]string{"qemu-usernet": {"virtio"}, "macos-socket-vmnet": {"virtio"}}}
	if caps.VCPU != nil {
		result.MaxVCPUs = caps.VCPU.Max
	}
	if caps.OS != nil {
		for _, e := range caps.OS.Enums {
			if e.Name == "firmware" {
				result.Firmwares = e.Values
			}
		}
	}
	// Older libvirt versions may omit firmware enumeration. Automatic firmware
	// selection cannot be promised there; explicit firmware requests fail closed.
	if caps.Devices != nil && caps.Devices.Disk != nil && caps.Devices.Disk.Supported == "yes" {
		for _, e := range caps.Devices.Disk.Enums {
			if e.Name == "bus" {
				for _, value := range e.Values {
					bus := compute.NewDeviceBus(value)
					if bus != compute.DeviceBusUnknown {
						result.DiskBuses = append(result.DiskBuses, bus)
					}
					if bus == compute.DeviceBusScsi {
						result.DiskControllers = []string{"virtio-scsi"}
					}
				}
			}
		}
	}
	if caps.Devices != nil {
		if caps.Devices.Graphics != nil {
			for _, e := range caps.Devices.Graphics.Enums {
				if e.Name == "type" {
					for _, value := range e.Values {
						if v := compute.NewGraphicType(value); v != compute.GraphicTypeUnknown {
							result.Graphics = append(result.Graphics, v)
						}
					}
				}
			}
		}
		if caps.Devices.Video != nil {
			for _, e := range caps.Devices.Video.Enums {
				if e.Name == "modelType" {
					for _, value := range e.Values {
						if v := compute.NewVideoModel(value); v != compute.VideoModelUnknown {
							result.VideoModels = append(result.VideoModels, v)
						}
					}
				}
			}
		}
	}
	// These are the portable NIC models exposed by this backend. Domain
	// capabilities do not enumerate NIC models on older supported libvirt releases.
	if result.Arch == compute.ArchAmd64 {
		result.NICModels = append(result.NICModels, "e1000", "rtl8139")
	}
	return result
}

// ValidateNew prevents a create request from silently updating an existing VM.
func (repo *VirtualMachineRepository) ValidateNew(vm *compute.VirtualMachine) error {
	conn, err := repo.pool.Acquire(vm.NodeId)
	if err != nil {
		return repositoryError(err, "backend validation failed")
	}
	defer repo.pool.Release(vm.NodeId)
	domain, err := conn.LookupDomainByName(vm.Name)
	if err == nil {
		domain.Free()
		return fmt.Errorf("%w: VM %q already exists", compute.ErrConflict, vm.Name)
	}
	if !errors.Is(err, libvirt.ERR_NO_DOMAIN) {
		return repositoryError(err, "backend validation failed")
	}
	domain, err = conn.LookupDomainByUUIDString(vm.Id)
	if err == nil {
		domain.Free()
		return fmt.Errorf("%w: VM identity already exists", compute.ErrConflict)
	}
	if !errors.Is(err, libvirt.ERR_NO_DOMAIN) {
		return repositoryError(err, "backend identity validation failed")
	}
	return nil
}
