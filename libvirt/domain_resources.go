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
