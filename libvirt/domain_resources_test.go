package libvirt

import (
	"strings"
	"subuk/vmango/compute"
	"testing"

	libvirtxml "libvirt.org/go/libvirtxml"
)

func TestResourceChangeOnAdoptedDomainPreservesUnrelatedXML(t *testing.T) {
	caps := &libvirtxml.Caps{}
	if err := caps.Unmarshal(`<capabilities><host><cpu><topology sockets="1" cores="4" threads="2"/></cpu></host></capabilities>`); err != nil {
		t.Fatal(err)
	}
	for _, cpuXML := range []string{"", `<cpu mode="host-passthrough"><feature policy="require" name="aes"/></cpu>`} {
		t.Run(cpuXML, func(t *testing.T) {
			config := &libvirtxml.Domain{}
			if err := config.Unmarshal(`<domain type="kvm"><name>adopted</name>` + cpuXML + `<memoryBacking><locked/><source type="memfd"/><hugepages><page size="2048" unit="KiB"/></hugepages></memoryBacking></domain>`); err != nil {
				t.Fatal(err)
			}
			vm := &compute.VirtualMachine{VCpus: 4, Memory: compute.NewSize(4, compute.SizeUnitG), Hugepages: true}
			applyDomainResources(config, vm, caps)
			if config.CPU == nil || config.CPU.Topology == nil || config.CPU.Topology.Cores != 2 || config.CurrentMemory.Value != 4<<30 {
				t.Fatalf("resource sizing was not applied: %+v", config)
			}
			if cpuXML != "" && (config.CPU.Mode != "host-passthrough" || len(config.CPU.Features) != 1) {
				t.Fatal("existing CPU policy changed")
			}
			if len(config.MemoryBacking.MemoryHugePages.Hugepages) != 1 {
				t.Fatal("existing hugepage configuration changed")
			}
			vm.Hugepages = false
			applyDomainResources(config, vm, caps)
			xml, err := config.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(xml, "<hugepages") || !strings.Contains(xml, "<locked") || !strings.Contains(xml, `type="memfd"`) {
				t.Fatalf("memory backing settings were lost: %s", xml)
			}
		})
	}
}
