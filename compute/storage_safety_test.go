package compute

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

type safetyVolumes struct {
	VolumeRepository
	state   StorageDependencyState
	scanErr error
	scans   int
	writes  int
	source  Volume
	cloned  VolumeCloneParams
}

func (r *safetyVolumes) InspectDependencies(string) (StorageDependencyState, error) {
	r.scans++
	return r.state, r.scanErr
}
func (r *safetyVolumes) Get(path, node string) (*Volume, error) {
	v := r.source
	v.Path = path
	v.NodeId = node
	return &v, nil
}
func (r *safetyVolumes) ValidateCreate(VolumeCreateParams) error { return nil }
func (r *safetyVolumes) Clone(p VolumeCloneParams) (*Volume, error) {
	r.writes++
	r.cloned = p
	return &Volume{Path: "/copy", NodeId: p.NodeId}, nil
}
func (r *safetyVolumes) Resize(string, string, Size) error              { r.writes++; return nil }
func (r *safetyVolumes) Delete(string, string) error                    { r.writes++; return nil }
func (r *safetyVolumes) Upload(string, string, io.Reader, uint64) error { r.writes++; return nil }
func (r *safetyVolumes) Create(p VolumeCreateParams) (*Volume, error) {
	r.writes++
	return &Volume{NodeId: p.NodeId, Path: "/new"}, nil
}

type safetyMachines struct {
	provisioningVMs
	starts int
}

func (r *safetyMachines) Start(string, string) error { r.starts++; return nil }
func (r *safetyMachines) Get(id, node string) (*VirtualMachine, error) {
	return &VirtualMachine{Id: id, NodeId: node, Volumes: []*VirtualMachineAttachedVolume{{Managed: true, Path: "/base"}}}, nil
}

type safetyEvents struct{}

func (safetyEvents) Publish(Event) error { return nil }

func safetyFixture() *safetyVolumes {
	return &safetyVolumes{source: Volume{Format: VolumeFormatRaw, Identity: "source", Size: NewSize(1, SizeUnitG)}, state: StorageDependencyState{Dependencies: []VolumeDependency{{BasePath: "/base", DependentPath: "/foreign/private-copy"}}, WritableDisks: map[string][]string{"vm": {"/base"}}, CanonicalPaths: map[string]string{"/alias": "/base"}}}
}
func TestReferencedBaseMutationsAndIncompleteScan(t *testing.T) {
	for _, name := range []string{"resize", "delete", "upload"} {
		for _, incomplete := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/incomplete=%v", name, incomplete), func(t *testing.T) {
				repo := safetyFixture()
				if incomplete {
					repo.state = StorageDependencyState{}
					repo.scanErr = fmt.Errorf("incomplete scan")
				}
				volumes := NewVolumeService(repo)
				var err error
				switch name {
				case "resize":
					err = volumes.Resize("/alias", "node", NewSize(2, SizeUnitG))
				case "delete":
					err = volumes.Delete("/alias", "node")
				case "upload":
					err = volumes.Upload("/alias", "node", strings.NewReader("x"), 1)
				}
				if err == nil || repo.writes != 0 || repo.scans != 1 {
					t.Fatalf("unsafe mutation: %v %+v", err, repo)
				}
				if strings.Contains(err.Error(), "private-copy") {
					t.Fatal("foreign dependency exposed", err)
				}
			})
		}
	}
	repo := safetyFixture()
	volumes := NewVolumeService(repo)
	if err := volumes.Delete("/overlay", "node"); err != nil || repo.writes != 1 {
		t.Fatal("overlay deletion rejected", err)
	}
	// A fresh service sees the backend state; no dependency registry is required.
	repo.state.Dependencies = nil
	if err := NewVolumeService(repo).Delete("/base", "node"); err != nil {
		t.Fatal(err)
	}
}
func TestEveryServiceStartPathChecksDependencies(t *testing.T) {
	for _, action := range []bool{false, true} {
		repo := safetyFixture()
		machines := &safetyMachines{}
		vms := NewVirtualMachineService(machines)
		vms.Dependencies = repo
		var err error
		if action {
			err = vms.Action("vm", "node", "start")
		} else {
			err = vms.Start("vm", "node")
		}
		if !errors.Is(err, ErrConflict) || machines.starts != 0 {
			t.Fatal("start bypassed dependency guard", err)
		}
		repo.state.WritableDisks["vm"] = nil
		if err := vms.Start("vm", "node"); err != nil || machines.starts != 1 {
			t.Fatal("read-only machine rejected", err)
		}
		repo.scanErr = errors.New("incomplete")
		if err := vms.Start("vm", "node"); err == nil || machines.starts != 1 {
			t.Fatal("incomplete scan permitted start", err)
		}
	}
}
func TestCreateAndStartPreservesCreatedResourcesOnDependencyConflict(t *testing.T) {
	repo := safetyFixture()
	machines := &safetyMachines{}
	manager := NewVirtualMachineManager(NewVirtualMachineService(machines), NewVolumeService(repo), safetyEvents{}, nil, nil)
	vm := &VirtualMachine{Id: "vm", Name: "vm", NodeId: "node", VCpus: 1, Memory: NewSize(1, SizeUnitG), Graphic: VirtualMachineGraphic{Type: GraphicTypeNone}, VideoModel: VideoModelNone, Volumes: []*VirtualMachineAttachedVolume{{Path: "/base"}}}
	err := manager.createPrepared(vm, nil, []VirtualMachineManagerCreatedVolumeParams{{Name: "new", Pool: "pool", Format: VolumeFormatRaw, Size: NewSize(1, SizeUnitG)}}, true, VirtualMachineManagerNodeSettings{}, nil)
	var partial *ProvisioningError
	if !errors.As(err, &partial) || !errors.Is(err, ErrConflict) || !partial.VMCreated || len(partial.Volumes) != 1 || machines.starts != 0 || machines.deleted {
		t.Fatalf("incorrect partial result: %+v %v", partial, err)
	}
}
func TestVMDeleteChecksAllDisksBeforeDeletingMachine(t *testing.T) {
	repo := safetyFixture()
	machines := &safetyMachines{}
	manager := NewVirtualMachineManager(NewVirtualMachineService(machines), NewVolumeService(repo), safetyEvents{}, nil, nil)
	if err := manager.Delete("vm", "node", true); !errors.Is(err, ErrConflict) || machines.deleted || repo.writes != 0 {
		t.Fatal("partially deleted conflicting VM", err)
	}
}
func TestCloneSourcePolicyAndIdentity(t *testing.T) {
	for _, name := range []string{"default-copy", "overlay", "wrong-format", "overlay-of-overlay", "copy-overlay", "identity-change", "shrink", "hidden", "attached"} {
		t.Run(name, func(t *testing.T) {
			repo := safetyFixture()
			p := VolumeCloneParams{NodeId: "node", OriginalPath: "/base", NewName: "copy", NewPool: "pool", NewSize: NewSize(2, SizeUnitG), Format: VolumeFormatQcow2, SourceIdentity: "source"}
			wantErr := false
			switch name {
			case "overlay":
				p.CloneMode = CloneModeOverlay
			case "wrong-format":
				p.CloneMode = CloneModeOverlay
				p.Format = VolumeFormatRaw
				wantErr = true
			case "overlay-of-overlay":
				p.CloneMode = CloneModeOverlay
				repo.source.BackingStore = &VolumeBackingStore{Path: "/parent", Format: VolumeFormatRaw}
				wantErr = true
			case "copy-overlay":
				repo.source.BackingStore = &VolumeBackingStore{Path: "/parent", Format: VolumeFormatRaw}
			case "identity-change":
				repo.source.Identity = "replacement"
				wantErr = true
			case "shrink":
				p.NewSize = NewSize(1, SizeUnitM)
				wantErr = true
			case "hidden":
				repo.source.Metadata.Hidden = true
				wantErr = true
			case "attached":
				repo.source.AttachedTo = "vm"
				wantErr = true
			}
			_, err := NewVolumeService(repo).Clone(p)
			if (err != nil) != wantErr || (repo.writes == 0) != wantErr {
				t.Fatalf("clone policy: writes=%d err=%v", repo.writes, err)
			}
		})
	}
}

func TestNamespaceGuardsUseHostDependenciesWithoutExposingForeignDisks(t *testing.T) {
	workload, rawVMs, rawVolumes, _ := scopedFixture(t)
	inspector := safetyFixture()
	inspector.state = StorageDependencyState{Dependencies: []VolumeDependency{{BasePath: "/pool/a", DependentPath: "/pool/b"}}, WritableDisks: map[string][]string{"a": {"/pool/a"}}}
	workload.Clouds.VMs.Dependencies = inspector
	workload.Clouds.Volumes.Dependencies = inspector
	_, vms, volumes, err := workload.Bind(WorkloadContext{Namespace: "a", Cloud: "cloud"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = volumes.Get("/pool/b", "node"); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign disk visible", err)
	}
	for _, err := range []error{vms.Action("a", "node", "start"), volumes.Resize("/pool/a", "node", NewSize(2, SizeUnitG)), volumes.Delete("/pool/a", "node")} {
		if !errors.Is(err, ErrConflict) || strings.Contains(err.Error(), "/pool/b") {
			t.Fatal("unsafe or leaking namespace guard", err)
		}
	}
	if inspector.scans != 3 || rawVMs.mutations != 0 || rawVolumes.mutations != 0 {
		t.Fatal("namespace wrapper bypassed host guard")
	}
}
