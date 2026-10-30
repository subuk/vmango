package libvirt

import (
	"encoding/base64"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"subuk/vmango/compute"
)

func logicalNetworkAlias(network string) string {
	if network == "" {
		return ""
	}
	return "ua-vmango-net-" + base64.RawURLEncoding.EncodeToString([]byte(network)) + "." + uuid.NewString()
}
func networkFromAlias(alias string) string {
	const prefix = "ua-vmango-net-"
	if !strings.HasPrefix(alias, prefix) {
		return ""
	}
	encoded := strings.SplitN(strings.TrimPrefix(alias, prefix), ".", 2)[0]
	value, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	return string(value)
}

func (repo *VirtualMachineRepository) ValidateNetwork(node string, nic *compute.VirtualMachineAttachedInterface) error {
	if nic == nil {
		return compute.ErrInvalid
	}
	if nic.Bridge {
		// Direct bridges are administrator-provisioned host devices. Libvirt's
		// network API does not inventory arbitrary bridges on remote hosts.
		if nic.NetworkName == "" || strings.ContainsAny(nic.NetworkName, "/\t\r\n ") {
			return fmt.Errorf("%w: invalid bridge binding", compute.ErrInvalid)
		}
		return nil
	}
	if nic.NetworkName == "qemu-usernet" || nic.NetworkName == "macos-socket-vmnet" {
		if nic.AccessVlan != 0 {
			return fmt.Errorf("%w: this network does not support VLAN tags", compute.ErrInvalid)
		}
		return nil
	}
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return err
	}
	defer repo.pool.Release(node)
	network, err := conn.LookupNetworkByName(nic.NetworkName)
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
