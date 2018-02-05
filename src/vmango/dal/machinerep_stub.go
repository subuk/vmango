package dal

import (
	"vmango/domain"
)

type StubMachinerep struct {
	ListResponse struct {
		Machines *domain.VirtualMachineList
		Error    error
	}
	GetResponse struct {
		Machine *domain.VirtualMachine
		Exist   bool
		Error   error
	}
	CreateResponse struct {
		Machine *domain.VirtualMachine
		Error   error
	}
	StartResponse  error
	StopResponse   error
	RemoveResponse error
	RebootResponse error
}

func (repo *StubMachinerep) List(vms *domain.VirtualMachineList) error {
	if repo.ListResponse.Machines != nil {
		*vms = *repo.ListResponse.Machines
	}
	return repo.ListResponse.Error
}
func (repo *StubMachinerep) Get(vm *domain.VirtualMachine) (bool, error) {
	if vm.Id == "" {
		panic("no id specified")
	}
	if repo.GetResponse.Machine != nil {
		*vm = *repo.GetResponse.Machine
	}
	return repo.GetResponse.Exist, repo.GetResponse.Error
}
func (repo *StubMachinerep) Create(vm *domain.VirtualMachine, image *domain.Image, plan *domain.Plan) error {
	if repo.CreateResponse.Machine != nil {
		*vm = *repo.CreateResponse.Machine
	}
	vm.Id = "stub-machine-id"
	return repo.CreateResponse.Error
}
func (repo *StubMachinerep) Start(vm *domain.VirtualMachine) error {
	return repo.StartResponse
}
func (repo *StubMachinerep) Stop(vm *domain.VirtualMachine) error {
	return repo.StopResponse
}
func (repo *StubMachinerep) Remove(vm *domain.VirtualMachine) error {
	return repo.RemoveResponse
}
func (repo *StubMachinerep) Reboot(vm *domain.VirtualMachine) error {
	return repo.RebootResponse
}
