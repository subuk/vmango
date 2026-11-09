package compute

import (
	"context"
	"fmt"
	"subuk/vmango/tasks"
)

// WorkloadService enforces workload membership independently of transport.
// Clouds retains the unfiltered inventory used for shared capacity and inspection.
type WorkloadService struct {
	Namespaces  *NamespaceService
	Clouds      *CloudService
	Assignments VolumeAssignmentRepository
	Scopes      map[string]string
}

func (s *WorkloadService) ReadVolume(v *Volume) error {
	if s.Assignments == nil {
		return fmt.Errorf("%w: volume assignment storage unavailable", ErrUnavailable)
	}
	a, err := s.Assignments.Get(v, s.Scopes[v.NodeId])
	if err != nil {
		return err
	}
	v.NamespaceID, v.Label, v.OwnerVM = "", "", ""
	if a != nil {
		v.NamespaceID, v.Label, v.OwnerVM = a.NamespaceID, a.Label, a.OwnerVM
		if v.OwnerVM != "" {
			v.Metadata.Hidden = true
		}
	}
	return nil
}
func (s *WorkloadService) CheckMachine(c WorkloadContext, vm *VirtualMachine, unassigned bool) error {
	if err := s.Namespaces.Admit(c.Namespace, c.Cloud); err != nil {
		return err
	}
	if _, _, err := s.Clouds.ForHost(c.Cloud, vm.NodeId); err != nil {
		return err
	}
	if repo, ok := s.Clouds.VMs.VirtualMachineRepository.(MachineNamespaceRepository); ok {
		id, label, err := repo.GetNamespace(vm.Id, vm.NodeId)
		if err != nil {
			return err
		}
		vm.NamespaceID, vm.Label = id, label
	}
	if vm.NamespaceID != c.Namespace && !(unassigned && vm.NamespaceID == "") {
		return ErrNotFound
	}
	return nil
}
func (s *WorkloadService) CheckVolume(c WorkloadContext, v *Volume, unassigned bool) error {
	if err := s.Namespaces.Admit(c.Namespace, c.Cloud); err != nil {
		return err
	}
	if _, _, err := s.Clouds.ForHost(c.Cloud, v.NodeId); err != nil {
		return err
	}
	if err := s.ReadVolume(v); err != nil {
		return err
	}
	if v.NamespaceID != c.Namespace && !(unassigned && v.NamespaceID == "") {
		return ErrNotFound
	}
	return nil
}
func (s *WorkloadService) CheckGraph(c WorkloadContext, vm *VirtualMachine, unassigned bool) error {
	if err := s.CheckMachine(c, vm, unassigned); err != nil {
		return err
	}
	for _, d := range vm.Volumes {
		v, err := s.Clouds.Volumes.Get(d.Path, vm.NodeId)
		if err != nil {
			return err
		}
		if err = s.CheckVolume(c, v, unassigned); err != nil {
			return err
		}
		if v.OwnerVM != "" && v.OwnerVM != vm.Id {
			return ErrConflict
		}
	}
	return nil
}
func (s *WorkloadService) Machine(c WorkloadContext, id string) (*VirtualMachine, error) {
	vm, err := s.Clouds.GetMachine(context.Background(), c.Cloud, id)
	if err != nil {
		return nil, err
	}
	if err = s.CheckMachine(c, vm, false); err != nil {
		return nil, err
	}
	return vm, nil
}
func (s *WorkloadService) Volume(c WorkloadContext, id string) (*Volume, error) {
	v, err := s.Clouds.GetVolume(c.Cloud, id)
	if err != nil {
		return nil, err
	}
	if err = s.CheckVolume(c, v, false); err != nil {
		return nil, err
	}
	if v.OwnerVM != "" || v.Metadata.Hidden {
		return nil, ErrNotFound
	}
	return v, nil
}

// AssignmentPlan snapshots identifiers and graph edges before any writes. A plan
// is revalidated in full on apply and can be resumed after a partial write.
type AssignmentPlan struct {
	BackendScopes map[string]string `json:"backend_scopes"`
	Context       WorkloadContext   `json:"context"`
	Machines      []*VirtualMachine `json:"machines"`
	Volumes       []*Volume         `json:"volumes"`
}

func (s *WorkloadService) PreviewAssignment(c WorkloadContext, ids []string, disks []LocalDiskRef) (AssignmentPlan, error) {
	if len(ids) == 0 && len(disks) == 0 {
		return AssignmentPlan{}, ErrInvalid
	}
	p := AssignmentPlan{BackendScopes: map[string]string{}, Context: c, Machines: []*VirtualMachine{}, Volumes: []*Volume{}}
	if err := s.Namespaces.Admit(c.Namespace, c.Cloud); err != nil {
		return p, err
	}
	seen := map[string]bool{}
	add := func(node, path string) error {
		key := node + "\x00" + path
		if seen[key] {
			return nil
		}
		seen[key] = true
		v, err := s.Clouds.Volumes.Get(path, node)
		if err != nil {
			return err
		}
		if err = s.CheckVolume(c, v, true); err != nil {
			return err
		}
		p.BackendScopes[v.NodeId] = s.Scopes[v.NodeId]
		p.Volumes = append(p.Volumes, v)
		return nil
	}
	for _, id := range ids {
		vm, err := s.Clouds.GetMachine(context.Background(), c.Cloud, id)
		if err != nil {
			return p, err
		}
		if err = s.CheckMachine(c, vm, true); err != nil {
			return p, err
		}
		p.BackendScopes[vm.NodeId] = s.Scopes[vm.NodeId]
		p.Machines = append(p.Machines, vm)
		for _, d := range vm.Volumes {
			if err = add(vm.NodeId, d.Path); err != nil {
				return p, err
			}
			for _, v := range p.Volumes {
				if v.NodeId == vm.NodeId && v.Path == d.Path {
					if v.OwnerVM != "" && v.OwnerVM != vm.Id {
						return p, ErrConflict
					}
					if d.Purpose == PurposeConfigDrive && d.Managed {
						v.OwnerVM = vm.Id
					}
				}
			}
		}
	}
	for _, d := range disks {
		if err := add(d.Host, d.Path); err != nil {
			return p, err
		}
	}
	included := map[string]bool{}
	for _, vm := range p.Machines {
		included[vm.NodeId+"\x00"+vm.Id] = true
	}
	for _, v := range p.Volumes {
		if v.AttachedTo != "" && !included[v.NodeId+"\x00"+v.AttachedTo] {
			return p, fmt.Errorf("%w: include the attached machine in the assignment graph", ErrConflict)
		}
		if v.Metadata.Hidden && v.AttachedTo == "" {
			return p, ErrNotFound
		}
	}
	if err := s.validateAssignmentNames(p); err != nil {
		return p, err
	}
	return p, nil
}
func machineLabel(v *VirtualMachine) string {
	if v.Label != "" {
		return v.Label
	}
	return v.Name
}
func volumeLabel(v *Volume) string {
	if v.Label != "" {
		return v.Label
	}
	return v.Name
}
func (s *WorkloadService) validateAssignmentNames(p AssignmentPlan) error {
	for _, vm := range p.Machines {
		if err := s.CheckMachineLabel(p.Context, machineLabel(vm), vm.Id); err != nil {
			return err
		}
	}
	for _, v := range p.Volumes {
		if err := s.CheckVolumeLabel(p.Context, v.NodeId, v.Pool, volumeLabel(v), v.Path); err != nil {
			return err
		}
	}
	for i, a := range p.Machines {
		for _, b := range p.Machines[i+1:] {
			if a.Id != b.Id && machineLabel(a) == machineLabel(b) {
				return fmt.Errorf("%w: duplicate machine label in assignment", ErrConflict)
			}
		}
	}
	for i, a := range p.Volumes {
		for _, b := range p.Volumes[i+1:] {
			if a.NodeId == b.NodeId && a.Pool == b.Pool && a.Path != b.Path && volumeLabel(a) == volumeLabel(b) {
				return fmt.Errorf("%w: duplicate volume label in assignment", ErrConflict)
			}
		}
	}
	return nil
}
func (s *WorkloadService) ApplyAssignment(p AssignmentPlan) error {
	return s.ApplyAssignmentWithEffects(p, nil)
}
func (s *WorkloadService) ApplyAssignmentWithEffects(p AssignmentPlan, effect func(tasks.ResourceRef, string) error) error {
	s.Namespaces.mutationMu.Lock()
	defer s.Namespaces.mutationMu.Unlock()
	current, err := s.validateAssignment(p)
	if err != nil {
		return err
	}
	repo, _ := s.Clouds.VMs.VirtualMachineRepository.(MachineNamespaceRepository)
	for _, v := range current.Volumes {
		if err = s.Assignments.Put(v, s.Scopes[v.NodeId], VolumeAssignment{NamespaceID: p.Context.Namespace, Label: volumeLabel(v), OwnerVM: v.OwnerVM}); err != nil {
			return err
		}
		if effect != nil {
			if err = effect(VolumeRef(v.NodeId, v.Path), "namespace-assigned"); err != nil {
				return err
			}
		}
	}
	for _, vm := range current.Machines {
		if err = repo.AssignNamespace(vm.Id, vm.NodeId, p.Context.Namespace, machineLabel(vm)); err != nil {
			return err
		}
		if effect != nil {
			if err = effect(VMRef(vm.NodeId, vm.Id), "namespace-assigned"); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *WorkloadService) CheckMachineLabel(c WorkloadContext, label, except string) error {
	if !ValidResourceLabel(label) {
		return invalid("name", "invalid display label")
	}
	list, err := s.Clouds.ListMachines(context.Background(), c.Cloud)
	if err != nil {
		return err
	}
	if len(list.UnavailableHosts) > 0 {
		return ErrUnavailable
	}
	for _, vm := range list.Machines {
		if vm.Id != except && vm.NamespaceID == c.Namespace && machineLabel(vm) == label {
			return fmt.Errorf("%w: machine label already exists in namespace and cloud", ErrConflict)
		}
	}
	return nil
}
func (s *WorkloadService) CheckVolumeLabel(c WorkloadContext, node, pool, label, except string) error {
	if !ValidResourceLabel(label) {
		return invalid("name", "invalid display label")
	}
	inventory, err := s.Clouds.Volumes.ListInventory(VolumeListOptions{NodeIds: []string{node}, PoolNames: []string{pool}})
	if err != nil {
		return err
	}
	if len(inventory.UnavailableHosts) != 0 {
		return ErrUnavailable
	}
	for _, v := range inventory.Volumes {
		if err = s.ReadVolume(v); err != nil {
			return err
		}
		if v.Path != except && v.NamespaceID == c.Namespace && volumeLabel(v) == label {
			return fmt.Errorf("%w: volume label already exists in namespace and pool", ErrConflict)
		}
	}
	return nil
}

func (s *WorkloadService) validateAssignment(p AssignmentPlan) (AssignmentPlan, error) {
	for _, vm := range p.Machines {
		if scope, ok := p.BackendScopes[vm.NodeId]; !ok || scope != s.Scopes[vm.NodeId] {
			return AssignmentPlan{}, fmt.Errorf("%w: assignment backend changed", ErrConflict)
		}
	}
	for _, v := range p.Volumes {
		if scope, ok := p.BackendScopes[v.NodeId]; !ok || scope != s.Scopes[v.NodeId] {
			return AssignmentPlan{}, fmt.Errorf("%w: assignment backend changed", ErrConflict)
		}
	}
	ids := []string{}
	disks := []LocalDiskRef{}
	for _, vm := range p.Machines {
		ids = append(ids, vm.Id)
	}
	for _, v := range p.Volumes {
		disks = append(disks, LocalDiskRef{Host: v.NodeId, Path: v.Path})
	}
	current, err := s.PreviewAssignment(p.Context, ids, disks)
	if err != nil {
		return AssignmentPlan{}, err
	}
	if len(current.Machines) != len(p.Machines) || len(current.Volumes) != len(p.Volumes) {
		return AssignmentPlan{}, ErrConflict
	}
	for i, v := range current.Volumes {
		old := p.Volumes[i]
		if old.NodeId != v.NodeId || old.Path != v.Path || old.Identity != v.Identity {
			return AssignmentPlan{}, fmt.Errorf("%w: assignment volume identity changed", ErrConflict)
		}
	}
	for i, vm := range current.Machines {
		old := p.Machines[i]
		if old.NodeId != vm.NodeId || old.Id != vm.Id || len(old.Volumes) != len(vm.Volumes) {
			return AssignmentPlan{}, ErrConflict
		}
		for j, d := range vm.Volumes {
			if old.Volumes[j].Path != d.Path {
				return AssignmentPlan{}, ErrConflict
			}
		}
	}
	_, ok := s.Clouds.VMs.VirtualMachineRepository.(MachineNamespaceRepository)
	if len(current.Machines) > 0 && !ok {
		return AssignmentPlan{}, ErrUnavailable
	}

	return current, nil
}
