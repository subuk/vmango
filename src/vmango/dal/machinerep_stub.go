package dal

import (
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
		Server *models.Server
		Error  error
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
	if repo.CreateResponse.Machine != nil {
		*vm = *repo.CreateResponse.Machine
	}
	return repo.CreateResponse.Error
}
func (repo *StubMachinerep) Start(*models.VirtualMachine) error {
	return repo.StartResponse
}
func (repo *StubMachinerep) Stop(*models.VirtualMachine) error {
	return repo.StopResponse
}
func (repo *StubMachinerep) Remove(*models.VirtualMachine) error {
	return repo.RemoveResponse
}
func (repo *StubMachinerep) Reboot(*models.VirtualMachine) error {
	return repo.RebootResponse
}
func (repo *StubMachinerep) ServerInfo(server *models.Server) error {
	*server = *repo.ServerInfoResponse.Server
	return repo.ServerInfoResponse.Error
}
