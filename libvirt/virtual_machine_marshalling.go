package libvirt

import (
	"fmt"
	"subuk/vmango/compute"

	"github.com/libvirt/libvirt-go"
	"github.com/libvirt/libvirt-go-xml"
)

func VirtualMachineAttachedVolumeFromDomainDiskConfig(diskConfig libvirtxml.DomainDisk) *compute.VirtualMachineAttachedVolume {
	volume := &compute.VirtualMachineAttachedVolume{}
	if diskConfig.Device != "disk" {
		return nil
	}
	if diskConfig.Source == nil {
		return nil
	}
	if diskConfig.Source.File != nil {
		volume.Type = "file"
		volume.Path = diskConfig.Source.File.File
	}
	if diskConfig.Source.Block != nil {
		volume.Type = "block"
		volume.Path = diskConfig.Source.Block.Dev
	}
	return volume
}

func VirtualMachineAttachedInterfaceFromInterfaceConfig(ifaceConfig libvirtxml.DomainInterface) *compute.VirtualMachineAttachedInterface {
	iface := &compute.VirtualMachineAttachedInterface{}
	iface.Mac = ifaceConfig.MAC.Address
	return iface
}

func VirtualMachineFromDomainConfig(domainConfig *libvirtxml.Domain, domainInfo *libvirt.DomainInfo) (*compute.VirtualMachine, error) {
	vm := &compute.VirtualMachine{}
	vm.Id = domainConfig.Name
	vm.VCpus = domainConfig.VCPU.Value

	switch domainConfig.OS.Type.Arch {
	default:
		vm.Arch = compute.ArchUnknown
	case "x86_64":
		vm.Arch = compute.ArchAmd64
	}

	switch domainInfo.State {
	default:
		vm.State = compute.StateUnknown
	case libvirt.DOMAIN_NOSTATE:
		vm.State = compute.StateUnknown
	case libvirt.DOMAIN_RUNNING:
		vm.State = compute.StateRunning
	case libvirt.DOMAIN_BLOCKED:
		vm.State = compute.StateStopped
	case libvirt.DOMAIN_PAUSED:
		vm.State = compute.StateStopped
	case libvirt.DOMAIN_SHUTDOWN:
		vm.State = compute.StateStopped
	case libvirt.DOMAIN_CRASHED:
		vm.State = compute.StateStopped
	case libvirt.DOMAIN_PMSUSPENDED:
		vm.State = compute.StateStopped
	case libvirt.DOMAIN_SHUTOFF:
		vm.State = compute.StateStopped
	}

	switch domainConfig.Memory.Unit {
	case "KiB":
		vm.Memory = domainConfig.Memory.Value
	case "MiB":
		vm.Memory = domainConfig.Memory.Value * 1024
	case "GiB":
		vm.Memory = domainConfig.Memory.Value * 1024
	default:
		return nil, fmt.Errorf("unknown memory unit '%s' for domain %s", domainConfig.Memory.Unit, domainConfig.Name)
	}

	for _, netInterfaceConfig := range domainConfig.Devices.Interfaces {
		iface := VirtualMachineAttachedInterfaceFromInterfaceConfig(netInterfaceConfig)
		vm.Interfaces = append(vm.Interfaces, iface)
	}

	for _, diskConfig := range domainConfig.Devices.Disks {
		volume := VirtualMachineAttachedVolumeFromDomainDiskConfig(diskConfig)
		if volume == nil {
			continue
		}
		vm.Volumes = append(vm.Volumes, volume)
	}
	return vm, nil
}
