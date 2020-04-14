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
	virt VirtualMachineRepository
	vol  VolumeRepository
	epub EventPublisher
	key  KeyRepository
}

func New(epub EventPublisher, virt VirtualMachineRepository, vol VolumeRepository, key KeyRepository) *Service {
	return &Service{epub: epub, virt: virt, vol: vol, key: key}
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
				clonedVolume, err := service.vol.Clone(volumeCloneParams)
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
				createdVolume, err := service.vol.Create(volumeCreateParams)
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
