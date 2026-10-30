package libvirt

import (
	xml "libvirt.org/go/libvirtxml"
	"strings"
	"subuk/vmango/compute"
	"testing"
)

func TestSchedulerMetadataRoundTripPreservesOtherNamespaces(t *testing.T) {
	d := &xml.Domain{Metadata: &xml.DomainMetadata{XML: `<other xmlns="urn:other"><value>unchanged</value></other>`}}
	m := &compute.MachineMetadata{Cloud: "cloud", Zone: "z", InstanceType: "small", Image: "image", CPURequest: 2, MemoryRequestBytes: 4 << 30}
	if err := writeSchedulerMetadata(d, m); err != nil {
		t.Fatal(err)
	}
	m.CPURequest = 1
	if err := writeSchedulerMetadata(d, m); err != nil {
		t.Fatal(err)
	}
	actual, err := readSchedulerMetadata(d)
	if err != nil || actual.CPURequest != 1 || actual.InstanceType != "small" || !strings.Contains(d.Metadata.XML, "unchanged") || strings.Count(d.Metadata.XML, "<allocation") != 1 {
		t.Fatalf("metadata corrupted: %s %v", d.Metadata.XML, err)
	}
	actual.CPURequest = 9
	again, _ := readSchedulerMetadata(d)
	if again.CPURequest != 1 {
		t.Fatal("metadata was not a stored snapshot")
	}
}
func TestSchedulerMetadataRejectsCorruptionRatherThanLegacyFallback(t *testing.T) {
	for _, body := range []string{`<allocation xmlns="https://vmango.org/xmlns/scheduler/1" version="7">{}</allocation>`, `<allocation xmlns="https://vmango.org/xmlns/scheduler/1" version="1">invalid</allocation>`} {
		if m, err := readSchedulerMetadata(&xml.Domain{Metadata: &xml.DomainMetadata{XML: body}}); err == nil || m != nil {
			t.Fatal("corrupt metadata ignored")
		}
	}
	if m, err := readSchedulerMetadata(&xml.Domain{}); err != nil || m != nil {
		t.Fatal("legacy domain needs no metadata")
	}
}

func TestLogicalNetworkIdentitySurvivesBindingChanges(t *testing.T) {
	for _, bridge := range []bool{false, true} {
		d := &xml.Domain{Devices: &xml.DomainDeviceList{}}
		repo := &VirtualMachineRepository{}
		nic := &compute.VirtualMachineAttachedInterface{LogicalNetwork: "private-lan", NetworkName: "actual-network", Bridge: bridge, Model: "virtio", Mac: "52:54:00:00:00:01"}
		if err := repo.attachInterface(d, nic); err != nil {
			t.Fatal(err)
		}
		actual := VirtualMachineAttachedInterfaceFromInterfaceConfig(d.Devices.Interfaces[0])
		if actual.LogicalNetwork != "private-lan" || actual.NetworkName != "actual-network" || actual.Bridge != bridge {
			t.Fatalf("logical network lost: %+v", actual)
		}
		if logicalNetworkAlias("private-lan") == d.Devices.Interfaces[0].Alias.Name {
			t.Fatal("duplicate interface aliases")
		}
	}
}
