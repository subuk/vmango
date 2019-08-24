package compute

import (
	"fmt"
)

type Service struct {
	virt VirtualMachineRepository
	vol  VolumeRepository
	host HostInfoRepository
}

func New(virt VirtualMachineRepository, vol VolumeRepository, host HostInfoRepository) *Service {
	return &Service{virt: virt, vol: vol, host: host}
}

func (service *Service) VirtualMachineList() ([]*VirtualMachine, error) {
	return service.virt.List()
}

func (service *Service) VirtualMachineDetail(id string) (*VirtualMachine, error) {
	return service.virt.Get(id)
}

func (service *Service) VolumeList() ([]*Volume, error) {
	return service.vol.List()
}

func (service *Service) HostInfo() (*HostInfo, error) {
	return service.host.Get()
}

func (service *Service) VirtualMachineAction(id string, action string) error {
	switch action {
	default:
		return fmt.Errorf("unknown action %s", action)
	case "reboot":
		return service.virt.Reboot(id)
	case "poweroff":
		return service.virt.Poweroff(id)
	case "start":
		return service.virt.Start(id)
	}
}
