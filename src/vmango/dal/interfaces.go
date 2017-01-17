package dal

import (
	"vmango/models"
)

type Machinerep interface {
	List(*models.VirtualMachineList) error
	Get(*models.VirtualMachine) (bool, error)
	Create(*models.VirtualMachine, *models.Image, *models.Plan) error
	Start(*models.VirtualMachine) error
	Stop(*models.VirtualMachine) error
}

type Imagerep interface {
	List(*[]*models.Image) error
	Get(*models.Image) (bool, error)
}

type Planrep interface {
	List(*[]*models.Plan) error
	Get(*models.Plan) (bool, error)
}

type IPPool interface {
	List(*models.IPList) error
	Get(*models.IP) (bool, error)
	Assign(*models.IP, *models.VirtualMachine) error
}
