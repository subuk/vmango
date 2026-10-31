package compute

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func testType() InstanceType {
	return InstanceType{ID: "small", Name: "Small", VCPUs: 4, CPURequest: 2, MemoryBytes: 2 << 30, MemoryRequestBytes: 2 << 30, BootDiskBytes: 10 << 30}
}
func testInventory() HostInventory {
	return HostInventory{Host: CloudHost{Node: "a", CPUOvercommit: 1, CPUReserve: 1, MemoryReserveBytes: 1 << 30}, Node: &Node{Cpus: make([]NodeCpu, 8), Numas: []NodeNuma{{Memory: NewSize(16, SizeUnitG)}}}}
}
func TestSchedulingAccountsInactiveLegacyAndAppliedRequests(t *testing.T) {
	i := testInventory()
	legacy := &VirtualMachine{Id: "legacy", Name: "legacy", VCpus: 4, Memory: NewSize(4, SizeUnitG), State: StateStopped}
	managed := &VirtualMachine{Id: "managed", Name: "managed", VCpus: 4, Memory: NewSize(4, SizeUnitG), Metadata: &MachineMetadata{CPURequest: 1, MemoryRequestBytes: 4 << 30}}
	i.Machines = []*VirtualMachine{legacy, managed}
	fit, err := Fit(i, testType(), "")
	if err != nil || fit.FreeCPU != 0 || fit.FreeMemory != 5<<30 {
		t.Fatalf("incorrect request accounting: %+v %v", fit, err)
	}
	legacy.VCpus = 5
	if _, err := Fit(i, testType(), ""); err == nil {
		t.Fatal("inactive legacy domain was not counted")
	}
	managed.Metadata.MemoryRequestBytes = 1
	if _, err := Fit(i, testType(), ""); err == nil {
		t.Fatal("invalid persisted memory requests accepted")
	}
}
func TestTypeAdmissionReplacesCurrentRequests(t *testing.T) {
	i := testInventory()
	i.Machines = []*VirtualMachine{{Id: "current", VCpus: 6, Memory: NewSize(12, SizeUnitG)}}
	if _, err := Fit(i, testType(), ""); err == nil {
		t.Fatal("expected insufficient CPU")
	}
	if fit, err := Fit(i, testType(), "current"); err != nil || fit.FreeCPU != 5 || fit.FreeMemory != 13<<30 {
		t.Fatalf("old allocation was double counted: %+v %v", fit, err)
	}
}
func TestPlacementPreferenceAndDeterministicTieBreak(t *testing.T) {
	a := PlacementCandidate{Inventory: HostInventory{Host: CloudHost{Node: "a"}}, FreeCPU: 8}
	b := PlacementCandidate{Inventory: HostInventory{Host: CloudHost{Node: "b"}}, Preferred: 1, FreeCPU: 2}
	h, err := choose([]PlacementCandidate{a, b})
	if err != nil || h.Node != "b" {
		t.Fatal(h, err)
	}
	b.Preferred = 0
	b.FreeCPU = 8
	h, _ = choose([]PlacementCandidate{b, a})
	if h.Node != "a" {
		t.Fatal("tie-break is unstable")
	}
}
func TestPlacementCapabilitiesRejectUnknownAndEmptyLabels(t *testing.T) {
	c := Cloud{Hosts: []CloudHost{{Labels: map[string]string{"rack": "one"}}}}
	if c.ValidatePlacement(Placement{Required: map[string]string{"rack": "two"}}) != nil {
		t.Fatal("known label rejected")
	}
	for _, p := range []Placement{{Required: map[string]string{"node": "raw-node"}}, {Preferred: map[string]string{"rack": ""}}} {
		if c.ValidatePlacement(p) == nil {
			t.Fatal("unsupported label accepted")
		}
	}
}
func TestInstanceTypeRejectsMemoryOverselling(t *testing.T) {
	c := Cloud{ID: "local", Name: "Local", Zones: []CatalogEntry{{"z", "Zone"}}, Networks: []CatalogEntry{{"n", "Network"}}, StorageClasses: []CatalogEntry{{"s", "Storage"}}, InstanceTypes: []InstanceType{testType()}, Images: []string{"ubuntu"}, Hosts: []CloudHost{{Node: "a", Zone: "z"}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.InstanceTypes[0].MemoryRequestBytes--
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "memory overselling is not supported yet") {
		t.Fatal(err)
	}
}

type inventoryNodes struct {
	mu sync.Mutex
	NodeRepository
	reads []string
	fail  string
}

func (r *inventoryNodes) Get(node string, _ NodeGetOptions) (*Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads = append(r.reads, node)
	if node == r.fail {
		return nil, fmt.Errorf("connection lost")
	}
	return testInventory().Node, nil
}

type inventoryVMs struct {
	mu sync.Mutex
	VirtualMachineRepository
	strict []bool
	nodes  []string
}

func (r *inventoryVMs) List(o VirtualMachineListOptions) ([]*VirtualMachine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.strict = append(r.strict, o.Strict)
	r.nodes = append(r.nodes, o.NodeIds...)
	return []*VirtualMachine{{Id: "inactive", State: StateStopped, VCpus: 2, Memory: NewSize(2, SizeUnitG)}}, nil
}

type inventoryPools struct {
	VolumePoolRepository
	fail bool
}

func (r inventoryPools) List(VolumePoolListOptions) ([]*VolumePool, error) {
	if r.fail {
		return nil, fmt.Errorf("storage lost")
	}
	return []*VolumePool{{Name: "pool", Free: NewSize(100, SizeUnitG)}}, nil
}
func TestCloudInventoryExcludesFailedHostsEntirely(t *testing.T) {
	c := Cloud{Hosts: []CloudHost{{Node: "one", Zone: "z1"}, {Node: "two", Zone: "z2"}}}
	nodes := &inventoryNodes{}
	vms := &inventoryVMs{}
	s := CloudService{Nodes: NewNodeService(nodes), VMs: NewVirtualMachineService(vms), Pools: NewVolumePoolService(inventoryPools{})}
	result, err := s.Inventory(c)
	if err != nil || len(result) != 2 || len(nodes.reads) != 2 || len(vms.strict) != 2 || !vms.strict[0] || !vms.strict[1] {
		t.Fatalf("incomplete inventory: %+v %v", result, err)
	}
	nodes.fail = "two"
	if inv, err := s.Inventory(c); err != nil || len(inv) != 1 || inv[0].Host.Node != "one" || len(inv[0].Machines) == 0 {
		t.Fatal("failed host treated as empty")
	}
	nodes.fail = ""
	s.Pools = NewVolumePoolService(inventoryPools{fail: true})
	if _, err := s.Inventory(c); err == nil {
		t.Fatal("storage failure ignored")
	}
}
func TestStorageAdmissionAggregatesSharedPools(t *testing.T) {
	i := HostInventory{Pools: []*VolumePool{{Name: "same", Free: NewSize(10, SizeUnitG)}}}
	required := map[string]uint64{}
	_ = addStorage(required, "same", 6<<30)
	_ = addStorage(required, "same", 5<<30)
	if storageFits(i, required) == nil {
		t.Fatal("shared pool capacity double used")
	}
	if storageFits(i, map[string]uint64{"missing": 1}) == nil {
		t.Fatal("missing pool accepted")
	}
}

type missingCloudImageBackend struct{ ManagedImageBackend }

func (missingCloudImageBackend) BindImage(node, scope, pool, name string, bytes uint64) (ImageBinding, error) {
	return ImageBinding{Node: node, Scope: scope, Pool: pool, PoolID: "id", Name: name, Path: "/pool/source"}, nil
}
func (missingCloudImageBackend) InspectImage(ImageBinding) (*Volume, string, error) {
	return nil, "", ErrNotFound
}

type missingCloudArtifacts struct{}

func (missingCloudArtifacts) Get(string) (ImageArtifact, error) {
	return ImageArtifact{}, ErrArtifactNotFound
}
func (missingCloudArtifacts) Put(string, ImageArtifact) error {
	return fmt.Errorf("unexpected mutation")
}

type cloudImageVMBackend struct{ provisioningVMs }

func (cloudImageVMBackend) Capabilities(node string, arch Arch) (VirtualMachineCapabilities, error) {
	return VirtualMachineCapabilities{Arch: arch, MaxVCPUs: 16, DiskBuses: []DeviceBus{DeviceBusVirtio}, NICModels: []string{"virtio"}, Graphics: []GraphicType{GraphicTypeVnc}, VideoModels: []VideoModel{VideoModelNone}}, nil
}
func TestMissingRetrievableImageCanBePlacedAndSharesStorageBudget(t *testing.T) {
	image := Image{CloudID: "c", ID: "image", Arch: ArchAmd64, Defaults: ImageDefaults{Initialization: "none"}, Download: &ImageDownload{URL: "https://example.org/source", SHA256: strings.Repeat("a", 64), Format: VolumeFormatRaw, SizeBytes: 32 << 20}}
	images := NewImageService(pinnedImages{image})
	images.Preflight = true
	images.ManagedSources = NewManagedImageService(missingCloudImageBackend{}, missingCloudArtifacts{}, map[string]ImageBinding{"a": {Scope: "a", Pool: "pool"}}, ImageTransferLimits{MaxBytes: 1 << 30})
	volumes := &provisioningVolumes{failAt: -1}
	manager := NewVirtualMachineManager(NewVirtualMachineService(&cloudImageVMBackend{}), NewVolumeService(volumes), nil, nil, images)
	s := CloudService{Manager: manager, Volumes: manager.volumes}
	host := CloudHost{Node: "a", Zone: "z", Networks: map[string]NetworkBinding{"lan": {Network: "net"}}, Storage: map[string]StorageBinding{"fast": {Pool: "pool", Format: VolumeFormatRaw}}}
	cloud := Cloud{ID: "c", InstanceTypes: []InstanceType{testType()}, Hosts: []CloudHost{host}}
	i := testInventory()
	i.Host = host
	i.Node.CpuArch = ArchAmd64
	i.Pools = []*VolumePool{{Name: "pool", Free: NewSize(100, SizeUnitG)}}
	spec := MachineCreateSpec{Name: "new", Image: "image", InstanceType: "small", Zone: "z", Network: "lan", StorageClass: "fast"}
	command, err := s.ResolveCreate(cloud, spec, images, []HostInventory{i})
	if err != nil || command.Node != "a" || len(volumes.created) != 0 {
		t.Fatalf("missing downloadable image rejected or provisioned early: %+v %v", command, err)
	}
	i.Pools[0].Free = NewSize(testType().BootDiskBytes, SizeUnitB)
	if _, err := s.ResolveCreate(cloud, spec, images, []HostInventory{i}); err == nil {
		t.Fatal("image preparation space was not included alongside the boot disk")
	}
}

type hiddenCloudVolume struct{ VolumeRepository }

func (hiddenCloudVolume) Get(path, node string) (*Volume, error) {
	return &Volume{Path: path, NodeId: node, Metadata: VolumeMetadata{Hidden: true, Protected: true}}, nil
}
func TestHiddenSourcePoliciesPrecedeOwnershipChecks(t *testing.T) {
	volumes := NewVolumeService(hiddenCloudVolume{})
	h := CommandHandler{Volumes: volumes}
	if err := h.checkCloudMutation("volume.delete", &InfrastructureCommand{CloudID: "cloud", Node: "host", Path: "/private"}); err != ErrVolumeNotFound {
		t.Fatalf("hidden source exposed through ownership check: %v", err)
	}
	if err := volumes.Resize("/private", "host", NewSize(2, SizeUnitG)); err != ErrVolumeNotFound {
		t.Fatal(err)
	}
}

type ownershipMemory map[string]VolumeOwnership

func (m ownershipMemory) Get(key string) (VolumeOwnership, error) {
	o, ok := m[key]
	if !ok {
		return o, ErrNotFound
	}
	return o, nil
}
func (m ownershipMemory) Put(key string, o VolumeOwnership) error { m[key] = o; return nil }

type recyclableVolume struct {
	VolumeRepository
	volume  Volume
	deleted bool
}

func (r *recyclableVolume) Get(string, string) (*Volume, error) { v := r.volume; return &v, nil }
func (r *recyclableVolume) Delete(string, string) error         { r.deleted = true; return nil }
func TestDeletedVolumeDoesNotTransferOwnershipToRecreatedPath(t *testing.T) {
	backend := &recyclableVolume{volume: Volume{NodeId: "host", Path: "/disk", Pool: "pool", Identity: "pool-uuid/path-key"}}
	s := NewVolumeService(backend)
	s.Ownership = ownershipMemory{}
	if err := s.recordOwnership(&backend.volume, "cloud", "class"); err != nil {
		t.Fatal(err)
	}
	if !s.Owned(&backend.volume) {
		t.Fatal("confirmed creation was not owned")
	}
	if err := s.Delete("/disk", "host"); err != nil {
		t.Fatal(err)
	}
	if !backend.deleted || s.Owned(&backend.volume) {
		t.Fatal("recreated backend path inherited deleted disk ownership")
	}
}

func TestCPUOvercommit(t *testing.T) {
	for _, ratio := range []int{0, 1, 4} {
		i := testInventory()
		i.Host.CPUOvercommit = ratio
		effective := ratio
		if effective == 0 {
			effective = 4
		}
		typ := testType()
		typ.CPURequest = 7 * effective
		typ.VCPUs = typ.CPURequest
		if fit, err := Fit(i, typ, ""); err != nil || fit.FreeCPU != 0 {
			t.Fatalf("ratio %d: %+v %v", ratio, fit, err)
		}
		typ.CPURequest++
		if _, err := Fit(i, typ, ""); err == nil {
			t.Fatalf("ratio %d exceeded capacity", ratio)
		}
	}
}
