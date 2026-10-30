package compute

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"subuk/vmango/configdrive"
)

var ErrInvalidImageRequest = errors.New("invalid image creation request")

func sizeBytes(s Size) (uint64, error) {
	if s.Value == 0 {
		return 0, nil
	}
	if s.Unit != SizeUnitB && s.Unit != SizeUnitK && s.Unit != SizeUnitM && s.Unit != SizeUnitG {
		return 0, fmt.Errorf("invalid size unit")
	}
	factor := NewSize(1, s.Unit).Bytes()
	if s.Value > math.MaxUint64/factor {
		return 0, fmt.Errorf("size overflows")
	}
	return s.Value * factor, nil
}

// PrepareImages resolves every image and validates the entire request before
// Create performs any mutations. Neither the catalog nor the caller's VM is changed.
func (m *VirtualMachineManager) PrepareImages(input *VirtualMachine, requested []VirtualMachineManagerClonedVolumeParams, newVolumes []VirtualMachineManagerCreatedVolumeParams, overrides ImageDefaults) (vm *VirtualMachine, clones []VirtualMachineManagerClonedVolumeParams, err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%w: %w", ErrInvalidImageRequest, err)
		}
	}()
	copyVM := *input
	vm = &copyVM
	clones = slices.Clone(requested)
	vm.Interfaces = nil
	for _, iface := range input.Interfaces {
		copyIface := *iface
		vm.Interfaces = append(vm.Interfaces, &copyIface)
	}
	vm.Volumes = slices.Clone(input.Volumes)
	if input.Config != nil {
		config := *input.Config
		vm.Config = &config
	}
	if m.images != nil {
		paths, e := m.images.SourcePaths()
		if e != nil {
			return nil, nil, e
		}
		for _, p := range clones {
			if p.ImageID == "" && paths[p.OriginalPath] {
				return nil, nil, fmt.Errorf("select an image identifier for source %q", p.OriginalPath)
			}
		}
	}
	hasImages := false
	nicOverride := overrides.NICModel
	needsNICDefault := false
	for _, iface := range vm.Interfaces {
		needsNICDefault = needsNICDefault || iface.Model == ""
	}
	if !needsNICDefault {
		nicOverride = "explicit interfaces"
	}
	common := ImageDefaults{}
	merge := func(label string, current *string, next, override string) error {
		if override != "" || next == "" {
			return nil
		}
		if *current != "" && *current != next {
			return fmt.Errorf("conflicting image defaults for %s; specify an override", label)
		}
		*current = next
		return nil
	}
	for n := range clones {
		p := &clones[n]
		if p.ImageID == "" {
			continue
		}
		hasImages = true
		if vm.Id == "" || vm.NodeId == "" {
			return nil, nil, fmt.Errorf("VM name and node are required")
		}
		if m.images == nil {
			return nil, nil, fmt.Errorf("image catalog is unavailable")
		}
		i, e := m.images.Get(p.ImageID)
		if e != nil {
			return nil, nil, e
		}
		if vm.Arch == ArchUnknown {
			vm.Arch = i.Arch
		}
		if i.Arch != vm.Arch {
			return nil, nil, fmt.Errorf("image %q architecture does not match the VM", i.ID)
		}
		source, e := m.images.Source(i, vm.NodeId, m.volumes)
		if e != nil {
			return nil, nil, fmt.Errorf("image %q source unavailable on node %q: %w", i.ID, vm.NodeId, e)
		}
		if source.NodeId != vm.NodeId || (!i.Managed() && (source.Path != i.SourcePath || source.Metadata.Hidden)) || source.AttachedTo != "" {
			return nil, nil, fmt.Errorf("image %q source is unavailable for cloning", i.ID)
		}
		if source.Format != VolumeFormatQcow2 && source.Format != VolumeFormatRaw {
			return nil, nil, fmt.Errorf("image %q source format is unsupported", i.ID)
		}
		p.OriginalPath = source.Path
		if p.NewName == "" || p.NewPool == "" {
			return nil, nil, fmt.Errorf("image %q needs a destination name and pool", i.ID)
		}
		capacity, e := sizeBytes(source.Size)
		if e != nil || capacity == 0 {
			return nil, nil, fmt.Errorf("image %q has invalid source capacity", i.ID)
		}
		size, e := sizeBytes(p.NewSize)
		if e != nil {
			return nil, nil, e
		}
		if size == 0 {
			size = max(capacity, i.MinimumDiskBytes)
		}
		if size < capacity || size < i.MinimumDiskBytes {
			return nil, nil, fmt.Errorf("image %q root disk needs at least %d bytes", i.ID, max(capacity, i.MinimumDiskBytes))
		}
		p.NewSize = NewSize(size, SizeUnitB)
		if p.NewFormat == VolumeFormatUnknown {
			p.NewFormat = source.Format
		}
		if p.NewFormat != VolumeFormatRaw && p.NewFormat != VolumeFormatQcow2 {
			return nil, nil, fmt.Errorf("unsupported clone format")
		}
		if p.DeviceType == DeviceTypeUnknown {
			p.DeviceType = DeviceTypeDisk
		}
		if p.DeviceType != DeviceTypeDisk {
			return nil, nil, fmt.Errorf("an image must be cloned as a disk")
		}
		if p.DeviceBus == DeviceBusUnknown {
			p.DeviceBus = i.Defaults.DiskBus
		}
		if overrides.DiskBus != DeviceBusUnknown {
			p.DeviceBus = overrides.DiskBus
		}
		if p.DeviceBus == DeviceBusUnknown {
			p.DeviceBus = DeviceBusVirtio
		}
		memory, e := sizeBytes(vm.Memory)
		if e != nil || memory == 0 || memory < i.MinimumMemoryBytes {
			return nil, nil, &ValidationError{Field: "memory_bytes", Message: fmt.Sprintf("Selected image requires at least %d bytes of memory.", i.MinimumMemoryBytes)}
		}
		for _, field := range []struct {
			name            string
			current         *string
			value, override string
		}{
			{"firmware", &common.Firmware, i.Defaults.Firmware, overrides.Firmware},
			{"NIC model", &common.NICModel, i.Defaults.NICModel, nicOverride},
			{"initialization", &common.Initialization, i.Defaults.Initialization, overrides.Initialization},
		} {
			if e := merge(field.name, field.current, field.value, field.override); e != nil {
				return nil, nil, e
			}
		}
		if p.DeviceBus == DeviceBusScsi {
			if e := merge("disk controller", &common.DiskController, i.Defaults.DiskController, overrides.DiskController); e != nil {
				return nil, nil, e
			}
		}
		if common.Graphics != "" && i.Defaults.Graphics != "" && common.Graphics != i.Defaults.Graphics {
			return nil, nil, fmt.Errorf("conflicting image graphics requirements")
		}
		if common.VideoModel != "" && i.Defaults.VideoModel != "" && common.VideoModel != i.Defaults.VideoModel {
			return nil, nil, fmt.Errorf("conflicting image video requirements")
		}
		if i.Defaults.Graphics != "" {
			common.Graphics = i.Defaults.Graphics
		}
		if i.Defaults.VideoModel != "" {
			common.VideoModel = i.Defaults.VideoModel
		}
		if overrides.GuestAgent == nil && i.Defaults.GuestAgent != nil {
			if common.GuestAgent != nil && *common.GuestAgent != *i.Defaults.GuestAgent {
				return nil, nil, fmt.Errorf("conflicting image defaults for guest agent; specify an override")
			}
			common.GuestAgent = i.Defaults.GuestAgent
		}
	}

	if !hasImages {
		if overrides.Firmware != "" {
			vm.Firmware = overrides.Firmware
			if vm.Firmware == "auto" {
				vm.Firmware = ""
			}
		}
		if overrides.DiskController != "" {
			vm.DiskController = overrides.DiskController
		}
		if overrides.GuestAgent != nil {
			vm.GuestAgent = *overrides.GuestAgent
		}
		if overrides.Initialization == "none" {
			vm.Config = nil
		} else if overrides.Initialization != "" {
			if vm.Config == nil {
				vm.Config = &VirtualMachineConfig{Hostname: vm.Name}
			}
			vm.Config.Initialization = overrides.Initialization
		}
		for _, n := range vm.Interfaces {
			if n.Model == "" {
				n.Model = overrides.NICModel
			}
			if n.Model == "" {
				n.Model = "virtio"
			}
		}
		return vm, clones, nil
	}
	// A raw path must not bypass image rules when the caller also selects images.
	for _, p := range clones {
		if p.ImageID == "" {
			return nil, nil, fmt.Errorf("image creation requires an image identifier for each clone")
		}
	}
	if overrides.Firmware != "" {
		common.Firmware = overrides.Firmware
	}
	if overrides.DiskController != "" {
		common.DiskController = overrides.DiskController
	}
	if overrides.NICModel != "" {
		common.NICModel = overrides.NICModel
	}
	if overrides.Initialization != "" {
		common.Initialization = overrides.Initialization
	}
	if overrides.GuestAgent != nil {
		common.GuestAgent = overrides.GuestAgent
	}
	if common.Firmware != "" {
		vm.Firmware = common.Firmware
	}
	if vm.Firmware == "auto" {
		vm.Firmware = ""
	}
	vm.Graphic.Type, vm.VideoModel = common.Display()
	vm.DiskController = common.DiskController
	vm.GuestAgent = true
	if common.GuestAgent != nil {
		vm.GuestAgent = *common.GuestAgent
	}
	for _, iface := range vm.Interfaces {
		if iface.Model == "" {
			iface.Model = common.NICModel
		}
		if iface.Model == "" {
			iface.Model = "virtio"
		}
	}
	initialization := common.Initialization
	if initialization == "" {
		initialization = m.settings[vm.NodeId].CdFormat.String()
	}
	if initialization == "none" {
		vm.Config = nil
	} else {
		if configdrive.NewFormat(initialization) == configdrive.FormatUnknown {
			return nil, nil, fmt.Errorf("unsupported initialization mode %q", initialization)
		}
		if vm.Config == nil {
			vm.Config = &VirtualMachineConfig{Hostname: vm.Name}
		}
		vm.Config.Initialization = initialization
	}
	if !resourceName.MatchString(vm.Name) {
		return nil, nil, fmt.Errorf("invalid VM name")
	}
	if e := ValidateVM(vm); e != nil {
		return nil, nil, e
	}
	for _, iface := range vm.Interfaces {
		if e := ValidateInterface(iface); e != nil {
			return nil, nil, e
		}
	}
	if e := m.vms.ValidateNew(vm); e != nil {
		return nil, nil, e
	}
	caps, e := m.vms.Capabilities(vm.NodeId, vm.Arch)
	if e != nil {
		return nil, nil, e
	}
	if caps.Arch != vm.Arch {
		return nil, nil, fmt.Errorf("target node does not support image architecture")
	}
	if vm.VCpus < 1 || (caps.MaxVCPUs > 0 && uint(vm.VCpus) > caps.MaxVCPUs) {
		return nil, nil, fmt.Errorf("unsupported vCPU count")
	}
	if vm.Firmware != "" && !slices.Contains(caps.Firmwares, vm.Firmware) {
		return nil, nil, fmt.Errorf("firmware %q is unsupported on this node", vm.Firmware)
	}
	if vm.DiskController != "" && !slices.Contains(caps.DiskControllers, vm.DiskController) {
		return nil, nil, fmt.Errorf("unsupported disk controller")
	}
	if !slices.Contains(caps.Graphics, vm.Graphic.Type) || !slices.Contains(caps.VideoModels, vm.VideoModel) {
		return nil, nil, fmt.Errorf("graphics or video model is unsupported on this node")
	}
	hasScsi := false
	destinations := map[string]bool{}
	validateDisk := func(pool, name string, bus DeviceBus, size Size) error {
		if !slices.Contains(caps.DiskBuses, bus) {
			return fmt.Errorf("disk bus %q is unsupported on this node", bus)
		}
		hasScsi = hasScsi || bus == DeviceBusScsi
		bytes, e := sizeBytes(size)
		if e != nil || bytes == 0 {
			return fmt.Errorf("invalid disk capacity")
		}
		key := pool + "\x00" + name
		if pool == "" || name == "" || destinations[key] {
			return fmt.Errorf("missing or duplicate destination volume")
		}
		destinations[key] = true
		return nil
	}
	for _, p := range clones {
		if e := validateDisk(p.NewPool, p.NewName, p.DeviceBus, p.NewSize); e != nil {
			return nil, nil, e
		}
	}
	for _, p := range newVolumes {
		if e := validateDisk(p.Pool, p.Name, p.DeviceBus, p.Size); e != nil {
			return nil, nil, e
		}
		if (p.Format != VolumeFormatRaw && p.Format != VolumeFormatQcow2) || (p.DeviceType != DeviceTypeDisk && p.DeviceType != DeviceTypeCdrom) {
			return nil, nil, fmt.Errorf("unsupported new volume format or device")
		}
	}
	for _, p := range vm.Volumes {
		if !slices.Contains(caps.DiskBuses, p.DeviceBus) {
			return nil, nil, fmt.Errorf("unsupported attached disk bus")
		}
		hasScsi = hasScsi || p.DeviceBus == DeviceBusScsi
	}
	if vm.DiskController != "" && !hasScsi {
		return nil, nil, fmt.Errorf("disk controller requires a SCSI disk")
	}
	for _, iface := range vm.Interfaces {
		if !slices.Contains(caps.NICModels, iface.Model) {
			return nil, nil, fmt.Errorf("NIC model %q is unsupported on this node", iface.Model)
		}
		if models, restricted := caps.NetworkModels[iface.NetworkName]; restricted && !slices.Contains(models, iface.Model) {
			return nil, nil, fmt.Errorf("NIC model is unsupported for the selected network")
		}
	}
	// Check all destination pools and names before executing even the first clone.
	for _, p := range clones {
		if e := m.volumes.ValidateCreate(VolumeCreateParams{NodeId: vm.NodeId, Pool: p.NewPool, Name: p.NewName, Format: p.NewFormat, Size: p.NewSize}); e != nil {
			return nil, nil, e
		}
	}
	for _, p := range newVolumes {
		if e := m.volumes.ValidateCreate(VolumeCreateParams{NodeId: vm.NodeId, Pool: p.Pool, Name: p.Name, Format: p.Format, Size: p.Size}); e != nil {
			return nil, nil, e
		}
	}
	if vm.Config != nil {
		settings := m.settings[vm.NodeId]
		name := vm.Name + settings.CdSuffix
		if destinations[settings.CdPool+"\x00"+name] {
			return nil, nil, fmt.Errorf("config drive conflicts with a requested disk")
		}
		if !slices.Contains(caps.DiskBuses, DeviceBusVirtio) {
			return nil, nil, fmt.Errorf("node cannot attach a virtio config drive")
		}
		if e := m.volumes.ValidateCreate(VolumeCreateParams{NodeId: vm.NodeId, Pool: settings.CdPool, Name: name, Format: VolumeFormatIso}); e != nil {
			return nil, nil, e
		}
	}
	return vm, clones, nil
}
