package models

import (
	"fmt"
	"gopkg.in/alexzorin/libvirt-go.v2"
)

type LibvirtStorage struct {
	conn libvirt.VirConnection
}

func NewLibvirtStorage(uri string) (*LibvirtStorage, error) {
	conn, err := libvirt.NewVirConnection(uri)
	if err != nil {
		return nil, err
	}
	return &LibvirtStorage{conn: conn}, nil
}

func fillVm(vm *VirtualMachine, domain libvirt.VirDomain) error {
	name, err := domain.GetName()
	if err != nil {
		return err
	}
	uuid, err := domain.GetUUID()
	if err != nil {
		return err
	}
	info, err := domain.GetInfo()
	if err != nil {
		return err
	}

	switch info.GetState() {
	default:
		vm.State = STATE_UNKNOWN
	case 1:
		vm.State = STATE_RUNNING
	case 5:
		vm.State = STATE_STOPPED
	}

	vm.Name = name
	vm.Uuid = fmt.Sprintf("%x", uuid)
	vm.Memory = int(info.GetMaxMem())
	vm.Cpus = int(info.GetNrVirtCpu())
	return nil
}

func (store *LibvirtStorage) ListMachines(machines *[]*VirtualMachine) error {
	domains, err := store.conn.ListAllDomains(0)
	if err != nil {
		return err
	}
	for _, domain := range domains {
		vm := &VirtualMachine{}
		if err := fillVm(vm, domain); err != nil {
			return err
		}
		*machines = append(*machines, vm)
	}
	return nil
}

func (store *LibvirtStorage) GetMachine(machine *VirtualMachine) (bool, error) {
	if machine.Name == "" {
		return false, nil
	}

	domain, err := store.conn.LookupDomainByName(machine.Name)
	if err != nil {
		virErr := err.(libvirt.VirError)
		if virErr.Code == libvirt.VIR_ERR_NO_DOMAIN {
			return false, nil
		}
		return false, virErr
	}
	fillVm(machine, domain)
	return true, nil
}
