package compute

import (
	"encoding/json"
	"fmt"
	"subuk/vmango/tasks"
)

// BackendMachineRepository owns the meaning and versioning of private machine
// plans. Common lifecycle code coordinates their storage without interpreting
// provider-specific hardware or device policy.
type BackendMachineRepository interface {
	ResolveHardware(vm *VirtualMachine, policy, binding json.RawMessage) error
	ApplyHardware(vm *VirtualMachine, applied json.RawMessage) error
	ValidateAppliedPolicy(vm *VirtualMachine) error
	ResolveDiskAttachment(vm *VirtualMachine, disk *VirtualMachineAttachedVolume) error
	ResolveInterfaceAttachment(vm *VirtualMachine, nic *VirtualMachineAttachedInterface) error
	ValidateResourceChange(current, requested *VirtualMachine) error
	ValidateAdoptionPolicy(vm *VirtualMachine, policy json.RawMessage) error
	PrepareAdoption(vm *VirtualMachine, cloud, policyRef string) (json.RawMessage, error)
	AdoptWithPolicy(id, node string, metadata *MachineMetadata, policy json.RawMessage) error
}

type MachineAutostartRepository interface {
	SetAutostart(id, node string, enabled bool) error
}

func (s *VirtualMachineService) ResolveDiskAttachment(vm *VirtualMachine, disk *VirtualMachineAttachedVolume) error {
	if backend, ok := s.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.ResolveDiskAttachment(vm, disk)
	}
	return nil
}

func (s *VirtualMachineService) SetAutostart(id, node string, enabled bool) error {
	if err := s.RequireManaged(id, node); err != nil {
		return err
	}
	if backend, ok := s.VirtualMachineRepository.(MachineAutostartRepository); ok {
		return backend.SetAutostart(id, node, enabled)
	}
	return nil
}

func (h *CommandHandler) CheckCommandCompatibility(command tasks.Command) error {
	var envelope struct{ Version int }
	if json.Unmarshal(command.Payload, &envelope) != nil || envelope.Version != InfrastructureCommandVersion {
		return fmt.Errorf("task was accepted before an incompatible upgrade; migrate configuration and resubmit (see https://github.com/subuk/vmango/blob/master/docs/HARDWARE-PROFILES-MIGRATION.md)")
	}
	return nil
}

// AdoptionRequiredError is safe to expose as an actionable mutation rejection.
type AdoptionRequiredError struct{ Message string }

func (e *AdoptionRequiredError) Error() string { return e.Message }
func (e *AdoptionRequiredError) Unwrap() error { return ErrConflict }

func (s *VirtualMachineService) ResolveInterfaceAttachment(vm *VirtualMachine, nic *VirtualMachineAttachedInterface) error {
	if backend, ok := s.VirtualMachineRepository.(BackendMachineRepository); ok {
		return backend.ResolveInterfaceAttachment(vm, nic)
	}
	return nil
}

// BackendArtifactRepository exposes confirmed provider-owned state for durable
// task effects without teaching the domain about firmware or storage mechanisms.
type BackendArtifactRepository interface {
	BackendArtifactRefs(vm *VirtualMachine) []tasks.ResourceRef
}

func (s *VirtualMachineService) BackendArtifactRefs(vm *VirtualMachine) []tasks.ResourceRef {
	if backend, ok := s.VirtualMachineRepository.(BackendArtifactRepository); ok {
		return backend.BackendArtifactRefs(vm)
	}
	return nil
}
