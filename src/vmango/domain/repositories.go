package domain

type Machinerep interface {
	List(*VirtualMachineList) error
	Get(*VirtualMachine) (bool, error)
	Create(*VirtualMachine, *Image, *Plan) error
	Start(*VirtualMachine) error
	Stop(*VirtualMachine) error
	Remove(*VirtualMachine) error
	Reboot(*VirtualMachine) error
}

type Imagerep interface {
	List(*ImageList) error
	Get(*Image) (bool, error)
}

type Planrep interface {
	List(*[]*Plan) error
	Get(*Plan) (bool, error)
}

type SSHKeyrep interface {
	List(*[]*SSHKey) error
	Get(*SSHKey) (bool, error)
}

type Authrep interface {
	Get(*User) (bool, error)
}

type Statusrep interface {
	Fetch(status *StatusInfo) error
}

type ProviderConfigrep interface {
	Get(name string) (*ProviderConfig, error)
	ListIds() ([]string, error)
}
