package compute

import (
	"fmt"
	"io"
	"os"
	"subuk/vmango/configdrive"
	"subuk/vmango/util"

	"github.com/google/uuid"
)

type VirtualMachineManagerNodeSettings struct {
	CdFormat configdrive.Format
	CdSuffix string
	CdPool   string
}

type VirtualMachineManagerClonedVolumeParams struct {
	OriginalPath string
	NewName      string
	NewPool      string
	NewSize      Size
	NewFormat    VolumeFormat
	Alias        string
	DeviceType   DeviceType
	DeviceBus    DeviceBus
}

type VirtualMachineManagerCreatedVolumeParams struct {
	Name       string
	Pool       string
	Format     VolumeFormat
	Size       Size
	Alias      string
	DeviceType DeviceType
	DeviceBus  DeviceBus
}

type VirtualMachineManagerCreateParams struct {
	Vm            *VirtualMachine
	CloneVolumes  []VolumeCloneParams
	CreateVolumes []VolumeCreateParams
	Start         bool
}

type VirtualMachineManager struct {
	vms      *VirtualMachineService
	volumes  *VolumeService
	settings map[string]VirtualMachineManagerNodeSettings
	epub     EventPublisher
}

func NewVirtualMachineManager(vms *VirtualMachineService, volumes *VolumeService, epub EventPublisher, settings map[string]VirtualMachineManagerNodeSettings) *VirtualMachineManager {
	return &VirtualMachineManager{
		vms:      vms,
		volumes:  volumes,
		epub:     epub,
		settings: settings,
	}
}

func (manager *VirtualMachineManager) Create(
	vm *VirtualMachine,
	image *ImageManifest,
	cloneVols []VirtualMachineManagerClonedVolumeParams,
	newVols []VirtualMachineManagerCreatedVolumeParams,
	start bool,
) error {
	for _, p := range cloneVols {
		params := VolumeCloneParams{
			NodeId:       vm.NodeId,
			Format:       p.NewFormat,
			OriginalPath: p.OriginalPath,
			NewName:      p.NewName,
			NewPool:      p.NewPool,
			NewSize:      p.NewSize,
		}
		volume, err := manager.volumes.Clone(params)
		if err != nil {
			return util.NewError(err, "cannot clone volume")
		}
		vm.Volumes = append(vm.Volumes, &VirtualMachineAttachedVolume{
			Path:       volume.Path,
			Alias:      p.Alias,
			DeviceType: p.DeviceType,
			DeviceBus:  p.DeviceBus,
		})
	}
	for _, p := range newVols {
		params := VolumeCreateParams{
			NodeId: vm.NodeId,
			Name:   p.Name,
			Pool:   p.Pool,
			Format: p.Format,
			Size:   p.Size,
		}
		volume, err := manager.volumes.Create(params)
		if err != nil {
			return util.NewError(err, "cannot create volume")
		}
		vm.Volumes = append(vm.Volumes, &VirtualMachineAttachedVolume{
			Path:       volume.Path,
			Alias:      p.Alias,
			DeviceType: p.DeviceType,
			DeviceBus:  p.DeviceBus,
		})
	}
	if err := manager.vms.Save(vm); err != nil {
		return err
	}
	settings := manager.settings[vm.NodeId]
	if vm.Config != nil {
		cdFile, err := manager.generateConfigDrive(vm.Config, settings.CdFormat)
		if err != nil {
			return util.NewError(err, "cannot generate configdrive")
		}
		cdLen, err := cdFile.Seek(0, io.SeekEnd)
		if err != nil {
			return util.NewError(err, "cannot get configdrive length")
		}
		if _, err := cdFile.Seek(0, io.SeekStart); err != nil {
			return util.NewError(err, "configdrive seek to start failed")
		}
		cdVolumeParams := VolumeCreateParams{
			NodeId: vm.NodeId,
			Name:   vm.Id + settings.CdSuffix,
			Pool:   settings.CdPool,
			Format: VolumeFormatIso,
			Size:   NewSize(uint64(cdLen), SizeUnitB),
		}
		cdVolume, err := manager.volumes.Create(cdVolumeParams)
		if err != nil {
			return util.NewError(err, "cannot create configdrive volume")
		}
		if err := manager.volumes.Upload(cdVolume.Path, cdVolume.NodeId, cdFile, cdVolume.Size.Bytes()); err != nil {
			return util.NewError(err, "cannot upload configdrive volume")
		}
		attachedVolume := &VirtualMachineAttachedVolume{
			Path:       cdVolume.Path,
			Alias:      "configdrive",
			DeviceType: DeviceTypeCdrom,
			DeviceBus:  DeviceBusIde,
		}
		if err := manager.vms.AttachVolume(vm.Id, vm.NodeId, attachedVolume); err != nil {
			return util.NewError(err, "cannot attach configdrive volume")
		}
	}
	if err := manager.epub.Publish(NewEventVirtualMachineCreated(vm)); err != nil {
		manager.vms.Delete(vm.Id, vm.NodeId) // Ignore error
		return util.NewError(err, "cannot publish event virtual machine created")
	}
	if start {
		if err := manager.vms.Start(vm.Id, vm.NodeId); err != nil {
			return util.NewError(err, "cannot start vm")
		}
	}
	return nil
}

func (manager *VirtualMachineManager) Delete(id, node string, deleteVolumes bool) error {
	volumesToDelete := []*VirtualMachineAttachedVolume{}
	if deleteVolumes {
		vm, err := manager.vms.Get(id, node)
		if err != nil {
			return util.NewError(err, "cannot fetch vm info")
		}
		for _, volume := range vm.Volumes {
			volumesToDelete = append(volumesToDelete, volume)
		}
	}
	if err := manager.vms.Delete(id, node); err != nil {
		return util.NewError(err, "cannot delete vm")
	}
	for _, volume := range volumesToDelete {
		if err := manager.volumes.Delete(volume.Path, node); err != nil {
			return util.NewError(err, "cannot delete volume")
		}
	}
	return nil
}

func (manager *VirtualMachineManager) generateConfigDrive(config *VirtualMachineConfig, format configdrive.Format) (*os.File, error) {
	var data configdrive.Data
	switch format {
	default:
		panic(fmt.Errorf("unknown configdrive write format '%s'", format))
	case configdrive.FormatOpenstack:
		osData := &configdrive.Openstack{
			Userdata: config.Userdata,
			Metadata: configdrive.OpenstackMetadata{
				Az:          "none",
				Files:       []struct{}{},
				Hostname:    config.Hostname,
				LaunchIndex: 0,
				Name:        config.Hostname,
				Meta:        map[string]string{},
				PublicKeys:  map[string]string{},
				UUID:        uuid.New().String(),
			},
		}
		for _, key := range config.Keys {
			osData.Metadata.PublicKeys[key.Comment] = string(key.Value)
		}
		data = osData
	case configdrive.FormatNoCloud:
		nocloudData := &configdrive.NoCloud{
			Userdata: config.Userdata,
			Metadata: configdrive.NoCloudMetadata{
				InstanceId:    uuid.New().String(),
				Hostname:      config.Hostname,
				LocalHostname: config.Hostname,
			},
		}
		for _, key := range config.Keys {
			nocloudData.Metadata.PublicKeys = append(nocloudData.Metadata.PublicKeys, string(key.Value))
		}
		data = nocloudData
	}

	file, err := configdrive.GenerateIso(data)
	if err != nil {
		return nil, util.NewError(err, "cannot generate iso")
	}
	return file, nil
}
