package dal

import (
	"fmt"
	"vmango/models"
)

type StubMachinerep struct {
	ListResponse struct {
		Machines *models.VirtualMachineList
		Error    error
	}
	GetResponse struct {
		Machine *models.VirtualMachine
		Exist   bool
		Error   error
	}
	CreateResponse struct {
		Machine *models.VirtualMachine
		Error   error
	}
	ServerInfoResponse struct {
		Servers *models.ServerList
		Error   error
	}
	StartResponse  error
	StopResponse   error
	RemoveResponse error
	RebootResponse error
}

func (repo *StubMachinerep) List(vms *models.VirtualMachineList) error {
	if repo.ListResponse.Machines != nil {
		*vms = *repo.ListResponse.Machines
	}
	return repo.ListResponse.Error
}
func (repo *StubMachinerep) Get(vm *models.VirtualMachine) (bool, error) {
	if repo.GetResponse.Machine != nil {
		*vm = *repo.GetResponse.Machine
	}
	return repo.GetResponse.Exist, repo.GetResponse.Error
}
func (repo *StubMachinerep) Create(vm *models.VirtualMachine, image *models.Image, plan *models.Plan) error {
	if vm.Hypervisor == "" {
		return fmt.Errorf("hypervisor not set")
	}
	if repo.CreateResponse.Machine != nil {
		*vm = *repo.CreateResponse.Machine
	}
	return repo.CreateResponse.Error
}
func (repo *StubMachinerep) Start(vm *models.VirtualMachine) error {
	if vm.Hypervisor == "" {
		return fmt.Errorf("hypervisor not set")
	}
	return repo.StartResponse
}
func (repo *StubMachinerep) Stop(vm *models.VirtualMachine) error {
	if vm.Hypervisor == "" {
		return fmt.Errorf("hypervisor not set")
	}
	return repo.StopResponse
}
func (repo *StubMachinerep) Remove(vm *models.VirtualMachine) error {
	if vm.Hypervisor == "" {
		return fmt.Errorf("hypervisor not set")
	}
	return repo.RemoveResponse
}
func (repo *StubMachinerep) Reboot(vm *models.VirtualMachine) error {
	if vm.Hypervisor == "" {
		return fmt.Errorf("hypervisor not set")
	}
	return repo.RebootResponse
}
func (repo *StubMachinerep) ServerInfo(servers *models.ServerList) error {
	*servers = *repo.ServerInfoResponse.Servers
	return repo.ServerInfoResponse.Error
}
