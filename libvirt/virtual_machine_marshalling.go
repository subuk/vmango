package libvirt

import (
	"encoding/json"
	"fmt"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"

	"libvirt.org/go/libvirt"
	libvirtxml "libvirt.org/go/libvirtxml"
)

func DomainDiskConfigFromVirtualMachineAttachedVolume(volume *compute.VirtualMachineAttachedVolume, volTargetFormatType, volumeType string, namer *DeviceNamer) *libvirtxml.DomainDisk {
	diskDriverType := "raw"
	if volTargetFormatType == "qcow2" {
		diskDriverType = "qcow2"
	}

	diskConfig := &libvirtxml.DomainDisk{
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu", Type: diskDriverType},
		Target: &libvirtxml.DomainDiskTarget{},
	}
	if volume.Alias != "" {
		diskConfig.Alias = &libvirtxml.DomainAlias{Name: "ua-" + volume.Alias}
	}
	switch volume.DeviceType {
	default:
		panic(fmt.Errorf("unsupported volume device type '%s'", volume.DeviceType))
	case compute.DeviceTypeCdrom:
		diskConfig.Device = "cdrom"
		diskConfig.ReadOnly = &libvirtxml.DomainDiskReadOnly{}
		diskConfig.Target.Bus = volume.DeviceBus.String()
		diskConfig.Target.Dev = namer.Next(volume.DeviceBus)
	case compute.DeviceTypeDisk:
		diskConfig.Device = "disk"
		diskConfig.Target.Bus = volume.DeviceBus.String()
		diskConfig.Target.Dev = namer.Next(volume.DeviceBus)
	}
	switch volumeType {
	default:
		panic(fmt.Errorf("unknown volume type '%s'", volumeType))
	case "file":
		diskConfig.Source = &libvirtxml.DomainDiskSource{
			File: &libvirtxml.DomainDiskSourceFile{File: volume.Path},
		}
	case "block":
		diskConfig.Source = &libvirtxml.DomainDiskSource{
			Block: &libvirtxml.DomainDiskSourceBlock{Dev: volume.Path},
		}
	}

	return diskConfig
}

func VirtualMachineAttachedVolumeFromDomainDiskConfig(diskConfig libvirtxml.DomainDisk) *compute.VirtualMachineAttachedVolume {
	volume := &compute.VirtualMachineAttachedVolume{}
	if diskConfig.Target != nil {
		volume.DeviceBus = compute.NewDeviceBus(diskConfig.Target.Bus)
		volume.DeviceID = diskConfig.Target.Dev
	}
	if diskConfig.Driver != nil {
		volume.Format = diskConfig.Driver.Type
	}
	if diskConfig.Alias != nil {
		alias := diskConfig.Alias.Name
		if strings.HasPrefix(alias, "ua-") {
			volume.Alias = alias[3:]
		}
	}

	switch diskConfig.Device {
	default:
		volume.DeviceType = compute.DeviceTypeUnknown
	case "disk":
		volume.DeviceType = compute.DeviceTypeDisk
	case "cdrom":
		volume.DeviceType = compute.DeviceTypeCdrom
	}

	if diskConfig.Source != nil {
		if diskConfig.Source.File != nil {
			volume.Path = diskConfig.Source.File.File
		}
		if diskConfig.Source.Block != nil {
			volume.Path = diskConfig.Source.Block.Dev
		}
	}
	return volume
}

func VirtualMachineAttachedInterfaceFromInterfaceConfig(ifaceConfig libvirtxml.DomainInterface) *compute.VirtualMachineAttachedInterface {
	iface := &compute.VirtualMachineAttachedInterface{}
	if ifaceConfig.MAC != nil {
		iface.Mac = strings.ToLower(ifaceConfig.MAC.Address)
	}
	iface.DeviceID = "mac:" + iface.Mac
	iface.Observation = interfaceObservation(ifaceConfig)
	if ifaceConfig.Model != nil {
		iface.Model = ifaceConfig.Model.Type
	}
	if ifaceConfig.Source != nil {
		if ifaceConfig.Source.Network != nil {
			iface.NetworkName = ifaceConfig.Source.Network.Network
			iface.Binding = &compute.NetworkBinding{Type: compute.NetworkLibvirt, Network: iface.NetworkName}
		}
		if ifaceConfig.Source.Bridge != nil {
			iface.NetworkName = ifaceConfig.Source.Bridge.Bridge
			iface.Bridge = true
			iface.Binding = &compute.NetworkBinding{Type: compute.NetworkBridge, Bridge: iface.NetworkName}
		}
	}

	if ifaceConfig.Source != nil && ifaceConfig.Source.User != nil {
		iface.Binding = &compute.NetworkBinding{Type: compute.NetworkUser}
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
	metadata, err := readSchedulerMetadata(domainConfig)
	if err != nil {
		return nil, err
	}
	vm.ObservedBackend, _ = json.Marshal(hardware.ObservedHardware{Presentation: observeDisplayDevices(domainConfig), Storage: observeStorage(domainConfig), Firmware: observeFirmware(domainConfig), PlatformBinding: domainPlatform(domainConfig), GuestSystem: observeGuestSystem(domainConfig), CPU: observeCPU(domainConfig)})
	vm.Metadata = metadata
	vm.AppliedPolicy, err = readHardwareMetadata(domainConfig)
	if err != nil {
		return nil, err
	}
	vm.Id = domainConfig.UUID
	vm.Name = domainConfig.Name
	vm.NamespaceID, vm.Label, err = readWorkloadMetadata(domainConfig)
	if err != nil {
		return nil, err
	}
	if vm.Label == "" {
		vm.Label = vm.Name
	}
	vm.VCpus = int(domainConfig.VCPU.Value)
	vm.Memory = ComputeSizeFromLibvirtSize(domainConfig.Memory.Unit, uint64(domainConfig.Memory.Value))
	vm.Firmware = observeFirmware(domainConfig).Type

	switch domainConfig.OS.Type.Arch {
	default:
		vm.Arch = compute.ArchUnknown
	case "x86_64":
		vm.Arch = compute.ArchAmd64
	case "aarch64":
		vm.Arch = compute.ArchAarch64
	}

	switch domainInfo.State {
	default:
		vm.State = compute.StateUnknown
	case libvirt.DOMAIN_NOSTATE:
		vm.State = compute.StateUnknown
	case libvirt.DOMAIN_RUNNING:
		vm.State = compute.StateRunning
	case libvirt.DOMAIN_BLOCKED:
		vm.State = compute.StateUnknown
	case libvirt.DOMAIN_PAUSED:
		vm.State = compute.StateUnknown
	case libvirt.DOMAIN_SHUTDOWN:
		vm.State = compute.StateUnknown
	case libvirt.DOMAIN_CRASHED:
		vm.State = compute.StateStopped
	case libvirt.DOMAIN_PMSUSPENDED:
		vm.State = compute.StateUnknown
	case libvirt.DOMAIN_SHUTOFF:
		vm.State = compute.StateStopped
	}

	if domainConfig.VCPU.CPUSet != "" {
		vm.Cpupin = &compute.VirtualMachineCpuPin{Vcpus: map[uint][]uint{}, Emulator: []uint{}}
		cpus := ParseCpuAffinity(domainConfig.VCPU.CPUSet)
		for n := uint(0); n < domainConfig.VCPU.Value; n++ {
			vm.Cpupin.Vcpus[n] = append([]uint(nil), cpus...)
		}
	}
	if domainConfig.CPUTune != nil {
		if vm.Cpupin == nil {
			vm.Cpupin = &compute.VirtualMachineCpuPin{Vcpus: map[uint][]uint{}, Emulator: []uint{}}
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

	if domainConfig.MemoryBacking != nil && domainConfig.MemoryBacking.MemoryHugePages != nil {
		vm.Hugepages = true
	}

	for _, graphic := range domainConfig.Devices.Graphics {
		if graphic.VNC != nil {
			vm.Graphic.Type = compute.GraphicTypeVnc
			vm.Graphic.Listen = graphicsListenerAddress(graphic.VNC.Listen, graphic.VNC.Listeners)
			break
		}
		if graphic.Spice != nil {
			vm.Graphic.Type = compute.GraphicTypeSpice
			vm.Graphic.Listen = graphicsListenerAddress(graphic.Spice.Listen, graphic.Spice.Listeners)
			break
		}
	}
	if vm.Graphic.Type == compute.GraphicTypeUnknown {
		vm.Graphic.Type = compute.GraphicTypeNone
	}

	if len(domainConfig.Devices.Videos) == 0 {
		vm.VideoModel = compute.VideoModelNone
	} else {
		vm.VideoModel = compute.NewVideoModel(domainConfig.Devices.Videos[0].Model.Type)
	}

	vm.Interfaces = append(vm.Interfaces, observeRawInterfaces(domainConfig)...)
	if vm.Metadata != nil {
		_ = compute.ApplyAttachmentMetadata(vm, vm.Metadata.Attachments)
	}

	return vm, nil
}
