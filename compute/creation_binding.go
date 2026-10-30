package compute

import "encoding/json"

// CreationBindingRepository snapshots private host settings without exposing
// backend details through portable images, instance types or public capabilities.
// Applying a binding must not perform hypervisor mutations.
type CreationBindingRepository interface {
	SnapshotCreationBinding(node string) (json.RawMessage, error)
	ApplyCreationBinding(vm *VirtualMachine, binding json.RawMessage) error
}

func (s *VirtualMachineService) SnapshotCreationBinding(node string) (json.RawMessage, error) {
	if repo, ok := s.VirtualMachineRepository.(CreationBindingRepository); ok {
		return repo.SnapshotCreationBinding(node)
	}
	return nil, nil
}
func (s *VirtualMachineService) applyCreationBinding(vm *VirtualMachine, binding json.RawMessage) error {
	if repo, ok := s.VirtualMachineRepository.(CreationBindingRepository); ok {
		return repo.ApplyCreationBinding(vm, binding)
	}
	return nil
}
