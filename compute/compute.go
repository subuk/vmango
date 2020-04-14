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
	node    NodeRepository
	key     KeyRepository
	net     NetworkRepository
	epub    EventPublisher
}

func New(epub EventPublisher, virt VirtualMachineRepository, vol VolumeRepository, volpool VolumePoolRepository, node NodeRepository, key KeyRepository, net NetworkRepository) *Service {
	return &Service{epub: epub, virt: virt, vol: vol, volpool: volpool, node: node, key: key, net: net}
}

func (service *Service) VirtualMachineList() ([]*VirtualMachine, error) {
	return service.virt.List()
}

func (service *Service) VirtualMachineDetail(id, node string) (*VirtualMachine, error) {
	return service.virt.Get(id, node)
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
	NodeId     string
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
		volume, _ := service.vol.GetByName(volumeParams.Pool, volumeParams.Name, params.NodeId)
		if volume == nil {
			if volumeParams.CloneFrom != "" {
				volumeCloneParams := VolumeCloneParams{
					NodeId:       params.NodeId,
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
					NodeId: params.NodeId,
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
		iface := &VirtualMachineAttachedInterface{
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

	vm, err := service.virt.Create(params.Id, params.NodeId, NewArch(params.Arch), params.VCpus, params.Memory, volumes, interfaces, config)
	if err != nil {
		return nil, util.NewError(err, "cannot create virtual machine")
	}
	if err := service.epub.Publish(NewEventVirtualMachineCreated(vm)); err != nil {
		service.virt.Delete(vm.Id, params.NodeId) // Ignore error
		return nil, util.NewError(err, "cannot publish event virtual machine created")
	}
	if params.Start {
		if err := service.virt.Start(vm.Id, params.NodeId); err != nil {
			return nil, util.NewError(err, "cannot start vm")
		}
	}
	return vm, nil
}

func (service *Service) VirtualMachineDelete(id, node string, deleteVolumes bool) error {
	volumesToDelete := []*VirtualMachineAttachedVolume{}
	if deleteVolumes {
		vm, err := service.virt.Get(id, node)
		if err != nil {
			return util.NewError(err, "cannot fetch vm info")
		}
		for _, volume := range vm.Volumes {
			volumesToDelete = append(volumesToDelete, volume)
		}
	}
	if err := service.virt.Delete(id, node); err != nil {
		return util.NewError(err, "cannot delete vm")
	}
	for _, volume := range volumesToDelete {
		if err := service.vol.Delete(volume.Path, node); err != nil {
			return util.NewError(err, "cannot delete volume")
		}
	}
	return nil
}

type VolumeAttachmentParams struct {
	MachineId  string
	NodeId     string
	DeviceName string
	VolumePath string
	DeviceType DeviceType
	DeviceBus  DeviceBus
}

func (service *Service) VirtualMachineAttachVolume(params VolumeAttachmentParams) error {
	vol, err := service.vol.Get(params.VolumePath, params.NodeId)
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
	return service.virt.AttachVolume(params.MachineId, params.NodeId, attachedVolume)
}

func (service *Service) VirtualMachineDetachVolume(id, node, path string) error {
	return service.virt.DetachVolume(id, node, path)
}

func (service *Service) VirtualMachineAttachInterface(id, node string, iface *VirtualMachineAttachedInterface) error {
	return service.virt.AttachInterface(id, node, iface)
}

type VirtualMachineUpdateParams struct {
	Vcpus         *int
	Memory        *Size
	Autostart     *bool
	GuestAgent    *bool
	GraphicType   *GraphicType
	GraphicListen *string
}

func (service *Service) VirtualMachineUpdate(id, node string, params VirtualMachineUpdateParams) error {
	return service.virt.Update(id, node, params)
}

func (service *Service) VirtualMachineDetachInterface(id, node, mac string) error {
	return service.virt.DetachInterface(id, node, mac)
}

func (service *Service) VirtualMachineGetConsoleStream(id, node string) (VirtualMachineConsoleStream, error) {
	return service.virt.GetConsoleStream(id, node)
}

func (service *Service) VirtualMachineGetGraphicStream(id, node string) (VirtualMachineGraphicStream, error) {
	return service.virt.GetGraphicStream(id, node)
}

func (service *Service) VolumeList(options VolumeListOptions) ([]*Volume, error) {
	return service.vol.List(options)
}

type VolumeListOptions struct {
	NodeId string
}

func (service *Service) ImageList(options VolumeListOptions) ([]*Volume, error) {
	volumes, err := service.vol.List(options)
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

func (service *Service) VolumeGet(path, node string) (*Volume, error) {
	return service.vol.Get(path, node)
}

type VolumeCloneParams struct {
	NodeId       string
	Format       VolumeFormat
	OriginalPath string
	NewName      string
	NewPool      string
	NewSize      Size
}

func (service *Service) VolumeClone(params VolumeCloneParams) (*Volume, error) {
	return service.vol.Clone(params)
}

func (service *Service) VolumeResize(path, node string, size Size) error {
	return service.vol.Resize(path, node, size)
}

type VolumePoolListOptions struct {
	NodeId string
}

func (service *Service) VolumePoolList(options VolumePoolListOptions) ([]*VolumePool, error) {
	return service.volpool.List(options)
}

type VolumeCreateParams struct {
	NodeId string
	Name   string
	Pool   string
	Format VolumeFormat
	Size   Size
}

func (service *Service) VolumeCreate(params VolumeCreateParams) (*Volume, error) {
	return service.vol.Create(params)
}

func (service *Service) VolumeDelete(path, node string) error {
	return service.vol.Delete(path, node)
}

func (service *Service) NodeGet(node string) (*Node, error) {
	return service.node.Get(node)
}

func (service *Service) NodeList() ([]*Node, error) {
	return service.node.List()
}

func (service *Service) VirtualMachineAction(id string, node, action string) error {
	switch action {
	default:
		return fmt.Errorf("unknown action %s", action)
	case "reboot":
		return service.virt.Reboot(id, node)
	case "poweroff":
		return service.virt.Poweroff(id, node)
	case "start":
		return service.virt.Start(id, node)
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

type NetworkListOptions struct {
	NodeId string
}

func (service *Service) NetworkList(options NetworkListOptions) ([]*Network, error) {
	return service.net.List(options)
}

func (service *Service) NetworkGet(id, node string) (*Network, error) {
	return service.net.Get(id, node)
}
