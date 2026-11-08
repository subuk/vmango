package compute

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"subuk/vmango/tasks"
	"time"
)

// ForNamespace binds every repository operation to an explicit workload context.
// The original service remains the infrastructure view used for shared capacity.
func (s *VirtualMachineService) ForNamespace(c WorkloadContext, workload *WorkloadService) (*VirtualMachineService, error) {
	if workload == nil || workload.Clouds == nil || workload.Assignments == nil {
		return nil, ErrUnavailable
	}
	if err := workload.Namespaces.Admit(c.Namespace, c.Cloud); err != nil {
		return nil, err
	}
	return &VirtualMachineService{VirtualMachineRepository: &namespaceVMRepository{VirtualMachineRepository: s.VirtualMachineRepository, context: c, workload: workload}, CloudMode: s.CloudMode, Dependencies: s.Dependencies}, nil
}

func (s *VolumeService) ForNamespace(c WorkloadContext, workload *WorkloadService) (*VolumeService, error) {
	if workload == nil || workload.Clouds == nil || workload.Assignments == nil {
		return nil, ErrUnavailable
	}
	if err := workload.Namespaces.Admit(c.Namespace, c.Cloud); err != nil {
		return nil, err
	}
	return &VolumeService{VolumeRepository: &namespaceVolumeRepository{VolumeRepository: s.VolumeRepository, context: c, workload: workload}, Ownership: s.Ownership, Dependencies: s.Dependencies}, nil
}

func (s *WorkloadService) Bind(c WorkloadContext, manager *VirtualMachineManager) (*VirtualMachineManager, *VirtualMachineService, *VolumeService, error) {
	return s.bindWithSources(c, manager, nil)
}

// bindWithSources permits only explicitly accepted cloud image sources to be
// read and cloned while provisioning. It never grants attach, resize or delete.
func (s *WorkloadService) bindWithSources(c WorkloadContext, manager *VirtualMachineManager, sources map[string]bool, ownerVM ...string) (*VirtualMachineManager, *VirtualMachineService, *VolumeService, error) {
	vms, err := s.Clouds.VMs.ForNamespace(c, s)
	if err != nil {
		return nil, nil, nil, err
	}
	volumes, err := s.Clouds.Volumes.ForNamespace(c, s)
	if err != nil {
		return nil, nil, nil, err
	}
	allowed := map[string]bool{}
	for key, value := range sources {
		allowed[key] = value
	}
	volumes.VolumeRepository.(*namespaceVolumeRepository).sources = allowed
	if len(ownerVM) > 0 {
		volumes.VolumeRepository.(*namespaceVolumeRepository).ownerVM = ownerVM[0]
		vms.VirtualMachineRepository.(*namespaceVMRepository).ownerVM = ownerVM[0]
	}
	if manager == nil {
		return nil, vms, volumes, nil
	}
	bound := NewVirtualMachineManager(vms, volumes, manager.epub, manager.settings, manager.images)
	bound.creationBindings, bound.Clouds, bound.createMu = manager.creationBindings, manager.Clouds, manager.coordinationLock()
	bound.appliedPolicies = manager.appliedPolicies
	return bound, vms, volumes, nil
}

type namespaceVMRepository struct {
	ownerVM string
	VirtualMachineRepository
	context  WorkloadContext
	workload *WorkloadService
}

func (r *namespaceVMRepository) host(node string) error {
	if err := r.workload.Namespaces.Admit(r.context.Namespace, r.context.Cloud); err != nil {
		return err
	}
	_, _, err := r.workload.Clouds.ForHost(r.context.Cloud, node)
	return err
}

func (r *namespaceVMRepository) Get(id, node string) (*VirtualMachine, error) {
	if err := r.host(node); err != nil {
		return nil, err
	}
	vm, err := r.VirtualMachineRepository.Get(id, node)
	if err != nil {
		return nil, err
	}
	if err := r.workload.CheckMachine(r.context, vm, false); err != nil {
		return nil, err
	}
	return vm, nil
}

func (r *namespaceVMRepository) List(options VirtualMachineListOptions) ([]*VirtualMachine, error) {
	options.Strict = true
	if err := r.workload.Namespaces.Admit(r.context.Namespace, r.context.Cloud); err != nil {
		return nil, err
	}
	if len(options.NodeIds) == 0 {
		cloud, err := r.workload.Clouds.Get(r.context.Cloud)
		if err != nil {
			return nil, err
		}
		for _, host := range cloud.Hosts {
			options.NodeIds = append(options.NodeIds, host.Node)
		}
	}
	for _, node := range options.NodeIds {
		if err := r.host(node); err != nil {
			return nil, err
		}
	}
	machines, err := r.VirtualMachineRepository.List(options)
	if err != nil {
		return nil, err
	}
	result := []*VirtualMachine{}
	for _, vm := range machines {
		if err := r.workload.CheckMachine(r.context, vm, false); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		result = append(result, vm)
	}
	return result, nil
}

func (r *namespaceVMRepository) graph(id, node string) (*VirtualMachine, error) {
	vm, err := r.Get(id, node)
	if err != nil {
		return nil, err
	}
	if err := r.workload.CheckGraph(r.context, vm, false); err != nil {
		return nil, err
	}
	return vm, nil
}

func (r *namespaceVMRepository) ValidateNew(vm *VirtualMachine) error {
	if vm == nil {
		return ErrInvalid
	}
	if err := r.host(vm.NodeId); err != nil {
		return err
	}
	if vm.NamespaceID != "" && vm.NamespaceID != r.context.Namespace {
		return ErrNotFound
	}
	vm.NamespaceID = r.context.Namespace
	if vm.Label == "" {
		vm.Label = vm.Name
	}
	if err := r.workload.CheckMachineLabel(r.context, vm.Label, vm.Id); err != nil {
		return err
	}
	for _, disk := range vm.Volumes {
		v, err := r.workload.Clouds.Volumes.Get(disk.Path, vm.NodeId)
		if err != nil {
			return err
		}
		if err := r.workload.CheckVolume(r.context, v, false); err != nil {
			return err
		}
	}
	return r.VirtualMachineRepository.ValidateNew(vm)
}

func (r *namespaceVMRepository) Save(vm *VirtualMachine) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if vm == nil {
		return ErrInvalid
	}
	if err := r.host(vm.NodeId); err != nil {
		return err
	}
	previous, err := r.VirtualMachineRepository.Get(vm.Id, vm.NodeId)
	if err == nil {
		if err := r.workload.CheckGraph(r.context, previous, false); err != nil {
			return err
		}
		if vm.NamespaceID != r.context.Namespace {
			return ErrNotFound
		}
		if err := r.workload.CheckGraph(r.context, vm, false); err != nil {
			return err
		}
	} else if !errors.Is(err, ErrNotFound) {
		return err
	} else if err := r.ValidateNew(vm); err != nil {
		return err
	}
	return r.VirtualMachineRepository.Save(vm)
}

func (r *namespaceVMRepository) SaveResources(vm *VirtualMachine) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if vm == nil {
		return ErrInvalid
	}
	if _, err := r.graph(vm.Id, vm.NodeId); err != nil {
		return err
	}
	if vm.NamespaceID != r.context.Namespace {
		return ErrNotFound
	}
	backend, ok := r.VirtualMachineRepository.(MachineResourceRepository)
	if !ok {
		return ErrUnavailable
	}
	return backend.SaveResources(vm)
}

func (r *namespaceVMRepository) Delete(id, node string) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	return r.VirtualMachineRepository.Delete(id, node)
}

func (r *namespaceVMRepository) AttachVolume(id, node string, disk *VirtualMachineAttachedVolume) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if disk == nil {
		return ErrInvalid
	}
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	v, err := r.workload.Clouds.Volumes.Get(disk.Path, node)
	if err != nil {
		return err
	}
	if err := r.workload.CheckVolume(r.context, v, false); err != nil {
		return err
	}
	if v.Metadata.Hidden && (v.OwnerVM == "" || v.OwnerVM != id || r.ownerVM != id) {
		return ErrNotFound
	}
	return r.VirtualMachineRepository.AttachVolume(id, node, disk)
}

func (r *namespaceVMRepository) DetachVolume(id, node, path string) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	return r.VirtualMachineRepository.DetachVolume(id, node, path)
}

func (r *namespaceVMRepository) AttachInterface(id, node string, iface *VirtualMachineAttachedInterface) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	return r.VirtualMachineRepository.AttachInterface(id, node, iface)
}
func (r *namespaceVMRepository) DetachInterface(id, node, mac string) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	return r.VirtualMachineRepository.DetachInterface(id, node, mac)
}
func (r *namespaceVMRepository) Shutdown(id, node string) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	return r.VirtualMachineRepository.Shutdown(id, node)
}
func (r *namespaceVMRepository) Poweroff(id, node string) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	return r.VirtualMachineRepository.Poweroff(id, node)
}
func (r *namespaceVMRepository) Reboot(id, node string) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	return r.VirtualMachineRepository.Reboot(id, node)
}
func (r *namespaceVMRepository) Start(id, node string) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	return r.VirtualMachineRepository.Start(id, node)
}
func (r *namespaceVMRepository) GetConsoleStream(id, node string, force bool) (VirtualMachineConsoleStream, error) {
	if _, err := r.graph(id, node); err != nil {
		return nil, err
	}
	return r.VirtualMachineRepository.GetConsoleStream(id, node, force)
}
func (r *namespaceVMRepository) GetGraphicStream(id, node string) (VirtualMachineGraphicStream, error) {
	if _, err := r.graph(id, node); err != nil {
		return nil, err
	}
	return r.VirtualMachineRepository.GetGraphicStream(id, node)
}
func (r *namespaceVMRepository) Capabilities(node string, arch Arch) (VirtualMachineCapabilities, error) {
	if err := r.host(node); err != nil {
		return VirtualMachineCapabilities{}, err
	}
	return r.VirtualMachineRepository.Capabilities(node, arch)
}
func (r *namespaceVMRepository) ValidateNetwork(node string, nic *VirtualMachineAttachedInterface) error {
	if err := r.host(node); err != nil {
		return err
	}
	if backend, ok := r.VirtualMachineRepository.(NetworkValidator); ok {
		return backend.ValidateNetwork(node, nic)
	}
	return nil
}
func (r *namespaceVMRepository) SnapshotCreationBinding(node string) (json.RawMessage, error) {
	if err := r.host(node); err != nil {
		return nil, err
	}
	if backend, ok := r.VirtualMachineRepository.(CreationBindingRepository); ok {
		return backend.SnapshotCreationBinding(node)
	}
	return nil, nil
}
func (r *namespaceVMRepository) ApplyCreationBinding(vm *VirtualMachine, binding json.RawMessage) error {
	if vm == nil {
		return ErrInvalid
	}
	if err := r.host(vm.NodeId); err != nil {
		return err
	}
	if backend, ok := r.VirtualMachineRepository.(CreationBindingRepository); ok {
		return backend.ApplyCreationBinding(vm, binding)
	}
	return nil
}
func (r *namespaceVMRepository) Adopt(id, node string, metadata *MachineMetadata) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	if backend, ok := r.VirtualMachineRepository.(MachineAdoptionRepository); ok {
		return backend.Adopt(id, node, metadata)
	}
	return ErrUnavailable
}

type namespaceVolumeRepository struct {
	ownerVM string
	VolumeRepository
	context  WorkloadContext
	workload *WorkloadService
	sources  map[string]bool
}

func (r *namespaceVolumeRepository) host(node string) error {
	if err := r.workload.Namespaces.Admit(r.context.Namespace, r.context.Cloud); err != nil {
		return err
	}
	_, _, err := r.workload.Clouds.ForHost(r.context.Cloud, node)
	return err
}
func (r *namespaceVolumeRepository) source(node, path string) bool {
	return r.sources[VolumePolicyKey(node, path)]
}
func (r *namespaceVolumeRepository) Get(path, node string) (*Volume, error) {
	if err := r.host(node); err != nil {
		return nil, err
	}
	v, err := r.VolumeRepository.Get(path, node)
	if err != nil {
		return nil, err
	}
	if !r.source(node, path) {
		if err := r.workload.CheckVolume(r.context, v, false); err != nil {
			return nil, err
		}
	}
	if v.OwnerVM != "" {
		if v.OwnerVM != r.ownerVM {
			return nil, ErrNotFound
		}
		v.Metadata.Hidden = false
	}
	return v, nil
}

func (r *namespaceVolumeRepository) List(options VolumeListOptions) ([]*Volume, error) {
	result, err := r.ListInventory(options)
	if err != nil {
		return nil, err
	}
	if len(result.UnavailableHosts) != 0 {
		return nil, ErrUnavailable
	}
	return result.Volumes, nil
}

func (r *namespaceVolumeRepository) ListInventory(options VolumeListOptions) (VolumeInventory, error) {
	if err := r.workload.Namespaces.Admit(r.context.Namespace, r.context.Cloud); err != nil {
		return VolumeInventory{}, err
	}
	if len(options.NodeIds) == 0 {
		cloud, err := r.workload.Clouds.Get(r.context.Cloud)
		if err != nil {
			return VolumeInventory{}, err
		}
		for _, host := range cloud.Hosts {
			options.NodeIds = append(options.NodeIds, host.Node)
		}
	}
	for _, node := range options.NodeIds {
		if err := r.host(node); err != nil {
			return VolumeInventory{}, err
		}
	}
	var result VolumeInventory
	var err error
	if repo, ok := r.VolumeRepository.(VolumeInventoryRepository); ok {
		result, err = repo.ListInventory(options)
	} else {
		result.Volumes, err = r.VolumeRepository.List(options)
	}
	if err != nil {
		return VolumeInventory{}, err
	}
	visible := []*Volume{}
	for _, v := range result.Volumes {
		if err := r.workload.CheckVolume(r.context, v, false); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return VolumeInventory{}, err
		}
		if !v.Metadata.Hidden {
			visible = append(visible, v)
		}
	}
	result.Volumes = visible
	return result, nil
}

func (r *namespaceVolumeRepository) ResolvePath(node, pool, name string) (string, error) {
	if err := r.host(node); err != nil {
		return "", err
	}
	return r.VolumeRepository.ResolvePath(node, pool, name)
}
func (r *namespaceVolumeRepository) ValidateCreate(p VolumeCreateParams) error {
	if err := r.host(p.NodeId); err != nil {
		return err
	}
	if p.OwnerVM != "" && p.OwnerVM != r.ownerVM {
		return ErrNotFound
	}
	if p.NamespaceID != "" && p.NamespaceID != r.context.Namespace {
		return ErrNotFound
	}
	if p.Cloud != "" && p.Cloud != r.context.Cloud {
		return ErrNotFound
	}
	label := p.Label
	if label == "" {
		label = p.Name
	}
	if err := r.workload.CheckVolumeLabel(r.context, p.NodeId, p.Pool, label, ""); err != nil {
		return err
	}
	return r.VolumeRepository.ValidateCreate(p)
}

func assignmentPersistenceFailure(v *Volume, action string, err error) error {
	return &tasks.DiagnosticError{Details: tasks.Diagnostic{Message: "Volume namespace persistence failed after backend change", Backend: "filesystem", Operation: "record-volume-namespace", Resource: ptrVolumeRef(v)}, Cause: err, Effects: []tasks.Effect{{Resource: VolumeRef(v.NodeId, v.Path), Action: action, At: time.Now().UTC()}}}
}
func ptrVolumeRef(v *Volume) *tasks.ResourceRef { ref := VolumeRef(v.NodeId, v.Path); return &ref }

func (r *namespaceVolumeRepository) checkNewAssignment(node, pool, name string) error {
	guard, ok := r.workload.Assignments.(VolumeAssignmentCreationRepository)
	if !ok {
		return nil
	}
	path, err := r.VolumeRepository.ResolvePath(node, pool, name)
	if err != nil {
		return err
	}
	if _, err := r.VolumeRepository.Get(path, node); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrVolumeNotFound) {
		if err != nil {
			return err
		}
		return fmt.Errorf("%w: destination volume already exists", ErrConflict)
	}
	return guard.CheckCreate(node, path, r.workload.Scopes[node])
}
func (r *namespaceVolumeRepository) record(v *Volume, label, ownerVM string) error {
	if v == nil {
		return fmt.Errorf("backend returned no created volume")
	}
	if err := r.workload.Assignments.Put(v, r.workload.Scopes[v.NodeId], VolumeAssignment{NamespaceID: r.context.Namespace, Label: label, OwnerVM: ownerVM}); err != nil {
		return assignmentPersistenceFailure(v, "created", err)
	}
	if err := r.workload.ReadVolume(v); err != nil {
		return assignmentPersistenceFailure(v, "created", err)
	}
	if v.NamespaceID != r.context.Namespace || v.Label != label || v.OwnerVM != ownerVM {
		return assignmentPersistenceFailure(v, "created", errors.New("assignment verification failed"))
	}
	return nil
}
func (r *namespaceVolumeRepository) Create(p VolumeCreateParams) (*Volume, error) {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if err := r.ValidateCreate(p); err != nil {
		return nil, err
	}
	if err := r.checkNewAssignment(p.NodeId, p.Pool, p.Name); err != nil {
		return nil, err
	}
	p.NamespaceID = r.context.Namespace
	if p.Label == "" {
		p.Label = p.Name
	}
	v, err := r.VolumeRepository.Create(p)
	if err != nil {
		return v, err
	}
	return v, r.record(v, p.Label, p.OwnerVM)
}
func (r *namespaceVolumeRepository) Clone(p VolumeCloneParams) (*Volume, error) {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	source, err := r.Get(p.OriginalPath, p.NodeId)
	if err != nil {
		return nil, err
	}
	if source.OwnerVM != "" {
		return nil, ErrNotFound
	}
	create := VolumeCreateParams{NamespaceID: p.NamespaceID, Label: p.Label, Cloud: p.Cloud, NodeId: p.NodeId, Name: p.NewName, Pool: p.NewPool, Size: p.NewSize, Format: p.Format}
	if err := r.ValidateCreate(create); err != nil {
		return nil, err
	}
	if err := r.checkNewAssignment(p.NodeId, p.NewPool, p.NewName); err != nil {
		return nil, err
	}
	p.NamespaceID = r.context.Namespace
	if p.Label == "" {
		p.Label = p.NewName
	}
	v, err := r.VolumeRepository.Clone(p)
	if err != nil {
		return v, err
	}
	return v, r.record(v, p.Label, "")
}
func (r *namespaceVolumeRepository) mutable(path, node string) (*Volume, error) {
	v, err := r.Get(path, node)
	if err != nil {
		return nil, err
	}
	if err := r.workload.CheckVolume(r.context, v, false); err != nil {
		return nil, err
	}
	if v.AttachedTo != "" {
		vm, err := r.workload.Clouds.VMs.Get(v.AttachedTo, node)
		if err != nil {
			return nil, err
		}
		if err := r.workload.CheckGraph(r.context, vm, false); err != nil {
			return nil, err
		}
	}
	return v, nil
}
func (r *namespaceVolumeRepository) Resize(path, node string, size Size) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.mutable(path, node); err != nil {
		return err
	}
	return r.VolumeRepository.Resize(path, node, size)
}
func (r *namespaceVolumeRepository) Delete(path, node string) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	v, err := r.mutable(path, node)
	if err != nil {
		return err
	}
	if err := r.VolumeRepository.Delete(path, node); err != nil {
		return err
	}
	if _, err := r.VolumeRepository.Get(path, node); !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrVolumeNotFound) {
		if err == nil {
			err = errors.New("deleted volume still exists")
		}
		return assignmentPersistenceFailure(v, "delete-unverified", err)
	}
	if err := r.workload.Assignments.Delete(v, r.workload.Scopes[node]); err != nil {
		return assignmentPersistenceFailure(v, "deleted", err)
	}
	return nil
}
func (r *namespaceVolumeRepository) Upload(path, node string, content io.Reader, size uint64) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.mutable(path, node); err != nil {
		return err
	}
	return r.VolumeRepository.Upload(path, node, content, size)
}

func (r *namespaceVMRepository) ResolveHardware(vm *VirtualMachine, policy, binding json.RawMessage) error {
	if err := r.host(vm.NodeId); err != nil {
		return err
	}
	if backend, ok := r.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.ResolveHardware(vm, policy, binding)
	}
	if len(policy) != 0 {
		return ErrUnavailable
	}
	return nil
}
func (r *namespaceVMRepository) ApplyHardware(vm *VirtualMachine, policy json.RawMessage) error {
	if err := r.host(vm.NodeId); err != nil {
		return err
	}
	if backend, ok := r.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.ApplyHardware(vm, policy)
	}
	if len(policy) != 0 {
		return ErrUnavailable
	}
	return nil
}
func (r *namespaceVMRepository) ValidateAppliedPolicy(vm *VirtualMachine) error {
	if backend, ok := r.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.ValidateAppliedPolicy(vm)
	}
	return nil
}
func (r *namespaceVMRepository) ResolveDiskAttachment(vm *VirtualMachine, disk *VirtualMachineAttachedVolume) error {
	if backend, ok := r.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.ResolveDiskAttachment(vm, disk)
	}
	return nil
}
func (r *namespaceVMRepository) ResolveInterfaceAttachment(vm *VirtualMachine, nic *VirtualMachineAttachedInterface) error {
	if backend, ok := r.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.ResolveInterfaceAttachment(vm, nic)
	}
	return nil
}
func (r *namespaceVMRepository) ValidateResourceChange(current, requested *VirtualMachine) error {
	if backend, ok := r.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.ValidateResourceChange(current, requested)
	}
	return nil
}
func (r *namespaceVMRepository) PrepareAdoption(vm *VirtualMachine, cloud, policyRef string) (json.RawMessage, error) {
	if err := r.host(vm.NodeId); err != nil {
		return nil, err
	}
	if backend, ok := r.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.PrepareAdoption(vm, cloud, policyRef)
	}
	return nil, ErrUnavailable
}
func (r *namespaceVMRepository) AdoptWithPolicy(id, node string, metadata *MachineMetadata, policy json.RawMessage) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	if backend, ok := r.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.AdoptWithPolicy(id, node, metadata, policy)
	}
	return ErrUnavailable
}
func (r *namespaceVMRepository) SetAutostart(id, node string, enabled bool) error {
	r.workload.Namespaces.mutationMu.Lock()
	defer r.workload.Namespaces.mutationMu.Unlock()
	if _, err := r.graph(id, node); err != nil {
		return err
	}
	if backend, ok := r.VirtualMachineRepository.(MachineAutostartRepository); ok {
		return backend.SetAutostart(id, node, enabled)
	}
	return nil
}
func (r *namespaceVMRepository) ValidateAdoptionPolicy(vm *VirtualMachine, policy json.RawMessage) error {
	if err := r.host(vm.NodeId); err != nil {
		return err
	}
	if backend, ok := r.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.ValidateAdoptionPolicy(vm, policy)
	}
	return ErrUnavailable
}
