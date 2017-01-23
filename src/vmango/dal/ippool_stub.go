package dal

import (
	"vmango/models"
)

type StubIPPool struct {
	FetchResponse struct {
		IP    *models.IP
		Error error
	}
	ReleaseResponse error
}

func (pool *StubIPPool) List(*models.IPList) error {
	return nil
}
func (pool *StubIPPool) Get(*models.IP) (bool, error) {
	return false, nil
}
func (pool *StubIPPool) Assign(*models.IP, *models.VirtualMachine) error {
	return nil
}
func (pool *StubIPPool) Fetch(vm *models.VirtualMachine) error {
	if pool.FetchResponse.IP != nil {
		*vm.Ip = *pool.FetchResponse.IP
	}
	return pool.FetchResponse.Error
}
func (pool *StubIPPool) Release(*models.IP) error {
	return nil
}
