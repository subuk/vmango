package libvirt

import (
	"fmt"
	"subuk/vmango/compute"

	"github.com/libvirt/libvirt-go"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

func DomainDiskConfigFromVirtualMachineAttachedVolume(volume *compute.VirtualMachineAttachedVolume) *libvirtxml.DomainDisk {
	diskConfig := &libvirtxml.DomainDisk{
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu"},
		Target: &libvirtxml.DomainDiskTarget{},
	}
	switch volume.Format {
	default:
		panic(fmt.Errorf("unsupported volume format '%s'", volume.Format))
	case compute.FormatQcow2:
		diskConfig.Driver.Type = "qcow2"
	case compute.FormatRaw:
		diskConfig.Driver.Type = "raw"
	case compute.FormatIso:
		diskConfig.Driver.Type = "raw"
	}
	switch volume.DeviceType {
	default:
		panic(fmt.Errorf("unsupported volume device type '%s'", volume.DeviceType))
	case compute.DeviceTypeCdrom:
		diskConfig.Device = "cdrom"
		diskConfig.ReadOnly = &libvirtxml.DomainDiskReadOnly{}
		diskConfig.Target.Bus = volume.DeviceBus.String()
		diskConfig.Target.Dev = volume.DeviceName
	case compute.DeviceTypeDisk:
		diskConfig.Device = "disk"
		diskConfig.Target.Bus = volume.DeviceBus.String()
		diskConfig.Target.Dev = volume.DeviceName
	}
	switch volume.Type {
	default:
		panic(fmt.Errorf("unknown volume type '%s'", volume.Type))
	case compute.VolumeTypeFile:
		diskConfig.Source = &libvirtxml.DomainDiskSource{
			File: &libvirtxml.DomainDiskSourceFile{File: volume.Path},
		}
	case compute.VolumeTypeBlock:
		diskConfig.Source = &libvirtxml.DomainDiskSource{
			Block: &libvirtxml.DomainDiskSourceBlock{Dev: volume.Path},
		}
	}
	return diskConfig
}

func VirtualMachineAttachedVolumeFromDomainDiskConfig(diskConfig libvirtxml.DomainDisk) *compute.VirtualMachineAttachedVolume {
	volume := &compute.VirtualMachineAttachedVolume{}
	volume.DeviceName = diskConfig.Target.Dev
	volume.DeviceBus = compute.NewDeviceBus(diskConfig.Target.Bus)

	switch diskConfig.Device {
	default:
		volume.DeviceType = compute.DeviceTypeUnknown
	case "disk":
		volume.DeviceType = compute.DeviceTypeDisk
	case "cdrom":
		volume.DeviceType = compute.DeviceTypeCdrom
	}
	if diskConfig.Driver != nil {
		volume.Format = compute.NewVolumeFormat(diskConfig.Driver.Type)
	}
	volume.Type = compute.VolumeTypeUnknown
	if diskConfig.Source != nil {
		if diskConfig.Source.File != nil {
			volume.Type = compute.VolumeTypeFile
			volume.Path = diskConfig.Source.File.File
		}
		if diskConfig.Source.Block != nil {
			volume.Type = compute.VolumeTypeBlock
			volume.Path = diskConfig.Source.Block.Dev
		}
	}
	return volume
}

func VirtualMachineAttachedInterfaceFromInterfaceConfig(ifaceConfig libvirtxml.DomainInterface) *compute.VirtualMachineAttachedInterface {
	iface := &compute.VirtualMachineAttachedInterface{}
	iface.Mac = ifaceConfig.MAC.Address
	if ifaceConfig.Model != nil {
		iface.Model = ifaceConfig.Model.Type
	}
	if ifaceConfig.Source != nil {
		if ifaceConfig.Source.Bridge != nil {
			iface.NetworkType = compute.NetworkTypeBridge
			iface.NetworkName = ifaceConfig.Source.Bridge.Bridge
		}
		if ifaceConfig.Source.Network != nil {
			iface.NetworkType = compute.NetworkTypeLibvirt
			iface.NetworkName = ifaceConfig.Source.Network.Network
		}
	}
	if ifaceConfig.VLan != nil {
		if len(ifaceConfig.VLan.Tags) == 1 && ifaceConfig.VLan.Trunk == "" {
			iface.AccessVlan = ifaceConfig.VLan.Tags[0].ID
		}
	}
	return iface
}

func VirtualMachineFromDomainConfig(domainConfig *libvirtxml.Domain, domainInfo *libvirt.DomainInfo) (*compute.VirtualMachine, error) {
	vm := &compute.VirtualMachine{}
	vm.Id = domainConfig.Name
	vm.VCpus = domainConfig.VCPU.Value
	vm.Memory = ComputeSizeFromLibvirtSize(domainConfig.Memory.Unit, uint64(domainConfig.Memory.Value))

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

	if domainConfig.CPUTune != nil {
		vm.Cpupin = &compute.VirtualMachineCpuPin{
			Vcpus:    map[uint][]uint{},
			Emulator: []uint{},
		}
		for _, vcpupin := range domainConfig.CPUTune.VCPUPin {
			vm.Cpupin.Vcpus[vcpupin.VCPU] = ParseCpuAffinity(vcpupin.CPUSet)
		}
		if domainConfig.CPUTune.EmulatorPin != nil {
			vm.Cpupin.Emulator = ParseCpuAffinity(domainConfig.CPUTune.EmulatorPin.CPUSet)
		}
	}

	for _, netInterfaceConfig := range domainConfig.Devices.Interfaces {
		iface := VirtualMachineAttachedInterfaceFromInterfaceConfig(netInterfaceConfig)
		vm.Interfaces = append(vm.Interfaces, iface)
	}

	for _, diskConfig := range domainConfig.Devices.Disks {
		volume := VirtualMachineAttachedVolumeFromDomainDiskConfig(diskConfig)
		vm.Volumes = append(vm.Volumes, volume)
	}
	for _, channel := range domainConfig.Devices.Channels {
		if channel.Target != nil && channel.Target.VirtIO != nil && channel.Target.VirtIO.Name == "org.qemu.guest_agent.0" {
			vm.GuestAgent = true
		}
	}

	for _, graphic := range domainConfig.Devices.Graphics {
		if graphic.VNC != nil {
			vm.Graphic.Type = compute.GraphicTypeVnc
			vm.Graphic.Listen = graphic.VNC.Listen
			break
		}
		if graphic.Spice != nil {
			vm.Graphic.Type = compute.GraphicTypeSpice
			vm.Graphic.Listen = graphic.Spice.Listen
			break
		}
	}
	if vm.Graphic.Type == compute.GraphicTypeUnknown {
		vm.Graphic.Type = compute.GraphicTypeNone
	}

	return vm, nil
}
