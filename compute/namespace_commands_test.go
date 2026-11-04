package compute_test

import (
	"errors"
	"path/filepath"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

// Unlike the older scheduling fixture, this backend persists observed disk
// identities so namespace verification exercises the real command path.
type namespaceCommandVolumes struct {
	*cloudVolumeBackend
	mu      sync.Mutex
	volumes map[string]compute.Volume
}

func (r *namespaceCommandVolumes) Get(path, node string) (*compute.Volume, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.volumes[node+"/"+path]
	if !ok {
		return nil, compute.ErrVolumeNotFound
	}
	return &v, nil
}
func (r *namespaceCommandVolumes) List(options compute.VolumeListOptions) ([]*compute.Volume, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*compute.Volume{}
	for _, v := range r.volumes {
		include := len(options.NodeIds) == 0
		for _, node := range options.NodeIds {
			if v.NodeId == node {
				include = true
			}
		}
		if include {
			copy := v
			out = append(out, &copy)
		}
	}
	return out, nil
}
func (r *namespaceCommandVolumes) save(v *compute.Volume) *compute.Volume {
	v.PoolIdentity = v.NodeId + "-pool-uuid"
	v.BackendKey = v.Path
	r.mu.Lock()
	r.volumes[v.NodeId+"/"+v.Path] = *v
	r.mu.Unlock()
	return v
}
func (r *namespaceCommandVolumes) Create(p compute.VolumeCreateParams) (*compute.Volume, error) {
	v, err := r.cloudVolumeBackend.Create(p)
	if err != nil {
		return v, err
	}
	return r.save(v), nil
}
func (r *namespaceCommandVolumes) Clone(p compute.VolumeCloneParams) (*compute.Volume, error) {
	v, err := r.cloudVolumeBackend.Clone(p)
	if err != nil {
		return v, err
	}
	return r.save(v), nil
}
func (r *namespaceCommandVolumes) Delete(path, node string) error {
	r.mu.Lock()
	delete(r.volumes, node+"/"+path)
	r.mu.Unlock()
	return nil
}

func namespaceCommandFixture(t *testing.T) (*tasks.Service, *compute.CommandHandler, *cloudVMBackend, *namespaceCommandVolumes) {
	t.Helper()
	h, vms, oldVolumes, _ := cloudFixtureHandler(t, 8, nil)
	volumes := &namespaceCommandVolumes{cloudVolumeBackend: oldVolumes, volumes: map[string]compute.Volume{}}
	for _, node := range []string{"a", "b"} {
		source, _ := oldVolumes.Get("/source", node)
		volumes.save(source)
	}
	h.Volumes.VolumeRepository = volumes
	registry, err := compute.NewNamespaceService([]compute.Namespace{{ID: "alpha", Clouds: []string{"c"}}, {ID: "beta", Clouds: []string{"c"}}})
	if err != nil {
		t.Fatal(err)
	}
	assignments, err := filesystem.NewVolumeAssignmentRepository(filepath.Join(t.TempDir(), "assignments"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { assignments.Close() })
	h.Scopes = map[string]string{"a": "scope-a", "b": "scope-b"}
	h.Workloads = &compute.WorkloadService{Namespaces: registry, Clouds: h.Clouds, Assignments: assignments, Scopes: h.Scopes}
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	return tasks.New(repo, h, zerolog.Nop()), h, vms, volumes
}
func TestNamespaceCommandsCreateEqualLabelsAndPersistDisks(t *testing.T) {
	s, h, vms, _ := namespaceCommandFixture(t)
	a := submitCloud(t, s, "alpha-create-equal-label", "machine.create", compute.InfrastructureCommand{NamespaceID: "alpha", CloudID: "c", CreateSpec: createSpec("web")})
	b := submitCloud(t, s, "beta-create-equal-label", "machine.create", compute.InfrastructureCommand{NamespaceID: "beta", CloudID: "c", CreateSpec: createSpec("web")})
	runCloudService(t, s)
	for _, accepted := range []tasks.Task{a, b} {
		result := awaitCloudTask(t, s, accepted.ID)
		if result.State != tasks.Succeeded {
			t.Fatalf("namespace create failed: %+v", result)
		}
	}
	vms.mu.Lock()
	machines := []compute.VirtualMachine{}
	for _, vm := range vms.machines {
		machines = append(machines, vm)
	}
	vms.mu.Unlock()
	if len(machines) != 2 || machines[0].Name == machines[1].Name {
		t.Fatal("backend names not independent", machines)
	}
	for _, vm := range machines {
		if vm.Label != "web" || vm.NamespaceID == "" || len(vm.Volumes) == 0 {
			t.Fatal("membership or label missing", vm)
		}
		for _, disk := range vm.Volumes {
			volume, err := h.Volumes.Get(disk.Path, vm.NodeId)
			if err != nil {
				t.Fatal(err)
			}
			assignment, err := h.Workloads.Assignments.Get(volume, h.Scopes[vm.NodeId])
			if err != nil || assignment == nil || assignment.NamespaceID != vm.NamespaceID {
				t.Fatal("disk assignment missing", assignment, err)
			}
		}
	}
	_, err := s.Submit(tasks.Identity{ID: "admin"}, "duplicate-in-alpha", compute.NewCommand("machine.create", compute.InfrastructureCommand{NamespaceID: "alpha", CloudID: "c", CreateSpec: createSpec("web")}))
	if !errors.Is(err, compute.ErrConflict) {
		t.Fatal("duplicate label accepted in same namespace", err)
	}
}
func TestNamespaceQueuedContextRemovalFailsBeforeEffects(t *testing.T) {
	s, h, vms, volumes := namespaceCommandFixture(t)
	accepted := submitCloud(t, s, "removed-before-execution", "machine.create", compute.InfrastructureCommand{NamespaceID: "alpha", CloudID: "c", CreateSpec: createSpec("web")})
	h.Workloads.Namespaces, _ = compute.NewNamespaceService(nil)
	runCloudService(t, s)
	result := awaitCloudTask(t, s, accepted.ID)
	if result.State != tasks.Failed || result.OutcomeUncertain || len(result.Effects) != 0 {
		t.Fatal("removed context mutated backend", result)
	}
	vms.mu.Lock()
	defer vms.mu.Unlock()
	volumes.cloudVolumeBackend.mu.Lock()
	defer volumes.cloudVolumeBackend.mu.Unlock()
	if len(vms.machines) != 0 || volumes.created != 0 {
		t.Fatal("removed context caused creation")
	}
}

type namespaceCommandVMs struct{ *cloudVMBackend }

func (r *namespaceCommandVMs) GetNamespace(id, node string) (compute.NamespaceID, string, error) {
	vm, err := r.Get(id, node)
	if err != nil {
		return "", "", err
	}
	return vm.NamespaceID, vm.Label, nil
}
func (r *namespaceCommandVMs) AssignNamespace(id, node string, namespace compute.NamespaceID, label string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	vm, ok := r.machines[node+"/"+id]
	if !ok {
		return compute.ErrNotFound
	}
	if vm.NamespaceID != "" && vm.NamespaceID != namespace {
		return compute.ErrConflict
	}
	vm.NamespaceID = namespace
	vm.Label = label
	r.machines[node+"/"+id] = vm
	r.mutations++
	return nil
}
func seedNamespaceGraph(t *testing.T, h *compute.CommandHandler, vms *cloudVMBackend, volumes *namespaceCommandVolumes, namespace compute.NamespaceID) (compute.VirtualMachine, *compute.Volume) {
	t.Helper()
	h.VMs.VirtualMachineRepository = &namespaceCommandVMs{vms}
	vm := compute.VirtualMachine{Id: "existing-vm", Name: "existing", Label: "existing", NodeId: "a", NamespaceID: namespace, VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG), State: compute.StateStopped, Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z", CPURequest: 1, MemoryRequestBytes: 1 << 30}, Volumes: []*compute.VirtualMachineAttachedVolume{{Path: "/pool/existing-disk", Alias: "root", DeviceType: compute.DeviceTypeDisk, DeviceBus: compute.DeviceBusVirtio}}}
	vms.mu.Lock()
	vms.machines["a/"+vm.Id] = vm
	vms.mu.Unlock()
	v := volumes.save(&compute.Volume{NodeId: "a", Path: "/pool/existing-disk", Name: "existing-disk", Pool: "pool", Identity: "existing-disk-identity", Format: compute.VolumeFormatRaw, Size: compute.NewSize(1, compute.SizeUnitG), AttachedTo: vm.Id})
	if namespace != "" {
		if err := h.Workloads.Assignments.Put(v, h.Scopes["a"], compute.VolumeAssignment{NamespaceID: namespace, Label: "existing-disk"}); err != nil {
			t.Fatal(err)
		}
	}
	return vm, v
}
func TestNamespaceAssignmentCapturedBackendScopeRejectsRestartChange(t *testing.T) {
	s, h, vms, volumes := namespaceCommandFixture(t)
	vm, volume := seedNamespaceGraph(t, h, vms, volumes, "")
	accepted := submitCloud(t, s, "assignment-before-restart", "machine.assign", compute.InfrastructureCommand{NamespaceID: "alpha", CloudID: "c", VM: vm.Id})
	// The same configured host ID now points to another backend on restart.
	h.Scopes["a"] = "replacement-backend-scope"
	runCloudService(t, s)
	result := awaitCloudTask(t, s, accepted.ID)
	if result.State != tasks.Failed || len(result.Effects) != 0 || result.OutcomeUncertain || result.BlocksResources {
		t.Fatal("changed backend assignment accepted or retained unnecessary locks", result)
	}
	vms.mu.Lock()
	mutations := vms.mutations
	persisted := vms.machines["a/"+vm.Id]
	vms.mu.Unlock()
	if mutations != 0 || persisted.NamespaceID != "" {
		t.Fatal("changed backend mutated VM", persisted, mutations)
	}
	if record, err := h.Workloads.Assignments.Get(volume, h.Scopes["a"]); err != nil || record != nil {
		t.Fatal("changed backend got assignment", record, err)
	}
}
func TestNamespaceAssignmentRejectsForeignDiskBeforeAnyWrite(t *testing.T) {
	s, h, vms, volumes := namespaceCommandFixture(t)
	vm, volume := seedNamespaceGraph(t, h, vms, volumes, "")
	if err := h.Workloads.Assignments.Put(volume, h.Scopes["a"], compute.VolumeAssignment{NamespaceID: "beta", Label: "foreign-disk"}); err != nil {
		t.Fatal(err)
	}
	_, err := s.Submit(tasks.Identity{ID: "admin"}, "assignment-with-foreign-disk", compute.NewCommand("machine.assign", compute.InfrastructureCommand{NamespaceID: "alpha", CloudID: "c", VM: vm.Id}))
	if err == nil {
		t.Fatal("mixed-namespace graph accepted")
	}
	vms.mu.Lock()
	mutations := vms.mutations
	persisted := vms.machines["a/"+vm.Id]
	vms.mu.Unlock()
	if mutations != 0 || persisted.NamespaceID != "" {
		t.Fatal("foreign graph assigned VM", persisted, mutations)
	}
	assignment, err := h.Workloads.Assignments.Get(volume, h.Scopes["a"])
	if err != nil || assignment.NamespaceID != "beta" {
		t.Fatal("foreign assignment changed", assignment, err)
	}
	page, err := s.List(tasks.Filter{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("rejected assignment persisted task", page, err)
	}
}
func TestNamespaceCommandDetectsReplacedDiskBeforeExecution(t *testing.T) {
	s, h, vms, volumes := namespaceCommandFixture(t)
	vm, volume := seedNamespaceGraph(t, h, vms, volumes, "alpha")
	accepted := submitCloud(t, s, "delete-with-snapshotted-disk", "machine.delete", compute.InfrastructureCommand{NamespaceID: "alpha", CloudID: "c", VM: vm.Id})
	volume.Identity = "replacement-disk-identity"
	volume.BackendKey = "replacement-key"
	volumes.save(volume)
	runCloudService(t, s)
	result := awaitCloudTask(t, s, accepted.ID)
	if result.State != tasks.Failed || result.OutcomeUncertain || len(result.Effects) != 0 {
		t.Fatal("replacement was not rejected before effects", result)
	}
	vms.mu.Lock()
	defer vms.mu.Unlock()
	if len(vms.machines) != 1 || vms.mutations != 0 {
		t.Fatal("VM mutated despite replaced disk")
	}
}
func TestNamespaceRemovalKeepsAssignmentsAndTaskHistory(t *testing.T) {
	s, h, vms, _ := namespaceCommandFixture(t)
	accepted := submitCloud(t, s, "create-before-namespace-removal", "machine.create", compute.InfrastructureCommand{NamespaceID: "alpha", CloudID: "c", CreateSpec: createSpec("web")})
	runCloudService(t, s)
	result := awaitCloudTask(t, s, accepted.ID)
	if result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	vms.mu.Lock()
	var vm compute.VirtualMachine
	for _, created := range vms.machines {
		vm = created
	}
	vms.mu.Unlock()
	registry := h.Workloads.Namespaces
	h.Workloads.Namespaces, _ = compute.NewNamespaceService(nil)
	if _, err := h.Workloads.Machine(compute.WorkloadContext{Namespace: "alpha", Cloud: "c"}, vm.Id); err == nil {
		t.Fatal("removed namespace remained selectable")
	}
	stored, err := s.Get(accepted.ID)
	if err != nil || stored.NamespaceID != "alpha" || stored.CloudID != "c" || stored.State != tasks.Succeeded {
		t.Fatal("history attribution lost", stored, err)
	}
	for _, disk := range vm.Volumes {
		v, err := h.Volumes.Get(disk.Path, vm.NodeId)
		if err != nil {
			t.Fatal(err)
		}
		a, err := h.Workloads.Assignments.Get(v, h.Scopes[vm.NodeId])
		if err != nil || a == nil || a.NamespaceID != "alpha" {
			t.Fatal("removal erased assignment", a, err)
		}
	}
	h.Workloads.Namespaces = registry
	if restored, err := h.Workloads.Machine(compute.WorkloadContext{Namespace: "alpha", Cloud: "c"}, vm.Id); err != nil || restored.NamespaceID != "alpha" {
		t.Fatal("re-added namespace failed", restored, err)
	}
}
