package libvirt

import (
	"encoding/json"
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

func TestTypeChangePreservesBackendGuestDevices(t *testing.T) {
	xml := `<domain type="kvm"><name>adopted</name><memory unit="KiB">1048576</memory><vcpu>1</vcpu><os><type arch="x86_64">hvm</type></os><devices><graphics type="vnc" port="5901" passwd="secret" tlsPort="5902" listen="127.0.0.2"><listen type="address" address="127.0.0.2"/></graphics><video><model type="qxl" ram="65536" vram="65536" heads="2" primary="yes"/></video><channel type="unix"><source mode="bind" path="/guest/agent.sock"/><target type="virtio" name="org.qemu.guest_agent.0"/></channel><disk type="file" device="disk"><source file="/guest/root.qcow2"/><target dev="vda" bus="virtio"/></disk></devices></domain>`
	var config libvirtxml.Domain
	if err := config.Unmarshal(xml); err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(config.Devices)
	if err != nil {
		t.Fatal(err)
	}
	vm := &compute.VirtualMachine{Id: "adopted", VCpus: 2, Memory: compute.NewSize(2, compute.SizeUnitG), Autostart: true, GuestAgent: false, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone}
	applyDomainPolicy(&config, vm, &libvirtxml.Caps{}, true)
	after, err := json.Marshal(config.Devices)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("instance type rewrote devices:\nbefore=%s\nafter=%s", before, after)
	}
	if config.VCPU.Value != 2 || config.Memory.Value == 1048576 {
		t.Fatal("resource policy was not updated")
	}
}
