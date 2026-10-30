package compute_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"path/filepath"
	"subuk/vmango/catalog"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"sync"
	"testing"
	"time"
)

type cloudVMBackend struct {
	compute.VirtualMachineRepository
	mu                 sync.Mutex
	machines           map[string]compute.VirtualMachine
	inventory          []string
	failHost           string
	mutations          int
	starts             int
	unsupportedDisplay bool
	listeners          map[string]string
}

func (r *cloudVMBackend) Get(id, node string) (*compute.VirtualMachine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.machines[node+"/"+id]
	if !ok {
		return nil, compute.ErrNotFound
	}
	return &v, nil
}
func (r *cloudVMBackend) List(o compute.VirtualMachineListOptions) ([]*compute.VirtualMachine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []*compute.VirtualMachine{}
	for _, node := range o.NodeIds {
		r.inventory = append(r.inventory, node)
		if node == r.failHost {
			return nil, fmt.Errorf("inventory unavailable")
		}
		for _, v := range r.machines {
			if v.NodeId == node {
				copy := v
				out = append(out, &copy)
			}
		}
	}
	return out, nil
}
func (r *cloudVMBackend) ValidateNew(v *compute.VirtualMachine) error {
	_, err := r.Get(v.Id, v.NodeId)
	if err == nil {
		return compute.ErrConflict
	}
	return nil
}
func (r *cloudVMBackend) Capabilities(node string, arch compute.Arch) (compute.VirtualMachineCapabilities, error) {
	if r.unsupportedDisplay {
		return compute.VirtualMachineCapabilities{Arch: arch, DiskBuses: []compute.DeviceBus{compute.DeviceBusVirtio}, NICModels: []string{"virtio"}, Graphics: []compute.GraphicType{compute.GraphicTypeNone}, VideoModels: []compute.VideoModel{compute.VideoModelNone}}, nil
	}
	return compute.VirtualMachineCapabilities{Arch: arch, MaxVCPUs: 16, Firmwares: []string{"bios"}, DiskBuses: []compute.DeviceBus{compute.DeviceBusVirtio}, NICModels: []string{"virtio"}, Graphics: []compute.GraphicType{compute.GraphicTypeVnc, compute.GraphicTypeSpice, compute.GraphicTypeNone}, VideoModels: []compute.VideoModel{compute.VideoModelNone, compute.VideoModelQxl}}, nil
}
func (r *cloudVMBackend) Save(v *compute.VirtualMachine) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *v
	r.machines[v.NodeId+"/"+v.Id] = copy
	r.mutations++
	return nil
}
func (r *cloudVMBackend) Adopt(id, node string, m *compute.MachineMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.machines[node+"/"+id]
	v.Metadata = m
	r.machines[node+"/"+id] = v
	r.mutations++
	return nil
}
func (r *cloudVMBackend) Delete(id, node string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.machines, node+"/"+id)
	r.mutations++
	return nil
}

type cloudNodeBackend struct {
	compute.NodeRepository
	cpus int
}

func (r cloudNodeBackend) Get(id string, _ compute.NodeGetOptions) (*compute.Node, error) {
	return &compute.Node{Id: id, CpuArch: compute.ArchAmd64, Cpus: make([]compute.NodeCpu, r.cpus), Numas: []compute.NodeNuma{{Memory: compute.NewSize(16, compute.SizeUnitG)}}}, nil
}

type cloudPoolBackend struct{ compute.VolumePoolRepository }

func (cloudPoolBackend) List(o compute.VolumePoolListOptions) ([]*compute.VolumePool, error) {
	return []*compute.VolumePool{{NodeId: o.NodeIds[0], Name: "pool", Free: compute.NewSize(100, compute.SizeUnitG)}}, nil
}

type cloudVolumeBackend struct {
	compute.VolumeRepository
	mu           sync.Mutex
	created      int
	deleted      int
	cloneStarted chan struct{}
	cloneRelease chan struct{}
	cloneError   error
}

func (r *cloudVolumeBackend) Get(path, node string) (*compute.Volume, error) {
	return &compute.Volume{NodeId: node, Path: path, Name: "source", Pool: "pool", Format: compute.VolumeFormatRaw, Size: compute.NewSize(1, compute.SizeUnitG), Identity: node + path}, nil
}
func (r *cloudVolumeBackend) ValidateCreate(compute.VolumeCreateParams) error { return nil }
func (r *cloudVolumeBackend) ResolvePath(node, pool, name string) (string, error) {
	return "/pool/" + name, nil
}
func (r *cloudVolumeBackend) Clone(p compute.VolumeCloneParams) (*compute.Volume, error) {
	if r.cloneStarted != nil {
		if p.NewName == "one_root" {
			close(r.cloneStarted)
			<-r.cloneRelease
			if r.cloneError != nil {
				return nil, r.cloneError
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.created++
	return &compute.Volume{NodeId: p.NodeId, Path: "/pool/" + p.NewName, Name: p.NewName, Pool: p.NewPool, Format: p.Format, Size: p.NewSize, Identity: p.NodeId + "/pool/" + p.NewName}, nil
}
func (r *cloudVolumeBackend) Delete(path, node string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted++
	return nil
}

type cloudEvents struct{}

func (cloudEvents) Publish(compute.Event) error { return nil }

func cloudFixture(t *testing.T, cpus int) (*tasks.Service, *cloudVMBackend, *cloudVolumeBackend, compute.Cloud) {
	t.Helper()
	off := false
	imageRepo, err := catalog.NewImageRepository([]compute.Image{{ID: "image", Name: "Image", OSName: "Linux", Arch: compute.ArchAmd64, SourcePath: "/source", Defaults: compute.ImageDefaults{Initialization: "none", Graphics: "spice", VideoModel: "qxl", GuestAgent: &off}}})
	if err != nil {
		t.Fatal(err)
	}
	images := compute.NewImageService(imageRepo)
	vmBackend := &cloudVMBackend{machines: map[string]compute.VirtualMachine{}, listeners: map[string]string{"a": "127.0.0.2", "b": "::1"}}
	volumeBackend := &cloudVolumeBackend{}
	vms := compute.NewVirtualMachineService(vmBackend)
	vms.CloudMode = true
	volumes := compute.NewVolumeService(volumeBackend)
	nodes := compute.NewNodeService(cloudNodeBackend{cpus: cpus})
	pools := compute.NewVolumePoolService(cloudPoolBackend{})
	manager := compute.NewVirtualMachineManager(vms, volumes, cloudEvents{}, nil, images)
	c := compute.Cloud{ID: "c", Name: "Cloud", Zones: []compute.CatalogEntry{{ID: "z", Name: "Zone"}}, InstanceTypes: []compute.InstanceType{{ID: "small", Name: "Small", Autostart: true, VCPUs: 2, CPURequest: 2, MemoryBytes: 2 << 30, MemoryRequestBytes: 2 << 30, BootDiskBytes: 2 << 30}}, Images: []string{"image"}, Networks: []compute.CatalogEntry{{ID: "lan", Name: "LAN"}}, StorageClasses: []compute.CatalogEntry{{ID: "fast", Name: "Fast"}}, Hosts: []compute.CloudHost{{Node: "a", Zone: "z", Labels: map[string]string{"rack": "one"}, Networks: map[string]compute.NetworkBinding{"lan": {Network: "net"}}, Storage: map[string]compute.StorageBinding{"fast": {Pool: "pool", Format: compute.VolumeFormatRaw}}}, {Node: "b", Zone: "z", Labels: map[string]string{"rack": "two"}, Networks: map[string]compute.NetworkBinding{"lan": {Network: "net"}}, Storage: map[string]compute.StorageBinding{"fast": {Pool: "pool", Format: compute.VolumeFormatRaw}}}}}
	clouds := &compute.CloudService{Definitions: []compute.Cloud{c}, Nodes: nodes, VMs: vms, Pools: pools, Volumes: volumes, Manager: manager}
	manager.Clouds = clouds
	handler := &compute.CommandHandler{Clouds: clouds, Manager: manager, VMs: vms, Volumes: volumes, Nodes: nodes}
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	service := tasks.New(repo, handler, zerolog.Nop())
	t.Cleanup(func() { repo.Close() })
	return service, vmBackend, volumeBackend, c
}
func awaitCloudTask(t *testing.T, s *tasks.Service, id string) tasks.Task {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		task, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.State.Terminal() {
			return task
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("task did not finish")
	return tasks.Task{}
}
func runCloudService(t *testing.T, s *tasks.Service) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.Start(ctx, 4); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); s.Wait() })
}
func submitCloud(t *testing.T, s *tasks.Service, key, kind string, c compute.InfrastructureCommand) tasks.Task {
	t.Helper()
	task, err := s.Submit(tasks.Identity{ID: "admin"}, key, compute.NewCommand(kind, c))
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func createSpec(name string) *compute.MachineCreateSpec {
	return &compute.MachineCreateSpec{Name: name, Image: "image", InstanceType: "small", Zone: "z", Network: "lan", StorageClass: "fast", Placement: compute.Placement{Required: map[string]string{"rack": "one"}}}
}
func TestConcurrentCloudAdmissionUsesFreshInventory(t *testing.T) {
	s, backend, volumes, _ := cloudFixture(t, 2)
	one := submitCloud(t, s, "first-create", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("one")})
	two := submitCloud(t, s, "second-create", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("two")})
	runCloudService(t, s)
	a := awaitCloudTask(t, s, one.ID)
	b := awaitCloudTask(t, s, two.ID)
	if a.State != tasks.Succeeded || b.State != tasks.Failed || b.OutcomeUncertain {
		t.Fatalf("incorrect admission: %+v %+v", a, b)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	volumes.mu.Lock()
	defer volumes.mu.Unlock()
	if len(backend.machines) != 1 || volumes.created != 1 || len(backend.inventory) != 4 {
		t.Fatalf("capacity used twice: machines=%d disks=%d inventory=%v", len(backend.machines), volumes.created, backend.inventory)
	}
}
func TestCloudInventoryFailureDoesNotBlockHealthyHost(t *testing.T) {
	s, backend, volumes, _ := cloudFixture(t, 4)
	backend.failHost = "b"
	task := submitCloud(t, s, "failed-inventory", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("one")})
	runCloudService(t, s)
	result := awaitCloudTask(t, s, task.ID)
	if result.State != tasks.Succeeded || result.OutcomeUncertain || backend.mutations != 1 || volumes.created != 1 {
		t.Fatalf("healthy host was not used: %+v", result)
	}
}
func TestTypeChangeStaysOnHostAndPreservesDisks(t *testing.T) {
	for _, sufficient := range []bool{true, false} {
		t.Run(fmt.Sprint(sufficient), func(t *testing.T) {
			s, backend, _, _ := cloudFixture(t, 2)
			backend.machines["a/v"] = compute.VirtualMachine{Id: "v", Name: "v", NodeId: "a", Arch: compute.ArchAmd64, VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG), State: compute.StateStopped, GuestAgent: true, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeVnc, Listen: "127.0.0.9"}, VideoModel: compute.VideoModelQxl, Volumes: []*compute.VirtualMachineAttachedVolume{{Path: "/existing"}}, Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z", CPURequest: 1, MemoryRequestBytes: 1 << 30}}
			if !sufficient {
				backend.machines["a/other"] = compute.VirtualMachine{Id: "other", Name: "other", NodeId: "a", VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG)}
			}
			task := submitCloud(t, s, "change-instance-type", "machine.change-type", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "v", InstanceType: "small"})
			runCloudService(t, s)
			result := awaitCloudTask(t, s, task.ID)
			v, _ := backend.Get("v", "a")
			if sufficient {
				if !v.Autostart || !v.GuestAgent || v.Graphic.Listen != "127.0.0.9" || v.Graphic.Type != compute.GraphicTypeVnc || v.VideoModel != compute.VideoModelQxl || v.State != compute.StateStopped || result.State != tasks.Succeeded || v.VCpus != 2 || v.Metadata.CPURequest != 2 || v.Volumes[0].Path != "/existing" {
					t.Fatalf("incorrect change: %+v %+v", result, v)
				}
			} else if result.State != tasks.Failed || backend.mutations != 0 || v.VCpus != 1 {
				t.Fatalf("rescheduled or changed despite no capacity: %+v", result)
			}
		})
	}
}
func TestAdoptionDoesNotClaimDisksOrChangeMachine(t *testing.T) {
	s, backend, volumes, _ := cloudFixture(t, 4)
	backend.machines["a/external"] = compute.VirtualMachine{Id: "external", Name: "external", NodeId: "a", VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG), State: compute.StateRunning, Autostart: true, GuestAgent: true, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeVnc, Listen: "::1"}, VideoModel: compute.VideoModelCirrus, Volumes: []*compute.VirtualMachineAttachedVolume{{Path: "/external-disk"}}}
	if _, err := s.Submit(tasks.Identity{ID: "admin"}, "delete-before-adoption", compute.NewCommand("machine.delete", compute.InfrastructureCommand{Node: "a", VM: "external"})); err == nil {
		t.Fatal("unmanaged deletion accepted")
	}
	task := submitCloud(t, s, "explicit-adoption", "machine.adopt", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "external"})
	runCloudService(t, s)
	if result := awaitCloudTask(t, s, task.ID); result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	vm, _ := backend.Get("external", "a")
	if !vm.Autostart || !vm.GuestAgent || vm.Graphic.Type != compute.GraphicTypeVnc || vm.Graphic.Listen != "::1" || vm.VideoModel != compute.VideoModelCirrus || vm.Metadata == nil || vm.VCpus != 1 || vm.State != compute.StateRunning {
		t.Fatal("adoption changed configuration or claimed disks")
	}
	deletion := submitCloud(t, s, "delete-after-adoption", "machine.delete", compute.InfrastructureCommand{Node: "a", VM: "external", DeleteVolumes: true})
	if result := awaitCloudTask(t, s, deletion.ID); result.State != tasks.Succeeded || volumes.deleted != 0 {
		t.Fatalf("unowned disk deleted: %+v count=%d", result, volumes.deleted)
	}
}

func TestLocalDiskConstraintsOverridePreferencesAndRejectConflicts(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			s, backend, volumes, _ := cloudFixture(t, 4)
			spec := createSpec("local")
			spec.ExistingDisks = []compute.LocalDiskRef{{Host: "a", Path: "/existing"}}
			spec.Placement = compute.Placement{Preferred: map[string]string{"rack": "two"}}
			if conflict {
				spec.Placement.Required = map[string]string{"rack": "two"}
			}
			task := submitCloud(t, s, "local-disk-placement", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: spec})
			runCloudService(t, s)
			result := awaitCloudTask(t, s, task.ID)
			if conflict {
				if result.State != tasks.Failed || volumes.created != 0 {
					t.Fatalf("conflict provisioned resources: %+v", result)
				}
			} else {
				vm, err := backend.Get(taskMachineID(t, result), "a")
				if result.State != tasks.Succeeded || err != nil || vm.Volumes[0].Path != "/existing" {
					t.Fatalf("locality ignored: %+v %v", result, err)
				}
			}
		})
	}
}

func TestTypeChangeDoesNotSilentlyRewriteExplicitPinning(t *testing.T) {
	s, backend, _, _ := cloudFixture(t, 8)
	backend.machines["a/pinned"] = compute.VirtualMachine{Id: "pinned", Name: "pinned", NodeId: "a", Arch: compute.ArchAmd64, VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG), State: compute.StateStopped, Cpupin: &compute.VirtualMachineCpuPin{Vcpus: map[uint][]uint{0: {0}}}, Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z", CPURequest: 1, MemoryRequestBytes: 1 << 30}}
	task := submitCloud(t, s, "change-pinned-machine", "machine.change-type", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "pinned", InstanceType: "small"})
	runCloudService(t, s)
	result := awaitCloudTask(t, s, task.ID)
	if result.State != tasks.Failed || result.OutcomeUncertain || backend.mutations != 0 {
		t.Fatalf("explicit CPU pinning was rewritten: %+v", result)
	}
}

func (r *cloudVMBackend) Poweroff(id, node string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.machines[node+"/"+id]
	v.State = compute.StateStopped
	r.machines[node+"/"+id] = v
	return nil
}

func TestCloudProvisioningDoesNotBlockIndependentPower(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(fmt.Sprint(uncertain), func(t *testing.T) {
			s, backend, volumes, _ := cloudFixture(t, 8)
			volumes.cloneStarted, volumes.cloneRelease = make(chan struct{}), make(chan struct{})
			if uncertain {
				volumes.cloneError = errors.New("clone response lost")
			}
			for _, id := range []string{"during", "after"} {
				backend.machines["b/"+id] = compute.VirtualMachine{Id: id, NodeId: "b", State: compute.StateRunning, Metadata: &compute.MachineMetadata{Cloud: "c"}, Volumes: []*compute.VirtualMachineAttachedVolume{{Path: "/pool/" + id}}}
			}
			create := submitCloud(t, s, "blocked-create", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("one")})
			follower := submitCloud(t, s, "queued-create", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("two")})
			runCloudService(t, s)
			var once sync.Once
			release := func() { once.Do(func() { close(volumes.cloneRelease) }) }
			// Release before the service cleanup waits for workers, even on failure.
			t.Cleanup(release)
			select {
			case <-volumes.cloneStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("provisioning did not reach clone")
			}
			power := submitCloud(t, s, "power-during-clone", "machine.poweroff", compute.InfrastructureCommand{CloudID: "c", Node: "b", VM: "during"})
			if got := awaitCloudTask(t, s, power.ID); got.State != tasks.Succeeded {
				t.Fatal(got)
			}
			if got, _ := s.Get(create.ID); got.State != tasks.Running {
				t.Fatal("provisioning was not held open", got)
			}
			if got, _ := s.Get(follower.ID); got.State != tasks.Queued {
				t.Fatal("competing admission ran during clone", got)
			}
			checkProtected := func() {
				current, _ := s.Get(create.ID)
				id := taskMachineID(t, current)
				for _, command := range []tasks.Command{
					compute.NewCommand("machine.start", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: id}),
					compute.NewCommand("volume.resize", compute.InfrastructureCommand{CloudID: "c", Node: "a", Path: "/pool/one_root", Size: compute.NewSize(3, compute.SizeUnitG)}),
				} {
					if _, err := s.Submit(tasks.Identity{ID: "admin"}, "protected-resource", command); !isTaskConflict(err) {
						t.Fatalf("affected resource not protected: %v", err)
					}
				}
			}
			checkProtected()
			release()
			got := awaitCloudTask(t, s, create.ID)
			if uncertain {
				if got.State != tasks.Failed || !got.OutcomeUncertain || !got.BlocksResources {
					t.Fatal(got)
				}
				checkProtected()
				if _, err := s.Submit(tasks.Identity{ID: "admin"}, "uncertain-admission", compute.NewCommand("machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("three")})); !isTaskConflict(err) {
					t.Fatalf("uncertain capacity was not fenced: %v", err)
				}
				power = submitCloud(t, s, "power-after-uncertain", "machine.poweroff", compute.InfrastructureCommand{CloudID: "c", Node: "b", VM: "after"})
				if got := awaitCloudTask(t, s, power.ID); got.State != tasks.Succeeded {
					t.Fatal(got)
				}
				if got, _ := s.Get(follower.ID); got.State != tasks.Queued {
					t.Fatal("uncertain admission group released", got)
				}
			} else if got.State != tasks.Succeeded {
				t.Fatal(got)
			}
		})
	}
}

func isTaskConflict(err error) bool {
	var conflict *tasks.ConflictError
	return errors.As(err, &conflict)
}

func (r *cloudVMBackend) SnapshotCreationBinding(node string) (json.RawMessage, error) {
	return json.Marshal(r.listeners[node])
}
func (r *cloudVMBackend) ApplyCreationBinding(vm *compute.VirtualMachine, binding json.RawMessage) error {
	return json.Unmarshal(binding, &vm.Graphic.Listen)
}

func TestCloudCreatePinsPolicyAndSelectedHostBinding(t *testing.T) {
	s, backend, _, cloud := cloudFixture(t, 4)
	task := submitCloud(t, s, "accepted-policy", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("pinned")})
	// All private bindings and the type policy must remain as accepted, including
	// when scheduling does not choose a host until execution starts.
	backend.listeners["a"] = "192.0.2.4"
	cloud.InstanceTypes[0].Autostart = false
	runCloudService(t, s)
	result := awaitCloudTask(t, s, task.ID)
	vm, err := backend.Get(taskMachineID(t, result), "a")
	if err != nil || result.State != tasks.Succeeded {
		t.Fatalf("creation failed: %+v %v", result, err)
	}
	if !vm.Autostart || vm.GuestAgent || vm.Graphic.Type != compute.GraphicTypeSpice || vm.VideoModel != compute.VideoModelQxl || vm.Graphic.Listen != "127.0.0.2" {
		t.Fatalf("accepted policy changed: %+v", vm)
	}
}

func TestTypeChangePinsAutostartAndDoesNotRewriteOnCatalogEdit(t *testing.T) {
	s, backend, _, cloud := cloudFixture(t, 4)
	backend.machines["a/v"] = compute.VirtualMachine{Id: "v", Name: "v", NodeId: "a", Arch: compute.ArchAmd64, State: compute.StateStopped, VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG), Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z", CPURequest: 1, MemoryRequestBytes: 1 << 30}, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone}
	task := submitCloud(t, s, "accepted-type-policy", "machine.change-type", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "v", InstanceType: "small"})
	cloud.InstanceTypes[0].Autostart = false
	before, _ := backend.Get("v", "a")
	if before.Autostart {
		t.Fatal("catalog edit rewrote an existing VM")
	}
	runCloudService(t, s)
	if result := awaitCloudTask(t, s, task.ID); result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	after, _ := backend.Get("v", "a")
	if !after.Autostart || after.State != compute.StateStopped {
		t.Fatalf("type policy not pinned or changed power: %+v", after)
	}
}

func (r *cloudVMBackend) SaveResources(vm *compute.VirtualMachine) error { return r.Save(vm) }

func (r *cloudVMBackend) Start(id, node string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	vm := r.machines[node+"/"+id]
	vm.State = compute.StateRunning
	r.machines[node+"/"+id] = vm
	r.starts++
	return nil
}

func TestCreateStartIsIndependentOfAutostart(t *testing.T) {
	s, backend, _, cloud := cloudFixture(t, 4)
	cloud.InstanceTypes[0].Autostart = false
	spec := createSpec("start-once")
	spec.Start = true
	task := submitCloud(t, s, "one-time-start", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: spec})
	runCloudService(t, s)
	result := awaitCloudTask(t, s, task.ID)
	if result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	vm, _ := backend.Get(taskMachineID(t, result), "a")
	if vm.Autostart || vm.State != compute.StateRunning || backend.starts != 1 {
		t.Fatalf("one-time start replaced persistent policy: %+v", vm)
	}
}

func TestUnsupportedDisplayIsNotOfferedOrProvisioned(t *testing.T) {
	s, backend, volumes, cloud := cloudFixture(t, 4)
	backend.unsupportedDisplay = true
	repo, err := catalog.NewImageRepository([]compute.Image{{ID: "image", Arch: compute.ArchAmd64, SourcePath: "/source", Defaults: compute.ImageDefaults{Graphics: "spice", VideoModel: "qxl", Initialization: "none"}}})
	if err != nil {
		t.Fatal(err)
	}
	vms := compute.NewVirtualMachineService(backend)
	volumeService := compute.NewVolumeService(volumes)
	manager := compute.NewVirtualMachineManager(vms, volumeService, cloudEvents{}, nil, compute.NewImageService(repo))
	service := &compute.CloudService{VMs: vms, Volumes: volumeService, Nodes: compute.NewNodeService(cloudNodeBackend{cpus: 4}), Pools: compute.NewVolumePoolService(cloudPoolBackend{}), Manager: manager}
	offers, err := service.Offers(cloud)
	if err != nil || len(offers) != 0 {
		t.Fatalf("unsupported display offered: %+v %v", offers, err)
	}
	task := submitCloud(t, s, "unsupported-display", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("no-display")})
	runCloudService(t, s)
	if result := awaitCloudTask(t, s, task.ID); result.State != tasks.Failed || result.OutcomeUncertain || volumes.created != 0 || backend.mutations != 0 {
		t.Fatalf("unsupported display caused side effects: %+v", result)
	}
}

func taskMachineID(t *testing.T, task tasks.Task) string {
	t.Helper()
	for _, ref := range task.Resources {
		if ref.Kind == "machine" {
			return ref.ID
		}
	}
	t.Fatal("task has no machine identity")
	return ""
}

func TestCloudCreationNamesAreHostLocal(t *testing.T) {
	s, backend, volumes, _ := cloudFixture(t, 8)
	backend.machines["a/existing"] = compute.VirtualMachine{Id: "existing", Name: "same", NodeId: "a", VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG)}
	spec := createSpec("same")
	spec.Placement.Required["rack"] = "two"
	first := submitCloud(t, s, "same-name-other-host", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: spec})
	runCloudService(t, s)
	created := awaitCloudTask(t, s, first.ID)
	if created.State != tasks.Succeeded {
		t.Fatalf("cloud-wide name constraint remains: %+v", created)
	}
	vm, err := backend.Get(taskMachineID(t, created), "b")
	if err != nil || vm.Id == "same" || vm.Name != "same" {
		t.Fatalf("name and identity conflated: %+v %v", vm, err)
	}
	second := submitCloud(t, s, "same-name-same-host", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: spec})
	rejected := awaitCloudTask(t, s, second.ID)
	if rejected.State != tasks.Failed || rejected.OutcomeUncertain || volumes.created != 1 {
		t.Fatalf("host-local collision provisioned resources: %+v", rejected)
	}
}
