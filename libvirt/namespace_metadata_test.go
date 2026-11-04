package libvirt

import (
	"strings"
	"subuk/vmango/compute"
	"testing"

	xmlmodel "libvirt.org/go/libvirtxml"
)

func TestNamespaceMetadataPreservesSchedulerAndForeignXML(t *testing.T) {
	const foreign = `<foreign xmlns="urn:other" xmlns:r="urn:reference" r:type="r:Owner"><value>unchanged</value></foreign>`
	d := &xmlmodel.Domain{Metadata: &xmlmodel.DomainMetadata{XML: foreign}}
	scheduler := &compute.MachineMetadata{Cloud: "main", Zone: "zone", CPURequest: 1, MemoryRequestBytes: 1024}
	if err := writeSchedulerMetadata(d, scheduler); err != nil {
		t.Fatal(err)
	}
	before := d.Metadata.XML
	for _, label := range []string{"same display label", "unicode < & Пример"} {
		if err := writeWorkloadMetadata(d, "platform", label); err != nil {
			t.Fatal(err)
		}
		id, gotLabel, err := readWorkloadMetadata(d)
		if err != nil || id != "platform" || gotLabel != label {
			t.Fatalf("metadata did not round trip: %q %q %v", id, gotLabel, err)
		}
		if !strings.Contains(d.Metadata.XML, before) || strings.Count(d.Metadata.XML, "<workload") != 1 {
			t.Fatalf("unrelated metadata changed: %s", d.Metadata.XML)
		}
	}
	if err := writeWorkloadMetadata(d, "", ""); err != nil {
		t.Fatal(err)
	}
	id, _, err := readWorkloadMetadata(d)
	if err != nil || id != "platform" {
		t.Fatal("omitted assignment erased existing namespace")
	}
	if err := writeSchedulerMetadata(d, scheduler); err != nil {
		t.Fatal(err)
	}
	id, _, err = readWorkloadMetadata(d)
	if err != nil || id != "platform" || !strings.Contains(d.Metadata.XML, foreign) {
		t.Fatal("scheduler update lost workload metadata")
	}
}

func TestNamespaceMetadataMissingVersusCorrupt(t *testing.T) {
	if id, label, err := readWorkloadMetadata(&xmlmodel.Domain{}); err != nil || id != "" || label != "" {
		t.Fatal("legacy domain must be unassigned")
	}
	valid, _ := workloadElement("platform", "label")
	for _, bad := range []string{
		`<broken`,
		strings.Replace(valid, `version="1"`, `version="2"`, 1),
		strings.Replace(valid, `namespace="platform"`, `namespace=""`, 1),
		strings.Replace(valid, `namespace="platform"`, `namespace="bad/id"`, 1),
		strings.Replace(valid, "<label>label</label>", "<label></label>", 1),
		valid + valid,
	} {
		if id, _, err := readWorkloadMetadata(&xmlmodel.Domain{Metadata: &xmlmodel.DomainMetadata{XML: bad}}); err == nil || id != "" {
			t.Fatalf("corrupt membership treated as valid or unassigned: %s", bad)
		}
	}
}
