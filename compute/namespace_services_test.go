package compute

import (
	"errors"
	"fmt"
	"slices"
	"subuk/vmango/tasks"
	"sync"
	"testing"
)

type scopedTestVMs struct {
	mu sync.Mutex
	VirtualMachineRepository
	machines  map[string]*VirtualMachine
	mutations int
}

func (r *scopedTestVMs) Get(id, node string) (*VirtualMachine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	vm := r.machines[id]
	if vm == nil || vm.NodeId != node {
		return nil, ErrNotFound
	}
	copy := *vm
	return &copy, nil
}
func (r *scopedTestVMs) List(o VirtualMachineListOptions) ([]*VirtualMachine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := []*VirtualMachine{}
	for _, vm := range r.machines {
		if len(o.NodeIds) == 0 || slices.Contains(o.NodeIds, vm.NodeId) {
			copy := *vm
			result = append(result, &copy)
		}
	}
	return result, nil
}
func (r *scopedTestVMs) ValidateNew(vm *VirtualMachine) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.machines[vm.Id] != nil {
		return ErrConflict
	}
	return nil
}
func (r *scopedTestVMs) Save(vm *VirtualMachine) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	copy := *vm
	r.machines[vm.Id] = &copy
	r.mutations++
	return nil
}
func (r *scopedTestVMs) Delete(id, node string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.machines, id)
	r.mutations++
	return nil
}
func (r *scopedTestVMs) Start(id, node string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mutations++
	return nil
}
func (r *scopedTestVMs) AttachVolume(id, node string, d *VirtualMachineAttachedVolume) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.mutations++
	return nil
}

type scopedTestVolumes struct {
	mu sync.Mutex
	VolumeRepository
	volumes   map[string]*Volume
	mutations int
}

func (r *scopedTestVolumes) ResolvePath(node, pool, name string) (string, error) {
	return "/" + pool + "/" + name, nil
}

func (r *scopedTestVolumes) Get(path, node string) (*Volume, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := r.volumes[path]
	if v == nil || v.NodeId != node {
		return nil, ErrVolumeNotFound
	}
	copy := *v
	return &copy, nil
}
func (r *scopedTestVolumes) List(o VolumeListOptions) ([]*Volume, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := []*Volume{}
	for _, v := range r.volumes {
		if (len(o.NodeIds) == 0 || slices.Contains(o.NodeIds, v.NodeId)) && (len(o.PoolNames) == 0 || slices.Contains(o.PoolNames, v.Pool)) {
			copy := *v
			result = append(result, &copy)
		}
	}
	return result, nil
}
func (r *scopedTestVolumes) ValidateCreate(p VolumeCreateParams) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.volumes["/pool/"+p.Name] != nil {
		return ErrConflict
	}
	return nil
}
func (r *scopedTestVolumes) Create(p VolumeCreateParams) (*Volume, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	v := &Volume{NodeId: p.NodeId, Name: p.Name, Path: "/pool/" + p.Name, Pool: p.Pool, Identity: p.Name, Size: p.Size, Format: p.Format}
	r.volumes[v.Path] = v
	r.mutations++
	copy := *v
	return &copy, nil
}
func (r *scopedTestVolumes) Clone(p VolumeCloneParams) (*Volume, error) {
	return r.Create(VolumeCreateParams{NodeId: p.NodeId, Name: p.NewName, Pool: p.NewPool, Size: p.NewSize, Format: p.Format})
}
func (r *scopedTestVolumes) Delete(path, node string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.volumes, path)
	r.mutations++
	return nil
}
func (r *scopedTestVolumes) Resize(path, node string, size Size) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.volumes[path].Size = size
	r.mutations++
	return nil
}

type scopedTestAssignments struct {
	mu      sync.Mutex
	records map[string]VolumeAssignment
	fail    bool
}

func (r *scopedTestAssignments) CheckCreate(node, path, scope string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return ErrUnavailable
	}
	if _, ok := r.records[path]; ok {
		return ErrConflict
	}
	return nil
}

func (r *scopedTestAssignments) Get(v *Volume, scope string) (*VolumeAssignment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.fail {
		return nil, ErrUnavailable
	}
	a, ok := r.records[v.Path]
	if !ok {
		return nil, nil
	}
	return &a, nil
}
func (r *scopedTestAssignments) Put(v *Volume, scope string, a VolumeAssignment) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.fail {
		return ErrUnavailable
	}
	r.records[v.Path] = a
	return nil
}
func (r *scopedTestAssignments) Delete(v *Volume, scope string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.fail {
		return ErrUnavailable
	}
	delete(r.records, v.Path)
	return nil
}

func scopedFixture(t *testing.T) (*WorkloadService, *scopedTestVMs, *scopedTestVolumes, *scopedTestAssignments) {
	t.Helper()
	namespaces, err := NewNamespaceService([]Namespace{{ID: "a", Clouds: []string{"cloud"}}, {ID: "b", Clouds: []string{"cloud"}}})
	if err != nil {
		t.Fatal(err)
	}
	vms := &scopedTestVMs{machines: map[string]*VirtualMachine{
		"a": {Id: "a", Name: "backend-a", Label: "same", NamespaceID: "a", NodeId: "node", Volumes: []*VirtualMachineAttachedVolume{{Path: "/pool/a"}}},
		"b": {Id: "b", Name: "backend-b", Label: "same", NamespaceID: "b", NodeId: "node", Volumes: []*VirtualMachineAttachedVolume{{Path: "/pool/b"}}},
	}}
	vols := &scopedTestVolumes{volumes: map[string]*Volume{}}
	a := &scopedTestAssignments{records: map[string]VolumeAssignment{}}
	for _, id := range []string{"a", "b"} {
		path := "/pool/" + id
		vols.volumes[path] = &Volume{NodeId: "node", Path: path, Pool: "pool", Name: id, Identity: id, Format: VolumeFormatRaw, Size: NewSize(1, SizeUnitG)}
		a.records[path] = VolumeAssignment{NamespaceID: NamespaceID(id), Label: "same"}
	}
	clouds := &CloudService{Definitions: []Cloud{{ID: "cloud", Hosts: []CloudHost{{Node: "node"}}}}, VMs: NewVirtualMachineService(vms), Volumes: NewVolumeService(vols)}
	return &WorkloadService{Namespaces: namespaces, Clouds: clouds, Assignments: a, Scopes: map[string]string{"node": "scope"}}, vms, vols, a
}

func TestNamespaceServicesRejectCrossNamespaceGraphBeforeEffects(t *testing.T) {
	w, rawVMs, rawVolumes, _ := scopedFixture(t)
	_, vms, vols, err := w.Bind(WorkloadContext{Namespace: "a", Cloud: "cloud"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := vms.Get("b", "node"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross namespace VM read", err)
	}
	if _, err := vols.Get("/pool/b", "node"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross namespace volume read", err)
	}
	if err := vms.AttachVolume("a", "node", &VirtualMachineAttachedVolume{Path: "/pool/b", DeviceType: DeviceTypeDisk, DeviceBus: DeviceBusVirtio}); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross namespace attach", err)
	}
	if err := vols.Delete("/pool/b", "node"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross namespace delete", err)
	}
	if _, err := vols.Clone(VolumeCloneParams{NodeId: "node", OriginalPath: "/pool/b", NewName: "clone", NewPool: "pool"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross namespace clone", err)
	}
	// An externally inconsistent graph must stop even a mutation of its own VM.
	rawVMs.machines["a"].Volumes = append(rawVMs.machines["a"].Volumes, &VirtualMachineAttachedVolume{Path: "/pool/b"})
	if err := vms.Start("a", "node"); !errors.Is(err, ErrNotFound) {
		t.Fatal("inconsistent graph power action", err)
	}
	if rawVMs.mutations != 0 || rawVolumes.mutations != 0 {
		t.Fatal("rejected operation changed backend")
	}
	listed, err := vms.List(VirtualMachineListOptions{})
	if err != nil || len(listed) != 1 || listed[0].Id != "a" {
		t.Fatalf("scoped inventory: %+v %v", listed, err)
	}
	raw, err := w.Clouds.VMs.List(VirtualMachineListOptions{})
	if err != nil || len(raw) != 2 {
		t.Fatal("shared capacity inventory was scoped")
	}
	w.Namespaces, _ = NewNamespaceService(nil)
	if _, err := vms.Get("a", "node"); !errors.Is(err, ErrNotFound) {
		t.Fatal("removed namespace remained admitted")
	}
}

func TestNamespaceServicesCreationCloneDeletionRecords(t *testing.T) {
	w, _, raw, assignments := scopedFixture(t)
	_, _, vols, err := w.Bind(WorkloadContext{Namespace: "a", Cloud: "cloud"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	create := VolumeCreateParams{NodeId: "node", Name: "backend-unique", Label: "unique label", Pool: "pool", Size: NewSize(1, SizeUnitG), Format: VolumeFormatRaw}
	v, err := vols.Create(create)
	if err != nil || v.NamespaceID != "a" || v.Label != create.Label {
		t.Fatalf("create assignment: %+v %v", v, err)
	}
	if err := vols.Delete(v.Path, v.NodeId); err != nil {
		t.Fatal(err)
	}
	if _, ok := assignments.records[v.Path]; ok {
		t.Fatal("confirmed deletion retained assignment")
	}
	if _, err := vols.Create(VolumeCreateParams{NodeId: "node", Name: "another", Label: "same", Pool: "pool", Size: NewSize(1, SizeUnitG), Format: VolumeFormatRaw}); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate namespace label accepted", err)
	}
	delete(assignments.records, "/pool/a")
	if _, err := vols.Create(VolumeCreateParams{NodeId: "node", Name: "another", Label: "same", Pool: "pool", Size: NewSize(1, SizeUnitG), Format: VolumeFormatRaw}); err != nil {
		t.Fatal("other namespace label blocked creation", err)
	}
	// Only the accepted image path may cross assignment boundaries for cloning.
	raw.volumes["/pool/image"] = &Volume{NodeId: "node", Name: "image", Path: "/pool/image", Pool: "pool", Identity: "image", Metadata: VolumeMetadata{Hidden: true, Protected: true}, Format: VolumeFormatRaw, Size: NewSize(1, SizeUnitG)}
	_, _, provisioning, err := w.bindWithSources(WorkloadContext{Namespace: "a", Cloud: "cloud"}, nil, map[string]bool{VolumePolicyKey("node", "/pool/image"): true})
	if err != nil {
		t.Fatal(err)
	}
	clone := VolumeCloneParams{NodeId: "node", OriginalPath: "/pool/image", NewName: "image-clone", Label: "root", NewPool: "pool"}
	if _, err := vols.Clone(clone); !errors.Is(err, ErrNotFound) {
		t.Fatal("ordinary image clone bypass", err)
	}
	if _, err := provisioning.Clone(clone); !errors.Is(err, ErrVolumeNotFound) {
		t.Fatal("hidden image clone without verification", err)
	}
	cloned, err := provisioning.clone(clone, true)
	if err != nil || cloned.NamespaceID != "a" || cloned.Label != "root" {
		t.Fatalf("verified provisioning clone: %+v %v", cloned, err)
	}
	if err := provisioning.Resize("/pool/image", "node", NewSize(2, SizeUnitG)); err == nil {
		t.Fatal("image source exception allowed mutation")
	}
	if _, ok := assignments.records["/pool/image"]; ok {
		t.Fatal("read/clone assigned shared image")
	}
}

func TestNamespaceCreationPersistenceFailureRetainsReference(t *testing.T) {
	w, _, raw, assignments := scopedFixture(t)
	_, _, vols, err := w.Bind(WorkloadContext{Namespace: "a", Cloud: "cloud"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Fail assignment writes after creation; reads remain available for validation.
	w.Assignments = &failingScopedAssignments{scopedTestAssignments: assignments}
	v, err := vols.Create(VolumeCreateParams{NodeId: "node", Name: "partial", Label: "partial", Pool: "pool", Size: NewSize(1, SizeUnitG), Format: VolumeFormatRaw})
	var diagnostic *tasks.DiagnosticError
	if v == nil || !errors.As(err, &diagnostic) || len(diagnostic.Effects) != 1 || diagnostic.Effects[0].Resource.ID != v.Path || raw.volumes[v.Path] == nil {
		t.Fatalf("lost partial creation: %+v %v", v, err)
	}
}

type failingScopedAssignments struct{ *scopedTestAssignments }

func (r *failingScopedAssignments) Put(*Volume, string, VolumeAssignment) error {
	return fmt.Errorf("injected persistence failure")
}

func TestNamespaceArtifactsCannotBecomeSharedVolumes(t *testing.T) {
	w, _, _, _ := scopedFixture(t)
	c := WorkloadContext{Namespace: "a", Cloud: "cloud"}
	_, _, internal, err := w.bindWithSources(c, nil, nil, "a")
	if err != nil {
		t.Fatal(err)
	}
	_, _, ordinary, _ := w.Bind(c, nil)
	p := VolumeCreateParams{OwnerVM: "a", NodeId: "node", Name: "seed-unique", Label: "seed", Pool: "pool", Size: NewSize(1, SizeUnitM), Format: VolumeFormatIso}
	if _, err := ordinary.Create(p); !errors.Is(err, ErrNotFound) {
		t.Fatal("ordinary caller could create a VM-owned artifact", err)
	}
	v, err := internal.Create(p)
	if err != nil || v.OwnerVM != "a" {
		t.Fatalf("artifact creation: %+v %v", v, err)
	}
	if _, err := ordinary.Get(v.Path, v.NodeId); !errors.Is(err, ErrNotFound) {
		t.Fatal("ordinary detail exposed artifact", err)
	}
	listed, err := internal.List(VolumeListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range listed {
		if item.Path == v.Path {
			t.Fatal("artifact selectable as shared volume")
		}
	}
	if _, err := internal.Clone(VolumeCloneParams{NodeId: "node", OriginalPath: v.Path, NewName: "seed-copy", NewPool: "pool"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("artifact clone allowed", err)
	}
	if err := internal.Delete(v.Path, v.NodeId); err != nil {
		t.Fatal("own artifact cleanup failed", err)
	}
}

func TestNamespaceConcurrentVolumeLabelsAreReservedAcrossBoundServices(t *testing.T) {
	w, _, raw, _ := scopedFixture(t)
	c := WorkloadContext{Namespace: "a", Cloud: "cloud"}
	_, _, first, _ := w.Bind(c, nil)
	_, _, second, _ := w.Bind(c, nil)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i, service := range []*VolumeService{first, second} {
		go func(i int, service *VolumeService) {
			<-start
			_, err := service.Create(VolumeCreateParams{NodeId: "node", Name: fmt.Sprintf("backend-%d", i), Label: "contended label", Pool: "pool", Size: NewSize(1, SizeUnitG), Format: VolumeFormatRaw})
			results <- err
		}(i, service)
	}
	close(start)
	success, conflict := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 || raw.mutations != 1 {
		t.Fatalf("duplicate label race: success=%d conflicts=%d mutations=%d", success, conflict, raw.mutations)
	}
}

func TestNamespaceCreationRejectsStaleRecordBeforeBackendEffects(t *testing.T) {
	w, _, raw, assignments := scopedFixture(t)
	delete(raw.volumes, "/pool/b")
	_, _, volumes, err := w.Bind(WorkloadContext{Namespace: "a", Cloud: "cloud"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = volumes.Create(VolumeCreateParams{NodeId: "node", Name: "b", Label: "new label", Pool: "pool", Size: NewSize(1, SizeUnitG), Format: VolumeFormatRaw})
	if !errors.Is(err, ErrConflict) || raw.mutations != 0 || raw.volumes["/pool/b"] != nil {
		t.Fatalf("new volume inherited stale assignment: %v mutations=%d", err, raw.mutations)
	}
	_, err = volumes.Clone(VolumeCloneParams{NodeId: "node", OriginalPath: "/pool/a", NewName: "b", Label: "clone label", NewPool: "pool"})
	if !errors.Is(err, ErrConflict) || raw.mutations != 0 || raw.volumes["/pool/b"] != nil {
		t.Fatalf("clone inherited stale assignment: %v mutations=%d", err, raw.mutations)
	}
	if assignments.records["/pool/b"].NamespaceID != "b" {
		t.Fatal("failed create silently erased record")
	}
}

type unavailableNamespaceInventory struct {
	*scopedTestVolumes
	reads []VolumeListOptions
}

func (r *unavailableNamespaceInventory) ListInventory(options VolumeListOptions) (VolumeInventory, error) {
	r.reads = append(r.reads, options)
	return VolumeInventory{Volumes: []*Volume{}, UnavailableHosts: []string{"node"}}, nil
}

func TestNamespaceUnavailableVolumeInventoryPreventsCreation(t *testing.T) {
	w, _, raw, assignments := scopedFixture(t)
	inventory := &unavailableNamespaceInventory{scopedTestVolumes: raw}
	w.Clouds.Volumes = NewVolumeService(inventory)
	_, _, volumes, err := w.Bind(WorkloadContext{Namespace: "a", Cloud: "cloud"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	created, err := volumes.Create(VolumeCreateParams{NodeId: "node", Name: "new-backend", Label: "new label", Pool: "pool", Size: NewSize(1, SizeUnitG), Format: VolumeFormatRaw})
	if !errors.Is(err, ErrUnavailable) || created != nil {
		t.Fatalf("unavailable inventory was treated as an unused label: %+v %v", created, err)
	}
	if len(inventory.reads) != 1 || !slices.Equal(inventory.reads[0].NodeIds, []string{"node"}) || !slices.Equal(inventory.reads[0].PoolNames, []string{"pool"}) {
		t.Fatalf("label validation did not inspect target placement: %+v", inventory.reads)
	}
	if raw.mutations != 0 || raw.volumes["/pool/new-backend"] != nil || len(assignments.records) != 2 {
		t.Fatal("failed inventory validation changed backend or assignments")
	}
}

type scopedAttachmentPlanner struct {
	*scopedTestVMs
	calls int
}

func (r *scopedAttachmentPlanner) ValidateDiskAttachments(_ *VirtualMachine, disks []*VirtualMachineAttachedVolume) error {
	r.calls++
	if len(disks) != 2 {
		return errors.New("incomplete attachment set")
	}
	return errors.New("test capacity exhausted")
}
func TestNamespaceForwardsCompleteAttachmentPreflight(t *testing.T) {
	w, raw, _, _ := scopedFixture(t)
	planner := &scopedAttachmentPlanner{scopedTestVMs: raw}
	w.Clouds.VMs.VirtualMachineRepository = planner
	_, scoped, _, err := w.Bind(WorkloadContext{Namespace: "a", Cloud: "cloud"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	bound := scoped.VirtualMachineRepository.(MachineAttachmentPlanner)
	disks := []*VirtualMachineAttachedVolume{{Alias: "root"}, {Alias: "configdrive"}}
	if err = bound.ValidateDiskAttachments(&VirtualMachine{NodeId: "node"}, disks); err == nil || err.Error() != "test capacity exhausted" || planner.calls != 1 {
		t.Fatalf("preflight bypassed: calls=%d err=%v", planner.calls, err)
	}
	if err = bound.ValidateDiskAttachments(&VirtualMachine{NodeId: "outside"}, disks); err == nil || planner.calls != 1 {
		t.Fatal("preflight escaped namespace host boundary", err)
	}
}
