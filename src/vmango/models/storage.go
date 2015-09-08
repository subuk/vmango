package models

type Storage interface {
	ListMachines(machines *[]*VirtualMachine) error
	GetMachine(machine *VirtualMachine) (bool, error)
}
