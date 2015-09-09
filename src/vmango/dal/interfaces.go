package dal

import (
	"vmango/models"
)

type Machinerep interface {
	List(*[]*models.VirtualMachine) error
	Get(*models.VirtualMachine) (bool, error)
}

type Imagerep interface {
	List(*[]*models.Image) error
	Get(*models.Image) (bool, error)
}

type IPPool interface {
	List(*[]*models.IP) error
	Get(*models.IP) (bool, error)
}
