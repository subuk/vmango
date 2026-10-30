package compute_test

import (
	"fmt"
	"subuk/vmango/catalog"
	"subuk/vmango/compute"
	"subuk/vmango/configdrive"
	"testing"
)

type volumes struct {
	compute.VolumeRepository
	source    compute.Volume
	missing   bool
	reads     []string
	mutations int
}

func (v *volumes) Get(path, node string) (*compute.Volume, error) {
	v.reads = append(v.reads, node+":"+path)
	if v.missing {
		return nil, fmt.Errorf("not found")
	}
	s := v.source
	return &s, nil
}
func (v *volumes) ValidateCreate(compute.VolumeCreateParams) error { return nil }
func (v *volumes) Clone(p compute.VolumeCloneParams) (*compute.Volume, error) {
	v.mutations++
	return nil, fmt.Errorf("unexpected mutation")
}
func (v *volumes) Create(p compute.VolumeCreateParams) (*compute.Volume, error) {
	v.mutations++
	return nil, fmt.Errorf("unexpected mutation")
}

type machines struct {
	compute.VirtualMachineRepository
	caps compute.VirtualMachineCapabilities
}

func (v machines) ValidateNew(*compute.VirtualMachine) error { return nil }
func (v machines) Capabilities(string, compute.Arch) (compute.VirtualMachineCapabilities, error) {
	return v.caps, nil
}

func setup(t *testing.T, defs ...compute.Image) (*compute.VirtualMachineManager, *volumes, *compute.VirtualMachine, []compute.VirtualMachineManagerClonedVolumeParams) {
	t.Helper()
	if len(defs) == 0 {
		defs = []compute.Image{{ID: "default", Name: "Default", SourcePath: "/base", Arch: compute.ArchAmd64}}
	}
	repo, err := catalog.NewImageRepository(defs)
	if err != nil {
		t.Fatal(err)
	}
	vol := &volumes{source: compute.Volume{NodeId: "selected", Path: "/base", Size: compute.NewSize(4, compute.SizeUnitG), Format: compute.VolumeFormatQcow2}}
	caps := compute.VirtualMachineCapabilities{Arch: compute.ArchAmd64, MaxVCPUs: 8, Graphics: []compute.GraphicType{compute.GraphicTypeNone, compute.GraphicTypeVnc}, VideoModels: []compute.VideoModel{compute.VideoModelNone}, Firmwares: []string{"bios", "efi"}, DiskBuses: []compute.DeviceBus{compute.DeviceBusVirtio, compute.DeviceBusScsi}, NICModels: []string{"virtio", "e1000"}, DiskControllers: []string{"virtio-scsi"}}
	m := compute.NewVirtualMachineManager(compute.NewVirtualMachineService(machines{caps: caps}), compute.NewVolumeService(vol), nil, map[string]compute.VirtualMachineManagerNodeSettings{"selected": {CdFormat: configdrive.FormatOpenstack}}, compute.NewImageService(repo))
	vm := &compute.VirtualMachine{Id: "test", Name: "test", NodeId: "selected", Arch: compute.ArchAmd64, VCpus: 2, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone, Memory: compute.NewSize(2, compute.SizeUnitG), Config: &compute.VirtualMachineConfig{Hostname: "test"}, Interfaces: []*compute.VirtualMachineAttachedInterface{{NetworkName: "default"}}}
	clone := []compute.VirtualMachineManagerClonedVolumeParams{{ImageID: defs[0].ID, NewName: "test_root", NewPool: "pool"}}
	return m, vol, vm, clone
}

func TestImageDefaultResolution(t *testing.T) {
	m, vol, vm, clone := setup(t)
	prepared, disks, err := m.PrepareImages(vm, clone, nil, compute.ImageDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Graphic.Type != compute.GraphicTypeVnc || prepared.VideoModel != compute.VideoModelNone || !prepared.GuestAgent || prepared.Config.Initialization != "openstack" || prepared.Interfaces[0].Model != "virtio" || disks[0].DeviceBus != compute.DeviceBusVirtio || disks[0].NewSize.G() != 4 || disks[0].NewName != "test_root" {
		t.Fatalf("defaults: %+v %+v", prepared, disks)
	}
	if len(vol.reads) != 1 || vol.reads[0] != "selected:/base" {
		t.Fatalf("reads %v", vol.reads)
	}
	if vm.GuestAgent || vm.Interfaces[0].Model != "" || clone[0].OriginalPath != "" || vm.Config.Initialization != "" {
		t.Fatal("input was mutated")
	}
}

func TestImageExplicitDefaultsAndOverrides(t *testing.T) {
	off := false
	defs := []compute.Image{{ID: "custom", SourcePath: "/base", Arch: compute.ArchAmd64, MinimumDiskBytes: 8 << 30, Defaults: compute.ImageDefaults{Firmware: "efi", DiskBus: compute.DeviceBusScsi, DiskController: "virtio-scsi", NICModel: "e1000", GuestAgent: &off, Initialization: "none"}}}
	m, _, vm, clones := setup(t, defs...)
	prepared, disks, err := m.PrepareImages(vm, clones, nil, compute.ImageDefaults{})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.GuestAgent || prepared.Config != nil || prepared.Firmware != "efi" || prepared.DiskController != "virtio-scsi" || prepared.Interfaces[0].Model != "e1000" || disks[0].NewSize.G() != 8 {
		t.Fatalf("explicit defaults lost: %+v %+v", prepared, disks)
	}
	on := true
	vm.Interfaces[0].Model = "virtio"
	clones[0].DeviceBus = compute.DeviceBusVirtio
	prepared, disks, err = m.PrepareImages(vm, clones, nil, compute.ImageDefaults{Firmware: "bios", GuestAgent: &on, Initialization: "nocloud"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Graphic.Type != compute.GraphicTypeVnc || prepared.VideoModel != compute.VideoModelNone || !prepared.GuestAgent || prepared.Config.Initialization != "nocloud" || prepared.Firmware != "bios" || prepared.DiskController != "" || disks[0].DeviceBus != compute.DeviceBusVirtio || prepared.Interfaces[0].Model != "virtio" {
		t.Fatal("overrides lost")
	}
}

func TestImageFailuresBeforeMutation(t *testing.T) {
	for _, name := range []string{"missing", "wrong node", "hidden", "attached", "small disk", "architecture", "unsupported bus", "bad cpu", "unknown image", "bad second clone", "missing node", "missing name"} {
		t.Run(name, func(t *testing.T) {
			m, vol, vm, clones := setup(t)
			switch name {
			case "missing node":
				vm.NodeId = ""
			case "missing name":
				vm.Id = ""
			case "missing":
				vol.missing = true
			case "wrong node":
				vol.source.NodeId = "other"
			case "hidden":
				vol.source.Metadata.Hidden = true
			case "attached":
				vol.source.AttachedTo = "existing"
			case "small disk":
				clones[0].NewSize = compute.NewSize(1, compute.SizeUnitG)
			case "architecture":
				vm.Arch = compute.ArchAarch64
			case "unsupported bus":
				clones[0].DeviceBus = compute.DeviceBusIde
			case "bad cpu":
				vm.VCpus = 100
			case "unknown image":
				clones[0].ImageID = "missing"
			case "bad second clone":
				clones = append(clones, clones[0])
				clones[1].ImageID = "missing"
			}
			if err := m.Create(vm, clones, nil, false); err == nil {
				t.Fatal("invalid request accepted")
			}
			if vol.mutations != 0 {
				t.Fatal("mutated storage before validation")
			}
		})
	}
}

func TestImageAliasesAndConflicts(t *testing.T) {
	off := false
	a := compute.Image{ID: "a", SourcePath: "/base", Arch: compute.ArchAmd64, Defaults: compute.ImageDefaults{Initialization: "nocloud"}}
	b := a
	b.ID = "b"
	b.Defaults.Initialization = "none"
	b.Defaults.GuestAgent = &off
	m, _, vm, clones := setup(t, a, b)
	clones[0].ImageID = "b"
	resolved, _, err := m.PrepareImages(vm, clones, nil, compute.ImageDefaults{})
	if err != nil || resolved.Config != nil || resolved.GuestAgent {
		t.Fatalf("selected alias not applied: %v", err)
	}
	second := clones[0]
	second.ImageID = "a"
	second.NewName = "other"
	clones = append(clones, second)
	if _, _, err := m.PrepareImages(vm, clones, nil, compute.ImageDefaults{}); err == nil {
		t.Fatal("conflicting initialization accepted")
	}
	if _, _, err := m.PrepareImages(vm, clones, nil, compute.ImageDefaults{Initialization: "none"}); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogAvailability(t *testing.T) {
	a := compute.Image{ID: "a", SourcePath: "/base", Arch: compute.ArchAmd64}
	b := a
	b.ID = "b"
	repo, err := catalog.NewImageRepository([]compute.Image{a, b})
	if err != nil {
		t.Fatal(err)
	}
	service := compute.NewImageService(repo)
	for _, node := range []string{"one", "two"} {
		got, err := service.Available(node, compute.ArchAmd64, []*compute.Volume{{Path: "/base", NodeId: node}})
		if err != nil || len(got) != 2 {
			t.Fatalf("shared catalog on %s: %v %v", node, got, err)
		}
	}
	got, _ := service.Available("one", compute.ArchAmd64, []*compute.Volume{{Path: "/base", NodeId: "two"}})
	if len(got) != 0 {
		t.Fatal("another node made the image available")
	}
}

func TestManualCreationPreserved(t *testing.T) {
	m, _, vm, _ := setup(t)
	vm.GuestAgent = false
	vm.Config = nil
	clones := []compute.VirtualMachineManagerClonedVolumeParams{{OriginalPath: "/ordinary", NewName: "copy", NewPool: "pool", DeviceBus: compute.DeviceBusIde}}
	prepared, disks, err := m.PrepareImages(vm, clones, nil, compute.ImageDefaults{})
	if err != nil || prepared.Config != nil || prepared.GuestAgent || disks[0].OriginalPath != "/ordinary" || disks[0].DeviceBus != compute.DeviceBusIde {
		t.Fatalf("manual workflow changed: %v", err)
	}
	clones[0].OriginalPath = "/base"
	if _, _, err := m.PrepareImages(vm, clones, nil, compute.ImageDefaults{}); err == nil {
		t.Fatal("catalog image rules bypassed with source path")
	}
}

func TestExplicitInterfacesOverrideConflictingNICDefaults(t *testing.T) {
	a := compute.Image{ID: "a", SourcePath: "/base", Arch: compute.ArchAmd64, Defaults: compute.ImageDefaults{NICModel: "virtio"}}
	b := a
	b.ID = "b"
	b.Defaults.NICModel = "e1000"
	m, _, vm, clones := setup(t, a, b)
	second := clones[0]
	second.ImageID = "b"
	second.NewName = "second"
	clones = append(clones, second)
	vm.Interfaces[0].Model = "virtio"
	if _, _, err := m.PrepareImages(vm, clones, nil, compute.ImageDefaults{}); err != nil {
		t.Fatal(err)
	}
}
