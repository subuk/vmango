package compute

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"subuk/vmango/configdrive"
	"subuk/vmango/tasks"
	"subuk/vmango/util"
	"sync"

	"github.com/google/uuid"
)

type VirtualMachineManagerNodeSettings struct {
	CdFormat configdrive.Format
	CdSuffix string
	CdPool   string
}

type VirtualMachineManagerClonedVolumeParams struct {
	Cloud, StorageClass string
	ImageID             string
	OriginalPath        string
	NewName             string
	NewPool             string
	NewSize             Size
	NewFormat           VolumeFormat
	Alias               string
	DeviceType          DeviceType
	DeviceBus           DeviceBus
}

type VirtualMachineManagerCreatedVolumeParams struct {
	Cloud, StorageClass string
	Name                string
	Pool                string
	Format              VolumeFormat
	Size                Size
	Alias               string
	DeviceType          DeviceType
	DeviceBus           DeviceBus
}

type VirtualMachineManager struct {
	creationBindings map[string]json.RawMessage
	Clouds           *CloudService
	createMu         sync.Mutex
	images           *ImageService
	vms              *VirtualMachineService
	volumes          *VolumeService
	settings         map[string]VirtualMachineManagerNodeSettings
	epub             EventPublisher
}

func NewVirtualMachineManager(vms *VirtualMachineService, volumes *VolumeService, epub EventPublisher, settings map[string]VirtualMachineManagerNodeSettings, images *ImageService) *VirtualMachineManager {
	return &VirtualMachineManager{
		images:   images,
		vms:      vms,
		volumes:  volumes,
		epub:     epub,
		settings: settings,
	}
}

func (m *VirtualMachineManager) withImages(images *ImageService) *VirtualMachineManager {
	return NewVirtualMachineManager(m.vms, m.volumes, m.epub, m.settings, images)
}

func (manager *VirtualMachineManager) Create(vm *VirtualMachine, cloneVols []VirtualMachineManagerClonedVolumeParams, newVols []VirtualMachineManagerCreatedVolumeParams, start bool) error {
	return manager.CreateWithImageDefaults(vm, cloneVols, newVols, start, ImageDefaults{})
}

func (manager *VirtualMachineManager) AttachVolume(id, node string, disk *VirtualMachineAttachedVolume) error {
	if disk == nil {
		return ErrInvalid
	}
	manager.createMu.Lock()
	defer manager.createMu.Unlock()
	if err := manager.volumes.ValidateAttachable(disk.Path, node, manager.images); err != nil {
		return err
	}
	return manager.vms.AttachVolume(id, node, disk)
}

func (manager *VirtualMachineManager) PrepareCreation(vm *VirtualMachine, cloneVols []VirtualMachineManagerClonedVolumeParams, newVols []VirtualMachineManagerCreatedVolumeParams, overrides ImageDefaults) (*VirtualMachine, []VirtualMachineManagerClonedVolumeParams, error) {
	if vm == nil || vm.Id == "" || vm.NodeId == "" {
		return nil, nil, ErrInvalid
	}
	if err := manager.vms.ValidateNew(vm); err != nil {
		return nil, nil, err
	}
	prepared, clones, err := manager.PrepareImages(vm, cloneVols, newVols, overrides)
	if err != nil {
		return nil, nil, err
	}
	vm, cloneVols = prepared, clones
	if !vm.Graphic.BindingResolved {
		binding, pinned := manager.creationBindings[vm.NodeId]
		if !pinned {
			binding, err = manager.vms.SnapshotCreationBinding(vm.NodeId)
			if err != nil {
				return nil, nil, err
			}
		}
		if err = manager.vms.applyCreationBinding(vm, binding); err != nil {
			return nil, nil, err
		}
		vm.Graphic.BindingResolved = true
	}
	if err := ValidateVM(vm); err != nil {
		return nil, nil, err
	}
	if !resourceName.MatchString(vm.Id) {
		return nil, nil, fmt.Errorf("%w: invalid VM name", ErrInvalid)
	}
	for _, v := range vm.Volumes {
		if err := ValidateVolumeAttachment(v); err != nil {
			return nil, nil, err
		}
		if err := manager.volumes.ValidateAttachable(v.Path, vm.NodeId, manager.images); err != nil {
			return nil, nil, err
		}
	}
	for _, n := range vm.Interfaces {
		if err := manager.vms.ValidateNetwork(vm.NodeId, n); err != nil {
			return nil, nil, err
		}
		if err := ValidateInterface(n); err != nil {
			return nil, nil, err
		}
	}
	for _, p := range newVols {
		if err := validateVolumeCreate(VolumeCreateParams{NodeId: vm.NodeId, Name: p.Name, Pool: p.Pool, Format: p.Format, Size: p.Size}); err != nil {
			return nil, nil, err
		}
		if err := manager.volumes.ValidateCreate(VolumeCreateParams{NodeId: vm.NodeId, Name: p.Name, Pool: p.Pool, Format: p.Format, Size: p.Size}); err != nil {
			return nil, nil, err
		}
	}
	if vm.Config != nil && vm.Config.Initialization != "" && configdrive.NewFormat(vm.Config.Initialization) == configdrive.FormatUnknown {
		return nil, nil, fmt.Errorf("%w: invalid initialization", ErrInvalid)
	}
	return vm, cloneVols, nil
}

func (manager *VirtualMachineManager) CreateWithImageDefaults(vm *VirtualMachine, cloneVols []VirtualMachineManagerClonedVolumeParams, newVols []VirtualMachineManagerCreatedVolumeParams, start bool, overrides ImageDefaults) error {
	manager.createMu.Lock()
	defer manager.createMu.Unlock()
	vm, cloneVols, err := manager.PrepareCreation(vm, cloneVols, newVols, overrides)
	if err != nil {
		return err
	}
	return manager.createPrepared(vm, cloneVols, newVols, start, manager.settings[vm.NodeId], nil)
}

// createPrepared executes the accepted snapshot without reapplying a changed
// catalog after restart. Progress writes precede every provisioning side effect.
func (manager *VirtualMachineManager) createPrepared(vm *VirtualMachine, cloneVols []VirtualMachineManagerClonedVolumeParams, newVols []VirtualMachineManagerCreatedVolumeParams, start bool, settings VirtualMachineManagerNodeSettings, progress *tasks.Execution) (err error) {
	failure := &ProvisioningError{Node: vm.NodeId, VM: vm.Id, Volumes: []string{}}
	mutated := false
	defer func() {
		if err != nil && mutated {
			failure.Cause = err
			err = failure
		}
	}()
	vmRef := VMRef(vm.NodeId, vm.Id)
	stage := func(name string, ref tasks.ResourceRef, mutation bool, run func() error) error {
		failure.Stage = name
		mutated = mutated || mutation
		if progress == nil {
			return run()
		}
		return progress.Stage(name, &ref, mutation, run)
	}
	effect := func(ref tasks.ResourceRef, action string) error {
		if progress == nil {
			return nil
		}
		return progress.Effect(ref, action)
	}
	if progress != nil {
		// Queued work may execute after downtime. Recheck mutable backend
		// prerequisites without reapplying catalog defaults to the snapshot.
		if err := stage("validate-creation", vmRef, false, func() error {
			if err := manager.vms.ValidateNew(vm); err != nil {
				return err
			}
			for _, disk := range vm.Volumes {
				if err := manager.volumes.ValidateAttachable(disk.Path, vm.NodeId, manager.images); err != nil {
					return err
				}
			}
			for _, disk := range cloneVols {
				if err := manager.volumes.ValidateCreate(VolumeCreateParams{NodeId: vm.NodeId, Pool: disk.NewPool, Name: disk.NewName, Size: disk.NewSize, Format: disk.NewFormat}); err != nil {
					return err
				}
			}
			for _, disk := range newVols {
				if err := manager.volumes.ValidateCreate(VolumeCreateParams{NodeId: vm.NodeId, Pool: disk.Pool, Name: disk.Name, Size: disk.Size, Format: disk.Format}); err != nil {
					return err
				}
			}
			if vm.Config != nil {
				return manager.volumes.ValidateCreate(VolumeCreateParams{NodeId: vm.NodeId, Pool: settings.CdPool, Name: vm.Id + settings.CdSuffix, Format: VolumeFormatIso})
			}
			return nil
		}); err != nil {
			return err
		}
	}
	for _, p := range cloneVols {
		params := VolumeCloneParams{Cloud: p.Cloud, StorageClass: p.StorageClass, NodeId: vm.NodeId, Format: p.NewFormat, OriginalPath: p.OriginalPath, NewName: p.NewName, NewPool: p.NewPool, NewSize: p.NewSize}
		if err := stage("clone-volume", vmRef, true, func() error {
			verifiedManaged := false
			if p.ImageID != "" && manager.images != nil {
				i, imageErr := manager.images.Get(p.ImageID)
				if imageErr != nil {
					return imageErr
				}
				if i.Managed() {
					source, sourceErr := manager.images.Source(i, vm.NodeId, manager.volumes)
					if sourceErr != nil {
						return sourceErr
					}
					if source.Path != params.OriginalPath {
						return fmt.Errorf("managed source differs from pinned clone")
					}
					verifiedManaged = true
				}
			}
			volume, err := manager.volumes.clone(params, verifiedManaged)
			if err != nil {
				return err
			}
			failure.Volumes = append(failure.Volumes, volume.Path)
			vm.Volumes = append(vm.Volumes, &VirtualMachineAttachedVolume{Path: volume.Path, Alias: p.Alias, DeviceType: p.DeviceType, DeviceBus: p.DeviceBus})
			return effect(VolumeRef(vm.NodeId, volume.Path), "created")
		}); err != nil {
			return err
		}
	}
	for _, p := range newVols {
		params := VolumeCreateParams{Cloud: p.Cloud, StorageClass: p.StorageClass, NodeId: vm.NodeId, Name: p.Name, Pool: p.Pool, Format: p.Format, Size: p.Size}
		if err := stage("create-volume", vmRef, true, func() error {
			volume, err := manager.volumes.Create(params)
			if err != nil {
				return err
			}
			failure.Volumes = append(failure.Volumes, volume.Path)
			vm.Volumes = append(vm.Volumes, &VirtualMachineAttachedVolume{Path: volume.Path, Alias: p.Alias, DeviceType: p.DeviceType, DeviceBus: p.DeviceBus})
			return effect(VolumeRef(vm.NodeId, volume.Path), "created")
		}); err != nil {
			return err
		}
	}
	if err := stage("define-vm", vmRef, true, func() error {
		if err := manager.vms.Save(vm); err != nil {
			return err
		}
		failure.VMCreated = true
		return effect(vmRef, "created")
	}); err != nil {
		return err
	}
	if vm.Config != nil {
		format := settings.CdFormat
		if vm.Config.Initialization != "" {
			format = configdrive.NewFormat(vm.Config.Initialization)
		}
		var cdFile *os.File
		var cdLen int64
		defer func() {
			if cdFile != nil {
				cdFile.Close()
				os.Remove(cdFile.Name())
			}
		}()
		if err := stage("generate-config-drive", vmRef, false, func() error {
			var err error
			cdFile, err = manager.generateConfigDrive(vm.Config, format)
			if err != nil {
				return err
			}
			cdLen, err = cdFile.Seek(0, io.SeekEnd)
			if err != nil {
				return err
			}
			_, err = cdFile.Seek(0, io.SeekStart)
			return err
		}); err != nil {
			return err
		}
		var cdVolume *Volume
		if err := stage("create-config-drive", vmRef, true, func() error {
			var err error
			cdVolume, err = manager.volumes.Create(VolumeCreateParams{NodeId: vm.NodeId, Name: vm.Id + settings.CdSuffix, Pool: settings.CdPool, Format: VolumeFormatIso, Size: NewSize(uint64(cdLen), SizeUnitB)})
			if err != nil {
				return err
			}
			failure.Volumes = append(failure.Volumes, cdVolume.Path)
			return effect(VolumeRef(vm.NodeId, cdVolume.Path), "created")
		}); err != nil {
			return err
		}
		cdRef := VolumeRef(vm.NodeId, cdVolume.Path)
		if err := stage("upload-config-drive", cdRef, true, func() error {
			if err := manager.volumes.Upload(cdVolume.Path, cdVolume.NodeId, cdFile, cdVolume.Size.Bytes()); err != nil {
				return err
			}
			return effect(cdRef, "uploaded")
		}); err != nil {
			return err
		}
		if err := stage("attach-config-drive", vmRef, true, func() error {
			if err := manager.vms.AttachVolume(vm.Id, vm.NodeId, &VirtualMachineAttachedVolume{Path: cdVolume.Path, Alias: "configdrive", DeviceType: DeviceTypeDisk, DeviceBus: DeviceBusVirtio}); err != nil {
				return err
			}
			return effect(cdRef, "attached")
		}); err != nil {
			return err
		}
	}
	if progress != nil {
		if err := stage("verify-vm", vmRef, true, func() error {
			observed, err := manager.vms.Get(vm.Id, vm.NodeId)
			if err != nil {
				return err
			}
			if observed.VCpus != vm.VCpus || observed.Memory.Bytes() != vm.Memory.Bytes() || observed.Autostart != vm.Autostart {
				return fmt.Errorf("VM configuration does not match the accepted command")
			}
			for _, disk := range vm.Volumes {
				if observed.AttachmentInfo(disk.Path) == nil {
					return fmt.Errorf("requested disk attachment is not confirmed")
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if err := stage("created-event", vmRef, true, func() error { return manager.epub.Publish(NewEventVirtualMachineCreated(vm)) }); err != nil {
		return err
	}
	if start {
		if err := stage("start-vm", vmRef, true, func() error {
			if err := manager.vms.Start(vm.Id, vm.NodeId); err != nil {
				return err
			}
			return effect(vmRef, "start-request-completed")
		}); err != nil {
			return err
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
