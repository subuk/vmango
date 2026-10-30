package compute_test

import (
	"context"
	"encoding/json"
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
	mu        sync.Mutex
	machines  map[string]compute.VirtualMachine
	inventory []string
	failHost  string
	mutations int
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
	return compute.VirtualMachineCapabilities{Arch: arch, MaxVCPUs: 16, Firmwares: []string{"bios"}, DiskBuses: []compute.DeviceBus{compute.DeviceBusVirtio}, NICModels: []string{"virtio"}, Graphics: []compute.GraphicType{compute.GraphicTypeVnc, compute.GraphicTypeNone}, VideoModels: []compute.VideoModel{compute.VideoModelNone}}, nil
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
	mu      sync.Mutex
	created int
	deleted int
}

func (r *cloudVolumeBackend) Get(path, node string) (*compute.Volume, error) {
	return &compute.Volume{NodeId: node, Path: path, Name: "source", Pool: "pool", Format: compute.VolumeFormatRaw, Size: compute.NewSize(1, compute.SizeUnitG), Identity: node + path}, nil
}
func (r *cloudVolumeBackend) ValidateCreate(compute.VolumeCreateParams) error { return nil }
func (r *cloudVolumeBackend) ResolvePath(node, pool, name string) (string, error) {
	return "/pool/" + name, nil
}
func (r *cloudVolumeBackend) Clone(p compute.VolumeCloneParams) (*compute.Volume, error) {
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
	imageRepo, err := catalog.NewImageRepository([]compute.Image{{ID: "image", Name: "Image", OSName: "Linux", Arch: compute.ArchAmd64, SourcePath: "/source", Defaults: compute.ImageDefaults{Initialization: "none"}}})
	if err != nil {
		t.Fatal(err)
	}
	images := compute.NewImageService(imageRepo)
	vmBackend := &cloudVMBackend{machines: map[string]compute.VirtualMachine{}}
	volumeBackend := &cloudVolumeBackend{}
	vms := compute.NewVirtualMachineService(vmBackend)
	vms.CloudMode = true
	volumes := compute.NewVolumeService(volumeBackend)
	nodes := compute.NewNodeService(cloudNodeBackend{cpus: cpus})
	pools := compute.NewVolumePoolService(cloudPoolBackend{})
	manager := compute.NewVirtualMachineManager(vms, volumes, cloudEvents{}, nil, images)
	c := compute.Cloud{ID: "c", Name: "Cloud", Zones: []compute.CatalogEntry{{ID: "z", Name: "Zone"}}, InstanceTypes: []compute.InstanceType{{ID: "small", Name: "Small", VCPUs: 2, CPURequest: 2, MemoryBytes: 2 << 30, MemoryRequestBytes: 2 << 30, BootDiskBytes: 2 << 30}}, Images: []string{"image"}, Networks: []compute.CatalogEntry{{ID: "lan", Name: "LAN"}}, StorageClasses: []compute.CatalogEntry{{ID: "fast", Name: "Fast"}}, Hosts: []compute.CloudHost{{Node: "a", Zone: "z", Labels: map[string]string{"rack": "one"}, Networks: map[string]compute.NetworkBinding{"lan": {Network: "net"}}, Storage: map[string]compute.StorageBinding{"fast": {Pool: "pool", Format: compute.VolumeFormatRaw}}}, {Node: "b", Zone: "z", Labels: map[string]string{"rack": "two"}, Networks: map[string]compute.NetworkBinding{"lan": {Network: "net"}}, Storage: map[string]compute.StorageBinding{"fast": {Pool: "pool", Format: compute.VolumeFormatRaw}}}}}
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
	return &compute.MachineCreateSpec{ID: name, Image: "image", InstanceType: "small", Zone: "z", Network: "lan", StorageClass: "fast", Placement: compute.Placement{Required: map[string]string{"rack": "one"}}}
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
func TestCloudInventoryFailurePreventsProvisioning(t *testing.T) {
	s, backend, volumes, _ := cloudFixture(t, 4)
	backend.failHost = "b"
	task := submitCloud(t, s, "failed-inventory", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("one")})
	runCloudService(t, s)
	result := awaitCloudTask(t, s, task.ID)
	if result.State != tasks.Failed || result.OutcomeUncertain || backend.mutations != 0 || volumes.created != 0 {
		t.Fatalf("failed inventory mutated backend: %+v", result)
	}
}
func TestTypeChangeStaysOnHostAndPreservesDisks(t *testing.T) {
	for _, sufficient := range []bool{true, false} {
		t.Run(fmt.Sprint(sufficient), func(t *testing.T) {
			s, backend, _, _ := cloudFixture(t, 2)
			backend.machines["a/v"] = compute.VirtualMachine{Id: "v", NodeId: "a", Arch: compute.ArchAmd64, VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG), State: compute.StateStopped, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone, Volumes: []*compute.VirtualMachineAttachedVolume{{Path: "/existing"}}, Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z", CPURequest: 1, MemoryRequestBytes: 1 << 30}}
			if !sufficient {
				backend.machines["a/other"] = compute.VirtualMachine{Id: "other", NodeId: "a", VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG)}
			}
			task := submitCloud(t, s, "change-instance-type", "machine.change-type", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "v", InstanceType: "small"})
			runCloudService(t, s)
			result := awaitCloudTask(t, s, task.ID)
			v, _ := backend.Get("v", "a")
			if sufficient {
				if result.State != tasks.Succeeded || v.VCpus != 2 || v.Metadata.CPURequest != 2 || v.Volumes[0].Path != "/existing" {
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
	backend.machines["a/external"] = compute.VirtualMachine{Id: "external", NodeId: "a", VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG), State: compute.StateRunning, Volumes: []*compute.VirtualMachineAttachedVolume{{Path: "/external-disk"}}}
	if _, err := s.Submit(tasks.Identity{ID: "admin"}, "delete-before-adoption", compute.NewCommand("machine.delete", compute.InfrastructureCommand{Node: "a", VM: "external"})); err == nil {
		t.Fatal("unmanaged deletion accepted")
	}
	task := submitCloud(t, s, "explicit-adoption", "machine.adopt", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "external"})
	runCloudService(t, s)
	if result := awaitCloudTask(t, s, task.ID); result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	vm, _ := backend.Get("external", "a")
	if vm.Metadata == nil || vm.VCpus != 1 || vm.State != compute.StateRunning {
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
				vm, err := backend.Get("local", "a")
				if result.State != tasks.Succeeded || err != nil || vm.Volumes[0].Path != "/existing" {
					t.Fatalf("locality ignored: %+v %v", result, err)
				}
			}
		})
	}
}

func TestTypeChangeDoesNotSilentlyRewriteExplicitPinning(t *testing.T) {
	s, backend, _, _ := cloudFixture(t, 8)
	backend.machines["a/pinned"] = compute.VirtualMachine{Id: "pinned", NodeId: "a", Arch: compute.ArchAmd64, VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG), State: compute.StateStopped, Cpupin: &compute.VirtualMachineCpuPin{Vcpus: map[uint][]uint{0: {0}}}, Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z", CPURequest: 1, MemoryRequestBytes: 1 << 30}}
	task := submitCloud(t, s, "change-pinned-machine", "machine.change-type", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "pinned", InstanceType: "small"})
	runCloudService(t, s)
	result := awaitCloudTask(t, s, task.ID)
	if result.State != tasks.Failed || result.OutcomeUncertain || backend.mutations != 0 {
		t.Fatalf("explicit CPU pinning was rewritten: %+v", result)
	}
}

func TestCloudInterfaceAttachmentUsesAdministratorBinding(t *testing.T) {
	_, backend, _, cloud := cloudFixture(t, 4)
	cloud.Hosts[0].Networks["lan"] = compute.NetworkBinding{Network: "backend-net", Model: "virtio", VLAN: 7}
	backend.machines["a/v"] = compute.VirtualMachine{Id: "v", NodeId: "a", State: compute.StateStopped, Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z"}}
	vms := compute.NewVirtualMachineService(backend)
	handler := &compute.CommandHandler{Clouds: &compute.CloudService{Definitions: []compute.Cloud{cloud}}, VMs: vms}
	command, err := handler.Prepare(compute.NewCommand("machine.attach-interface", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "v", Interface: &compute.VirtualMachineAttachedInterface{NetworkName: "lan", Mac: "52:54:00:12:34:56", Model: "e1000", AccessVlan: 42}}))
	if err != nil {
		t.Fatal(err)
	}
	var prepared compute.InfrastructureCommand
	if err := json.Unmarshal(command.Payload, &prepared); err != nil {
		t.Fatal(err)
	}
	iface := prepared.Interface
	if iface.LogicalNetwork != "lan" || iface.NetworkName != "backend-net" || iface.Model != "virtio" || iface.AccessVlan != 7 || iface.Mac != "52:54:00:12:34:56" {
		t.Fatalf("administrator binding did not govern attachment: %+v", iface)
	}
}
