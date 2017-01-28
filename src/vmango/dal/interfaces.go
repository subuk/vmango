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
	Remove(*models.VirtualMachine) error
	Reboot(*models.VirtualMachine) error
	ServerInfo(*models.Server) error
}

type Imagerep interface {
	List(*[]*models.Image) error
	Get(*models.Image) (bool, error)
}

type Planrep interface {
	List(*[]*models.Plan) error
	Get(*models.Plan) (bool, error)
}

type SSHKeyrep interface {
	List(*[]*models.SSHKey) error
	Get(*models.SSHKey) (bool, error)
}

type Authrep interface {
	Get(*models.User) (bool, error)
}
