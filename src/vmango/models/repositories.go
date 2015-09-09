package models

type Machinerep interface {
	List(*[]*VirtualMachine) error
	Get(*VirtualMachine) (bool, error)
}

type Imagerep interface {
	List(*[]*Image) error
	Get(*Image) (bool, error)
}

type IPPool interface {
	List(*[]*IP) error
	Get(*IP) (bool, error)
}
