package libvirt

import (
	xml "libvirt.org/go/libvirtxml"
	"strings"
	"subuk/vmango/compute"
	"testing"
)

func TestSchedulerMetadataRoundTripPreservesOtherNamespaces(t *testing.T) {
	const other = `<other xmlns="urn:other" xmlns:ref="urn:references" ref:type="ref:owner"><value>unchanged</value></other>`
	d := &xml.Domain{Metadata: &xml.DomainMetadata{XML: other}}
	m := &compute.MachineMetadata{Cloud: "cloud", Zone: "z", InstanceType: "small", Image: "image", CPURequest: 2, MemoryRequestBytes: 4 << 30}
	if err := writeSchedulerMetadata(d, m); err != nil {
		t.Fatal(err)
	}
	m.CPURequest = 1
	if err := writeSchedulerMetadata(d, m); err != nil {
		t.Fatal(err)
	}
	actual, err := readSchedulerMetadata(d)
	if err != nil || actual.CPURequest != 1 || actual.InstanceType != "small" || !strings.Contains(d.Metadata.XML, other) || strings.Count(d.Metadata.XML, "<allocation") != 1 {
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
