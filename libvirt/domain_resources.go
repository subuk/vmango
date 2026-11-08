package libvirt

import (
	"fmt"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"

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
		if plan, err := hardware.DecodePlan(vm.AppliedPolicy); err == nil && plan.HugepageSizeKiB > 0 && len(config.MemoryBacking.MemoryHugePages.Hugepages) == 0 {
			config.MemoryBacking.MemoryHugePages.Hugepages = []libvirtxml.DomainMemoryHugepage{{Size: uint(plan.HugepageSizeKiB), Unit: "KiB"}}
		}
	} else if config.MemoryBacking != nil {
		config.MemoryBacking.MemoryHugePages = nil
	}
	threads := 0
	if plan, err := hardware.DecodePlan(vm.AppliedPolicy); err == nil {
		threads = plan.CPUThreads
	}
	if threads == 0 && caps.Host.CPU != nil && caps.Host.CPU.Topology != nil {
		threads = caps.Host.CPU.Topology.Threads
	}
	if threads < 1 {
		return
	}
	if config.CPU == nil {
		config.CPU = &libvirtxml.DomainCPU{}
	}

	if vm.VCpus%threads == 0 {
		config.CPU.Topology = &libvirtxml.DomainCPUTopology{Sockets: VirtualMachineSockets, Cores: vm.VCpus / threads, Threads: threads}
	} else {
		config.CPU.Topology = nil
	}
}

// Type changes update resource policy without rebuilding existing guest devices.
func applyDomainPolicy(virDomainConfig *libvirtxml.Domain, vm *compute.VirtualMachine, capsConfig *libvirtxml.Caps, resourcesOnly bool) {
	var previous *libvirtxml.DomainCPU
	var previousVCPU *libvirtxml.DomainVCPU
	if resourcesOnly && virDomainConfig.VCPU != nil {
		copy := *virDomainConfig.VCPU
		previousVCPU = &copy
	}
	if resourcesOnly && virDomainConfig.CPU != nil {
		copy := *virDomainConfig.CPU
		previous = &copy
		if copy.Topology != nil {
			topology := *copy.Topology
			previous.Topology = &topology
		}
	}
	applyDomainResources(virDomainConfig, vm, capsConfig)
	if resourcesOnly {
		if previousVCPU != nil {
			if previousVCPU.Current == previousVCPU.Value {
				previousVCPU.Current = uint(vm.VCpus)
			}
			previousVCPU.Value = uint(vm.VCpus)
			virDomainConfig.VCPU = previousVCPU
		}
		virDomainConfig.CPU = previous
		if previous != nil && previous.Topology != nil {
			factor := topologyFactor(previous.Topology)
			if factor > 0 && vm.VCpus%factor == 0 {
				previous.Topology.Cores = vm.VCpus / factor
			}
		}
		return
	}

	if vm.GuestAgent {
		hasController := false
		for _, c := range virDomainConfig.Devices.Controllers {
			if c.Type == "virtio-serial" {
				hasController = true
			}
		}
		if !hasController {
			virDomainConfig.Devices.Controllers = append(virDomainConfig.Devices.Controllers, libvirtxml.DomainController{Type: "virtio-serial", Index: uintPointer(0)})
		}
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
	case compute.VideoModelVirtio:
		virDomainConfig.Devices.Videos = []libvirtxml.DomainVideo{
			{Model: libvirtxml.DomainVideoModel{Type: "virtio"}},
		}
	case compute.VideoModelNone:
		virDomainConfig.Devices.Videos = []libvirtxml.DomainVideo{{Model: libvirtxml.DomainVideoModel{Type: "none"}}}
	}

}

func topologyFactor(t *libvirtxml.DomainCPUTopology) int {
	return max(t.Sockets, 1) * max(t.Dies, 1) * max(t.Clusters, 1) * max(t.Threads, 1)
}
func validateResourceTopology(c *libvirtxml.Domain, vcpus int) error {
	if c.VCPU != nil && int(c.VCPU.Value) != vcpus {
		if c.VCPU.CPUSet != "" {
			return &compute.ValidationError{Field: "instance_type", Message: "Changing the vCPU count with explicit CPU affinity is unsupported; keep a compatible instance type."}
		}
		if c.VCPU.Current != 0 && c.VCPU.Current != c.VCPU.Value || c.VCPUs != nil {
			return &compute.ValidationError{Field: "instance_type", Message: "Changing the vCPU count with an explicit CPU hotplug layout is unsupported."}
		}
	}
	if c.CPUTune != nil && (len(c.CPUTune.VCPUPin) > 0 || c.CPUTune.EmulatorPin != nil) && c.VCPU != nil && int(c.VCPU.Value) != vcpus {
		return fmt.Errorf("explicit CPU pinning requires preserving vCPU count")
	}
	if c.CPU != nil && c.CPU.Topology != nil {
		factor := topologyFactor(c.CPU.Topology)
		if factor <= 0 || vcpus%factor != 0 {
			return fmt.Errorf("vCPU count %d is incompatible with the applied CPU sockets/dies/clusters/threads topology", vcpus)
		}
	}
	return nil
}
