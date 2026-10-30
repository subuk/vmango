package libvirt

import (
	"subuk/vmango/compute"

	libvirtxml "libvirt.org/go/libvirtxml"
)

func applyDomainResources(config *libvirtxml.Domain, vm *compute.VirtualMachine, caps *libvirtxml.Caps) {
	config.VCPU = &libvirtxml.DomainVCPU{Placement: "static", Value: uint(vm.VCpus)}
	config.Memory = &libvirtxml.DomainMemory{Unit: "bytes", Value: uint(vm.Memory.Bytes())}
	config.CurrentMemory = &libvirtxml.DomainCurrentMemory{Unit: "bytes", Value: uint(vm.Memory.Bytes())}
	if vm.Hugepages {
		if config.MemoryBacking == nil {
			config.MemoryBacking = &libvirtxml.DomainMemoryBacking{}
		}
		if config.MemoryBacking.MemoryHugePages == nil {
			config.MemoryBacking.MemoryHugePages = &libvirtxml.DomainMemoryHugepages{}
		}
	} else if config.MemoryBacking != nil {
		config.MemoryBacking.MemoryHugePages = nil
	}
	if caps.Host.CPU == nil || caps.Host.CPU.Topology == nil || caps.Host.CPU.Topology.Threads < 1 {
		return
	}
	// Adopted domains may omit CPU configuration entirely. Preserve any
	// existing CPU model and features while applying the new resource sizing.
	if config.CPU == nil {
		config.CPU = &libvirtxml.DomainCPU{}
	}
	threads := caps.Host.CPU.Topology.Threads
	if vm.VCpus%threads == 0 {
		config.CPU.Topology = &libvirtxml.DomainCPUTopology{Sockets: VirtualMachineSockets, Cores: vm.VCpus / threads, Threads: threads}
	} else {
		config.CPU.Topology = nil
	}
}

// Type changes update resource policy without rebuilding existing guest devices.
func applyDomainPolicy(virDomainConfig *libvirtxml.Domain, vm *compute.VirtualMachine, capsConfig *libvirtxml.Caps, resourcesOnly bool) {
	applyDomainResources(virDomainConfig, vm, capsConfig)
	if resourcesOnly {
		return
	}

	if vm.GuestAgent {
		hasGuestAgent := false
		for _, channel := range virDomainConfig.Devices.Channels {
			if channel.Target != nil && channel.Target.VirtIO != nil && channel.Target.VirtIO.Name == "org.qemu.guest_agent.0" {
				hasGuestAgent = true
				break
			}
		}
		if !hasGuestAgent {
			virDomainConfig.Devices.Channels = append(virDomainConfig.Devices.Channels, libvirtxml.DomainChannel{
				Protocol: &libvirtxml.DomainChardevProtocol{Type: "unix"},
				Target:   &libvirtxml.DomainChannelTarget{VirtIO: &libvirtxml.DomainChannelTargetVirtIO{Name: "org.qemu.guest_agent.0"}},
				Source:   &libvirtxml.DomainChardevSource{UNIX: &libvirtxml.DomainChardevSourceUNIX{}},
			})
		}
	} else {
		newChannels := []libvirtxml.DomainChannel{}
		for _, channel := range virDomainConfig.Devices.Channels {
			if channel.Target != nil && channel.Target.VirtIO != nil && channel.Target.VirtIO.Name == "org.qemu.guest_agent.0" {
				continue
			}
			newChannels = append(newChannels, channel)
		}
		virDomainConfig.Devices.Channels = newChannels
	}

	switch vm.Graphic.Type {
	default:
		panic("unknown graphic type")
	case compute.GraphicTypeNone:
		virDomainConfig.Devices.Graphics = nil
	case compute.GraphicTypeVnc:
		vncGraphic := &libvirtxml.DomainGraphicVNC{Port: -1, AutoPort: "yes", Listen: vm.Graphic.Listen}
		virDomainConfig.Devices.Graphics = []libvirtxml.DomainGraphic{
			libvirtxml.DomainGraphic{
				VNC: vncGraphic,
			},
		}
	case compute.GraphicTypeSpice:
		spiceGraphic := &libvirtxml.DomainGraphicSpice{Port: -1, AutoPort: "yes", Listen: vm.Graphic.Listen}
		virDomainConfig.Devices.Graphics = []libvirtxml.DomainGraphic{
			libvirtxml.DomainGraphic{
				Spice: spiceGraphic,
			},
		}
	}

	switch vm.VideoModel {
	default:
		panic("unknown video model")
	case compute.VideoModelQxl:
		virDomainConfig.Devices.Videos = []libvirtxml.DomainVideo{
			libvirtxml.DomainVideo{Model: libvirtxml.DomainVideoModel{Type: "qxl"}},
		}
	case compute.VideoModelCirrus:
		virDomainConfig.Devices.Videos = []libvirtxml.DomainVideo{
			libvirtxml.DomainVideo{Model: libvirtxml.DomainVideoModel{Type: "cirrus"}},
		}
	case compute.VideoModelNone:
		virDomainConfig.Devices.Videos = nil
	}

}
