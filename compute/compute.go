package compute

import (
	"fmt"
)

type Service struct {
	virt VirtualMachineRepository
	vol  VolumeRepository
	host HostInfoRepository
	key  KeyRepository
}

func New(virt VirtualMachineRepository, vol VolumeRepository, host HostInfoRepository, key KeyRepository) *Service {
	return &Service{virt: virt, vol: vol, host: host, key: key}
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

func (service *Service) VolumeGet(path string) (*Volume, error) {
	return service.vol.Get(path)
}

func (service *Service) VolumeClone(originalPath, volumeName, poolName string, volumeFormat VolumeFormat, newSizeMb uint64) (*Volume, error) {
	return service.vol.Clone(originalPath, volumeName, poolName, volumeFormat, newSizeMb)
}

func (service *Service) VolumeResize(path string, size uint64) error {
	return service.vol.Resize(path, size)
}

func (service *Service) VolumePoolList() ([]*VolumePool, error) {
	return service.vol.Pools()
}

func (service *Service) VolumeCreate(poolName, volumeName string, volumeFormat VolumeFormat, size uint64) (*Volume, error) {
	return service.vol.Create(poolName, volumeName, volumeFormat, size)
}

func (service *Service) VolumeDelete(path string) error {
	return service.vol.Delete(path)
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

func (service *Service) KeyList() ([]*Key, error) {
	return service.key.List()
}

func (service *Service) KeyDetail(fingerprint string) (*Key, error) {
	return service.key.Get(fingerprint)
}

func (service *Service) KeyDelete(fingerprint string) error {
	return service.key.Delete(fingerprint)
}

func (service *Service) KeyAdd(input string) error {
	return service.key.Add([]byte(input))
}
