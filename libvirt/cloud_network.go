package libvirt

import (
	"fmt"
	"subuk/vmango/compute"
)

func (repo *VirtualMachineRepository) ValidateNetwork(node string, nic *compute.VirtualMachineAttachedInterface) error {
	if nic == nil {
		return compute.ErrInvalid
	}
	if nic.Binding == nil {
		return &compute.ValidationError{Field: "network", Message: "A resolved host network binding is required."}
	}
	if err := nic.Binding.Validate(); err != nil {
		return err
	}
	if nic.AccessVlan != 0 {
		return &compute.ValidationError{Field: "network", Message: "Configure VLANs in the host network infrastructure."}
	}
	switch nic.Binding.Kind() {
	case compute.NetworkBridge, compute.NetworkUser:
		return nil
	case compute.NetworkSocketVMNet:
		if nic.Model != "" && nic.Model != "virtio" {
			return &compute.ValidationError{Field: "network", Message: "socket-vmnet requires a hardware profile with nic_model = virtio."}
		}
		return repo.validateSocketVMNet(node, nic.Binding.Socket)
	}
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return err
	}
	defer repo.pool.Release(node)
	network, err := conn.LookupNetworkByName(nic.Binding.Network)
	if err != nil {
		return repositoryError(err, "configured network is unavailable")
	}
	defer network.Free()
	active, err := network.IsActive()
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("%w: configured network %s is inactive", compute.ErrConflict, nic.NetworkName)
	}
	return nil
}
