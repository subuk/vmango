package dal

import (
	"vmango/domain"
)

type LibvirtNetwork interface {
	Name() string
	AssignIP(vm *domain.VirtualMachine) error
	ReleaseIP(vm *domain.VirtualMachine) error
	LookupIP(vm *domain.VirtualMachine) error
}
