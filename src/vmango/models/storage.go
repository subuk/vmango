package models

type Storage interface {
	ListMachines(machines *[]*VirtualMachine) error
	GetMachine(machine *VirtualMachine) (bool, error)
}

type Imagerep interface {
	List(*[]*Image) error
	Get(*Image) (bool, error)
}
