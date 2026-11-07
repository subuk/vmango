package libvirt

import (
	"fmt"
	xml "libvirt.org/go/libvirtxml"
	"subuk/vmango/compute"
	"testing"
)

func dependencyVolume(p, format, backing, backingFormat string) *xml.StorageVolume {
	v := &xml.StorageVolume{Target: &xml.StorageVolumeTarget{Path: p, Format: &xml.StorageVolumeTargetFormat{Type: format}}}
	if backing != "" {
		v.BackingStore = &xml.StorageVolumeBackingStore{Path: backing, Format: &xml.StorageVolumeTargetFormat{Type: backingFormat}}
	}
	return v
}
func dependencyFixture(volumes ...*xml.StorageVolume) *dependencyScan {
	byPath := map[string]*xml.StorageVolume{}
	for _, v := range volumes {
		byPath[v.Target.Path] = v
	}
	return newDependencyScan(func(p string) (*xml.StorageVolume, error) {
		if v := byPath[p]; v != nil {
			return v, nil
		}
		return nil, fmt.Errorf("missing metadata")
	}, func(s *xml.DomainDiskSource) (string, error) {
		if s.File == nil {
			return "", fmt.Errorf("unsupported source")
		}
		return s.File.File, nil
	})
}
func dependencyDomain(id, p string, readonly bool) *xml.Domain {
	disk := xml.DomainDisk{Device: "disk", Source: &xml.DomainDiskSource{File: &xml.DomainDiskSourceFile{File: p}}}
	if readonly {
		disk.ReadOnly = &xml.DomainDiskReadOnly{}
	}
	return &xml.Domain{UUID: id, Devices: &xml.DomainDeviceList{Disks: []xml.DomainDisk{disk}}}
}

func TestDependenciesIncludeUnattachedAndStoppedDisks(t *testing.T) {
	base := dependencyVolume("/pool/base", "raw", "", "")
	child := dependencyVolume("/pool/child", "qcow2", "base", "raw")
	other := dependencyVolume("/pool/other", "qcow2", "/pool/base", "raw")
	scan := dependencyFixture(base, child, other)
	if err := scan.addVolume(other); err != nil {
		t.Fatal(err)
	}
	// Persistent stopped/unmanaged VM XML can contain only its top-level disk.
	if err := scan.addDomain(dependencyDomain("unmanaged", child.Target.Path, false)); err != nil {
		t.Fatal(err)
	}
	if err := scan.addDomain(dependencyDomain("base-writer", base.Target.Path, false)); err != nil {
		t.Fatal(err)
	}
	if err := scan.addDomain(dependencyDomain("base-reader", base.Target.Path, true)); err != nil {
		t.Fatal(err)
	}
	if !scan.state.Referenced(base.Target.Path) || len(scan.state.Dependencies) != 2 {
		t.Fatalf("lost dependencies: %+v", scan.state)
	}
	if len(scan.state.WritableDisks["base-reader"]) != 0 || len(scan.state.WritableDisks["base-writer"]) != 1 {
		t.Fatalf("wrong write access: %+v", scan.state.WritableDisks)
	}
	if scan.state.Referenced(child.Target.Path) {
		t.Fatal("leaf incorrectly considered base")
	}
	if err := scan.state.ValidateUnreferenced([]string{base.Target.Path}); err == nil {
		t.Fatal("referenced source permitted for mutation")
	}
	if err := scan.state.ValidateUnreferenced([]string{child.Target.Path}); err != nil {
		t.Fatal(err)
	}
}

func TestDependenciesRejectDeepMissingAndInconsistentMetadata(t *testing.T) {
	base := dependencyVolume("/base", "raw", "", "")
	child := dependencyVolume("/child", "qcow2", "/base", "raw")
	deep := dependencyVolume("/deep", "qcow2", "/child", "qcow2")
	for _, test := range []struct {
		name    string
		volumes []*xml.StorageVolume
		target  *xml.StorageVolume
	}{
		{"deeper", []*xml.StorageVolume{base, child, deep}, deep},
		{"missing base", []*xml.StorageVolume{child}, child},
		{"wrong format", []*xml.StorageVolume{base}, dependencyVolume("/bad", "qcow2", "/base", "qcow2")},
		{"missing format", []*xml.StorageVolume{base}, dependencyVolume("/bad", "qcow2", "/base", "")},
		{"cycle", nil, dependencyVolume("/cycle", "qcow2", "/cycle", "qcow2")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := dependencyFixture(test.volumes...).addVolume(test.target); err == nil {
				t.Fatal("accepted unsupported/incomplete dependency scan")
			}
		})
	}
	if err := dependencyFixture().addDomain(dependencyDomain("foreign", "/unavailable", false)); err == nil {
		t.Fatal("accepted unresolved unmanaged disk")
	}
}

func TestDependenciesUseLiveChainAndRejectDeeperLiveChain(t *testing.T) {
	base := dependencyVolume("/base", "raw", "", "")
	child := dependencyVolume("/child", "qcow2", "", "")
	domain := dependencyDomain("live", "/child", false)
	domain.Devices.Disks[0].BackingStore = &xml.DomainDiskBackingStore{Format: &xml.DomainDiskFormat{Type: "raw"}, Source: &xml.DomainDiskSource{File: &xml.DomainDiskSourceFile{File: "/base"}}}
	scan := dependencyFixture(base, child)
	if err := scan.addDomain(domain); err != nil {
		t.Fatal(err)
	}
	if !scan.state.Referenced("/base") {
		t.Fatal("ignored live backing chain")
	}
	domain.Devices.Disks[0].BackingStore.BackingStore = &xml.DomainDiskBackingStore{Source: &xml.DomainDiskSource{File: &xml.DomainDiskSourceFile{File: "/older"}}}
	if err := dependencyFixture(base, child).addDomain(domain); err == nil {
		t.Fatal("accepted deeper live backing chain")
	}
}

func TestBackingStoreRelativePathAndActualFormat(t *testing.T) {
	base := backingStore(dependencyVolume("/pool/child", "qcow2", "../images/base", "raw"))
	if base.Path != "/images/base" || base.Format != compute.VolumeFormatRaw {
		t.Fatalf("incorrect metadata: %+v", base)
	}
}

func TestDependencySourceAliasUsesBackendPath(t *testing.T) {
	base := dependencyVolume("/pool/base", "raw", "", "")
	child := dependencyVolume("/pool/child", "qcow2", "/alias/base", "raw")
	scan := dependencyFixture(base, child)
	lookup := scan.lookup
	scan.lookup = func(p string) (*xml.StorageVolume, error) {
		if p == "/alias/base" {
			return base, nil
		}
		return lookup(p)
	}
	if err := scan.addVolume(child); err != nil {
		t.Fatal(err)
	}
	if !scan.state.Referenced("/alias/base") || !scan.state.Referenced("/pool/base") {
		t.Fatalf("alias lost dependency: %+v", scan.state)
	}
}

func TestDependenciesRecordDisklessMachine(t *testing.T) {
	scan := dependencyFixture()
	if err := scan.addDomain(&xml.Domain{UUID: "diskless"}); err != nil {
		t.Fatal(err)
	}
	if _, exists := scan.state.WritableDisks["diskless"]; !exists {
		t.Fatal("diskless machine mistaken for missing machine")
	}
}

func TestDependenciesParseEmptyBackingTerminators(t *testing.T) {
	base := dependencyVolume("/base", "raw", "", "")
	child := dependencyVolume("/child", "qcow2", "/base", "raw")
	for _, body := range []string{
		`<disk type='file' device='disk'><source file='/base'/><backingStore/></disk>`,
		`<disk type='file' device='disk'><source file='/child'/><backingStore type='file'><format type='raw'/><source file='/base'/><backingStore/></backingStore></disk>`,
	} {
		var domain xml.Domain
		if err := domain.Unmarshal(`<domain><uuid>vm</uuid><devices>` + body + `</devices></domain>`); err != nil {
			t.Fatal(err)
		}
		scan := dependencyFixture(base, child)
		if err := scan.addDomain(&domain); err != nil {
			t.Fatalf("empty terminator rejected: %v", err)
		}
	}
}
