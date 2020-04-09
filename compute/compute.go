package compute

import (
	"errors"
	"fmt"
	"subuk/vmango/util"
)

var ErrArchNotsupported = errors.New("requested arch not supported")

type Event interface {
	Name() string
	Plain() map[string]string
}

type EventPublisher interface {
	Publish(event Event) error
}

type Service struct {
	virt    VirtualMachineRepository
	vol     VolumeRepository
	volpool VolumePoolRepository
	host    HostInfoRepository
	key     KeyRepository
	net     NetworkRepository
	epub    EventPublisher
}

func New(epub EventPublisher, virt VirtualMachineRepository, vol VolumeRepository, volpool VolumePoolRepository, host HostInfoRepository, key KeyRepository, net NetworkRepository) *Service {
	return &Service{epub: epub, virt: virt, vol: vol, volpool: volpool, host: host, key: key, net: net}
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
	CloneFrom string
	Name      string
	Pool      string
	Format    VolumeFormat
	Size      Size
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
	Memory     Size
	Volumes    []VirtualMachineCreateParamsVolume
	Interfaces []VirtualMachineCreateParamsInterface
	Config     VirtualMachineCreateParamsConfig
	Start      bool
}

func (service *Service) VirtualMachineCreate(params VirtualMachineCreateParams) (*VirtualMachine, error) {
	volumes := []*VirtualMachineAttachedVolume{}
	for _, volumeParams := range params.Volumes {
		volume, _ := service.vol.GetByName(volumeParams.Pool, volumeParams.Name)
		if volume == nil {
			if volumeParams.CloneFrom != "" {
				volumeCloneParams := VolumeCloneParams{
					// volumeParams.CloneFrom, volumeParams.Name, volumeParams.Pool, volumeParams.Format, volumeParams.Size
					Format:       volumeParams.Format,
					OriginalPath: volumeParams.CloneFrom,
					NewName:      volumeParams.Name,
					NewPool:      volumeParams.Pool,
					NewSize:      volumeParams.Size,
				}
				clonedVolume, err := service.VolumeClone(volumeCloneParams)
				if err != nil {
					return nil, err
				}
				volume = clonedVolume
			} else {
				volumeCreateParams := VolumeCreateParams{
					Name:   volumeParams.Name,
					Pool:   volumeParams.Pool,
					Format: volumeParams.Format,
					Size:   volumeParams.Size,
				}
				createdVolume, err := service.VolumeCreate(volumeCreateParams)
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
			DeviceName: "vda",
			Type:       volume.Type,
			Path:       volume.Path,
			Format:     volume.Format,
			DeviceType: DeviceTypeDisk,
			DeviceBus:  DeviceBusVirtio,
		})
	}

	interfaces := []*VirtualMachineAttachedInterface{}
	for _, ifaceParams := range params.Interfaces {
		network, err := service.net.Get(ifaceParams.Network)
		if err != nil {
			return nil, util.NewError(err, "network get failed")
		}
		iface := &VirtualMachineAttachedInterface{
			NetworkType: network.Type,
			NetworkName: ifaceParams.Network,
			Mac:         ifaceParams.Mac,
			AccessVlan:  ifaceParams.AccessVlan,
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

	vm, err := service.virt.Create(params.Id, NewArch(params.Arch), params.VCpus, params.Memory, volumes, interfaces, config)
	if err != nil {
		return nil, util.NewError(err, "cannot create virtual machine")
	}
	if err := service.epub.Publish(NewEventVirtualMachineCreated(vm)); err != nil {
		service.virt.Delete(vm.Id) // Ignore error
		return nil, util.NewError(err, "cannot publish event virtual machine created")
	}
	if params.Start {
		if err := service.virt.Start(vm.Id); err != nil {
			return nil, util.NewError(err, "cannot start vm")
		}
	}
	return vm, nil
}

func (service *Service) VirtualMachineDelete(id string, deleteVolumes bool) error {
	volumesToDelete := []*VirtualMachineAttachedVolume{}
	if deleteVolumes {
		vm, err := service.virt.Get(id)
		if err != nil {
			return util.NewError(err, "cannot fetch vm info")
		}
		for _, volume := range vm.Volumes {
			volumesToDelete = append(volumesToDelete, volume)
		}
	}
	if err := service.virt.Delete(id); err != nil {
		return util.NewError(err, "cannot delete vm")
	}
	for _, volume := range volumesToDelete {
		if err := service.vol.Delete(volume.Path); err != nil {
			return util.NewError(err, "cannot delete volume")
		}
	}
	return nil
}

type VolumeAttachmentParams struct {
	MachineId  string
	DeviceName string
	VolumePath string
	DeviceType DeviceType
	DeviceBus  DeviceBus
}

func (service *Service) VirtualMachineAttachVolume(params VolumeAttachmentParams) error {
	vol, err := service.vol.Get(params.VolumePath)
	if err != nil {
		return util.NewError(err, "cannot lookup volume")
	}
	attachedVolume := &VirtualMachineAttachedVolume{
		Path:       params.VolumePath,
		Type:       vol.Type,
		Format:     vol.Format,
		DeviceName: params.DeviceName,
		DeviceType: params.DeviceType,
		DeviceBus:  params.DeviceBus,
	}
	return service.virt.AttachVolume(params.MachineId, attachedVolume)
}

func (service *Service) VirtualMachineDetachVolume(id, path string) error {
	return service.virt.DetachVolume(id, path)
}

func (service *Service) VirtualMachineAttachInterface(id string, iface *VirtualMachineAttachedInterface) error {
	return service.virt.AttachInterface(id, iface)
}

type VirtualMachineUpdateParams struct {
	Vcpus         *int
	Memory        *Size
	Autostart     *bool
	GuestAgent    *bool
	GraphicType   *GraphicType
	GraphicListen *string
}

func (service *Service) VirtualMachineUpdate(id string, params VirtualMachineUpdateParams) error {
	return service.virt.Update(id, params)
}

func (service *Service) VirtualMachineDetachInterface(id, mac string) error {
	return service.virt.DetachInterface(id, mac)
}

func (service *Service) VirtualMachineGetConsoleStream(id string) (VirtualMachineConsoleStream, error) {
	return service.virt.GetConsoleStream(id)
}

func (service *Service) VolumeList() ([]*Volume, error) {
	return service.vol.List()
}

func (service *Service) ImageList() ([]*Volume, error) {
	volumes, err := service.vol.List()
	if err != nil {
		return nil, util.NewError(err, "cannot list volume")
	}
	annotatedVolumes := []*Volume{}
	detachedVolumes := []*Volume{}
	for _, volume := range volumes {
		if volume.Format == FormatIso {
			continue
		}
		if volume.AttachedTo != "" {
			continue
		}
		if volume.Metadata.OsName != "" {
			annotatedVolumes = append(annotatedVolumes, volume)
			continue
		}
		detachedVolumes = append(detachedVolumes, volume)
	}
	if len(annotatedVolumes) > 0 {
		return annotatedVolumes, nil
	}
	return detachedVolumes, nil
}

func (service *Service) VolumeGet(path string) (*Volume, error) {
	return service.vol.Get(path)
}

type VolumeCloneParams struct {
	Format       VolumeFormat
	OriginalPath string
	NewName      string
	NewPool      string
	NewSize      Size
}

func (service *Service) VolumeClone(params VolumeCloneParams) (*Volume, error) {
	return service.vol.Clone(params)
}

func (service *Service) VolumeResize(path string, size Size) error {
	return service.vol.Resize(path, size)
}

func (service *Service) VolumePoolList() ([]*VolumePool, error) {
	return service.volpool.List()
}

type VolumeCreateParams struct {
	Name   string
	Pool   string
	Format VolumeFormat
	Size   Size
}

func (service *Service) VolumeCreate(params VolumeCreateParams) (*Volume, error) {
	return service.vol.Create(params)
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
