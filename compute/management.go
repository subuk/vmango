package compute

import "fmt"

func (s *VirtualMachineService) RequireManaged(id, node string) error {
	if !s.CloudMode {
		return nil
	}
	vm, err := s.Get(id, node)
	if err != nil {
		return err
	}
	if vm.Metadata == nil {
		return fmt.Errorf("%w: machine is read-only until explicitly adopted", ErrConflict)
	}
	return nil
}
func (s *VirtualMachineService) Delete(id, node string) error {
	if err := s.RequireManaged(id, node); err != nil {
		return err
	}
	return s.VirtualMachineRepository.Delete(id, node)
}
func (s *VirtualMachineService) DetachVolume(id, node, path string) error {
	if err := s.RequireManaged(id, node); err != nil {
		return err
	}
	return s.VirtualMachineRepository.DetachVolume(id, node, path)
}
func (s *VirtualMachineService) DetachInterface(id, node, mac string) error {
	if err := s.RequireManaged(id, node); err != nil {
		return err
	}
	return s.VirtualMachineRepository.DetachInterface(id, node, mac)
}

func (s *VirtualMachineService) Start(id, node string) error {
	if err := s.RequireManaged(id, node); err != nil {
		return err
	}
	return s.VirtualMachineRepository.Start(id, node)
}
func (s *VirtualMachineService) Reboot(id, node string) error {
	if err := s.RequireManaged(id, node); err != nil {
		return err
	}
	return s.VirtualMachineRepository.Reboot(id, node)
}
func (s *VirtualMachineService) Poweroff(id, node string) error {
	if err := s.RequireManaged(id, node); err != nil {
		return err
	}
	return s.VirtualMachineRepository.Poweroff(id, node)
}

// ValidateNetwork delegates checks requiring actual backend network state.
func (s *VirtualMachineService) ValidateNetwork(node string, nic *VirtualMachineAttachedInterface) error {
	if backend, ok := s.VirtualMachineRepository.(NetworkValidator); ok {
		return backend.ValidateNetwork(node, nic)
	}
	return nil
}

func (s *VirtualMachineService) GetConsoleStream(id, node string, force bool) (VirtualMachineConsoleStream, error) {
	if err := s.RequireManaged(id, node); err != nil {
		return nil, err
	}
	return s.VirtualMachineRepository.GetConsoleStream(id, node, force)
}
func (s *VirtualMachineService) GetGraphicStream(id, node string) (VirtualMachineGraphicStream, error) {
	if err := s.RequireManaged(id, node); err != nil {
		return nil, err
	}
	return s.VirtualMachineRepository.GetGraphicStream(id, node)
}

func (s *VirtualMachineService) Shutdown(id, node string) error {
	if err := s.RequireManaged(id, node); err != nil {
		return err
	}
	return s.VirtualMachineRepository.Shutdown(id, node)
}
