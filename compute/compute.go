package compute

import (
	"errors"
	"fmt"
	"subuk/vmango/util"
)

var ErrArchNotsupported = errors.New("requested arch not supported")

type Service struct {
	virt    VirtualMachineRepository
	vol     VolumeRepository
	volpool VolumePoolRepository
	host    HostInfoRepository
	key     KeyRepository
	net     NetworkRepository
}

func New(virt VirtualMachineRepository, vol VolumeRepository, volpool VolumePoolRepository, host HostInfoRepository, key KeyRepository, net NetworkRepository) *Service {
	return &Service{virt: virt, vol: vol, volpool: volpool, host: host, key: key, net: net}
}

func (service *Service) VirtualMachineList() ([]*VirtualMachine, error) {
	return service.virt.List()
}

func (service *Service) VirtualMachineDetail(id string) (*VirtualMachine, error) {
	return service.virt.Get(id)
}

type VirtualMachineCreateParamsConfig struct {
	Hostname        string
	UserData        string
	KeyFingerprints []string
}

type VirtualMachineCreateParamsVolume struct {
	CloneFrom  string
	Name       string
	Pool       string
	Format     string
	DeviceType string
	SizeMb     uint64
}

type VirtualMachineCreateParamsInterface struct {
	Network    string
	Mac        string
	Model      string
	AccessVlan uint
}

type VirtualMachineCreateParams struct {
	Id         string
	VCpus      int
	Arch       string
	MemoryKb   uint // KiB
	Volumes    []VirtualMachineCreateParamsVolume
	Interfaces []VirtualMachineCreateParamsInterface
	Config     VirtualMachineCreateParamsConfig
}

func (service *Service) VirtualMachineCreate(params VirtualMachineCreateParams) (*VirtualMachine, error) {
	volumes := []*VirtualMachineAttachedVolume{}
	for _, volumeParams := range params.Volumes {
		volume, _ := service.vol.GetByName(volumeParams.Pool, volumeParams.Name)
		if volume == nil {
			if volumeParams.CloneFrom != "" {
				clonedVolume, err := service.VolumeClone(volumeParams.CloneFrom, volumeParams.Name, volumeParams.Pool, volumeParams.Format, volumeParams.SizeMb)
				if err != nil {
					return nil, err
				}
				volume = clonedVolume
			} else {
				createdVolume, err := service.VolumeCreate(volumeParams.Name, volumeParams.Pool, volumeParams.Format, volumeParams.SizeMb)
				if err != nil {
					return nil, err
				}
				volume = createdVolume
			}
		}
		if volume.AttachedTo != "" {
			return nil, fmt.Errorf("volume %s already exists and attached to %s as %s", volume.Path, volume.AttachedTo, volume.AttachedAs)
		}
		volumes = append(volumes, &VirtualMachineAttachedVolume{
			Type:   volume.Type,
			Path:   volume.Path,
			Format: volume.Format,
			Device: NewDeviceType(volumeParams.DeviceType),
		})
	}

	interfaces := []*VirtualMachineAttachedInterface{}
	for _, ifaceParams := range params.Interfaces {
		network, err := service.net.Get(ifaceParams.Network)
		if err != nil {
			return nil, util.NewError(err, "network get failed")
		}
		iface := &VirtualMachineAttachedInterface{
			Type:       network.Type,
			Network:    ifaceParams.Network,
			Mac:        ifaceParams.Mac,
			AccessVlan: ifaceParams.AccessVlan,
		}
		interfaces = append(interfaces, iface)
	}
	config := &VirtualMachineConfig{
		Hostname: params.Config.Hostname,
		Userdata: []byte(params.Config.UserData),
	}
	for _, fingerprint := range params.Config.KeyFingerprints {
		key, err := service.key.Get(fingerprint)
		if err != nil {
			return nil, util.NewError(err, "cannot load key")
		}
		config.Keys = append(config.Keys, key)
	}

	vm, err := service.virt.Create(params.Id, NewArch(params.Arch), params.VCpus, params.MemoryKb, volumes, interfaces, config)
	if err != nil {
		return nil, util.NewError(err, "cannot create virtual machine")
	}
	return vm, nil
}

func (service *Service) VirtualMachineDelete(id string) error {
	return service.virt.Delete(id)
}

func (service *Service) VirtualMachineAttachVolume(id, path string, deviceType DeviceType) (*VirtualMachineAttachedVolume, error) {
	vol, err := service.vol.Get(path)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup volume")
	}
	return service.virt.AttachVolume(id, path, vol.Type, vol.Format, deviceType)
}

func (service *Service) VirtualMachineDetachVolume(id, path string) error {
	return service.virt.DetachVolume(id, path)
}

func (service *Service) VirtualMachineAttachInterface(id, network, mac, model string, accessVlan uint) (*VirtualMachineAttachedInterface, error) {
	net, err := service.net.Get(network)
	if err != nil {
		return nil, util.NewError(err, "cannot get specified network")
	}
	return service.virt.AttachInterface(id, network, mac, model, accessVlan, net.Type)
}

func (service *Service) VirtualMachineDetachInterface(id, mac string) error {
	return service.virt.DetachInterface(id, mac)
}

func (service *Service) VolumeList() ([]*Volume, error) {
	return service.vol.List()
}

func (service *Service) VolumeGet(path string) (*Volume, error) {
	return service.vol.Get(path)
}

func (service *Service) VolumeClone(originalPath, volumeName, poolName, volumeFormatName string, newSizeMb uint64) (*Volume, error) {
	return service.vol.Clone(originalPath, volumeName, poolName, NewVolumeFormat(volumeFormatName), newSizeMb)
}

func (service *Service) VolumeResize(path string, size uint64) error {
	return service.vol.Resize(path, size)
}

func (service *Service) VolumePoolList() ([]*VolumePool, error) {
	return service.volpool.List()
}

func (service *Service) VolumeCreate(poolName, volumeName, volumeFormatName string, size uint64) (*Volume, error) {
	return service.vol.Create(poolName, volumeName, NewVolumeFormat(volumeFormatName), size)
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

func (service *Service) NetworkList() ([]*Network, error) {
	return service.net.List()
}

func (service *Service) NetworkGet(id string) (*Network, error) {
	return service.net.Get(id)
}
