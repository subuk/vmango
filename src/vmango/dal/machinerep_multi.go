package dal

import (
	"fmt"
	"vmango/models"
)

type MultiMachinerep struct {
	repos map[string]Machinerep
}

func NewMultiMachinerep(repos map[string]Machinerep) *MultiMachinerep {
	return &MultiMachinerep{
		repos: repos,
	}
}

func (multirep *MultiMachinerep) List(vms *models.VirtualMachineList) error {
	for repoName, repo := range multirep.repos {
		if err := repo.List(vms); err != nil {
			return fmt.Errorf("failed to query repo %s: %s", repoName, err)
		}
	}
	return nil
}

func (multirep *MultiMachinerep) Get(vm *models.VirtualMachine) (bool, error) {
	repo, exist := multirep.repos[vm.Hypervisor]
	if !exist {
		return false, fmt.Errorf("repo for hypervisor '%s' doesn't exist", vm.Hypervisor)
	}
	return repo.Get(vm)
}

func (multirep *MultiMachinerep) Create(vm *models.VirtualMachine, image *models.Image, plan *models.Plan) error {
	repo, exist := multirep.repos[vm.Hypervisor]
	if !exist {
		return fmt.Errorf("repo for hypervisor '%s' doesn't exist", vm.Hypervisor)
	}
	return repo.Create(vm, image, plan)
}

func (multirep *MultiMachinerep) Start(vm *models.VirtualMachine) error {
	repo, exist := multirep.repos[vm.Hypervisor]
	if !exist {
		return fmt.Errorf("repo for hypervisor '%s' doesn't exist", vm.Hypervisor)
	}
	return repo.Start(vm)
}

func (multirep *MultiMachinerep) Stop(vm *models.VirtualMachine) error {
	repo, exist := multirep.repos[vm.Hypervisor]
	if !exist {
		return fmt.Errorf("repo for hypervisor '%s' doesn't exist", vm.Hypervisor)
	}
	return repo.Stop(vm)
}

func (multirep *MultiMachinerep) Remove(vm *models.VirtualMachine) error {
	repo, exist := multirep.repos[vm.Hypervisor]
	if !exist {
		return fmt.Errorf("repo for hypervisor '%s' doesn't exist", vm.Hypervisor)
	}
	return repo.Remove(vm)
}

func (multirep *MultiMachinerep) Reboot(vm *models.VirtualMachine) error {
	repo, exist := multirep.repos[vm.Hypervisor]
	if !exist {
		return fmt.Errorf("repo for hypervisor '%s' doesn't exist", vm.Hypervisor)
	}
	return repo.Reboot(vm)
}
func (multirep *MultiMachinerep) ServerInfo(vm *models.Server) error {
	return fmt.Errorf("not implemented")
}
