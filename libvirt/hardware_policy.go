package libvirt

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"

	vir "libvirt.org/go/libvirt"
	xmlmodel "libvirt.org/go/libvirtxml"
)

func (repo *VirtualMachineRepository) ConfigureHardwareProfiles(profiles map[string]map[string]hardware.Profile) {
	repo.hardwareProfiles = profiles
}

func (repo *VirtualMachineRepository) ResolveHardware(vm *compute.VirtualMachine, policy, binding json.RawMessage) error {
	p, err := hardware.DecodePolicy(policy)
	if err != nil {
		return err
	}
	var b creationBinding
	if err = json.Unmarshal(binding, &b); err != nil {
		return fmt.Errorf("missing accepted host binding: %w", err)
	}
	plan, err := hardware.Resolve(p, b.Emulator)
	if err != nil {
		return err
	}
	conn, e := repo.pool.Acquire(vm.NodeId)
	if e != nil {
		return e
	}
	hostRaw, e := conn.GetCapabilities()
	var host xmlmodel.Caps
	if e == nil {
		e = host.Unmarshal(hostRaw)
	}
	plan.CPUThreads = 1
	if e == nil {
		var caps *xmlmodel.DomainCaps
		caps, e = resolveMachineCapabilities(p, vm.Arch.String(), plan.Emulator, &host, queryDomainCapabilities(conn), func(c *xmlmodel.DomainCaps) error {
			if err := validateCreationCapabilities(vm, p, c); err != nil {
				return err
			}
			return validateGuestSystemCapabilities(conn, p, c)
		})
		if e == nil {
			plan.Emulator, plan.Machine, plan.DomainType = caps.Path, caps.Machine, caps.Domain
		}
		if host.Host.CPU != nil && host.Host.CPU.Topology != nil && host.Host.CPU.Topology.Threads > 0 && vm.VCpus%host.Host.CPU.Topology.Threads == 0 {
			plan.CPUThreads = host.Host.CPU.Topology.Threads
		}
	}
	repo.pool.Release(vm.NodeId)
	if e != nil {
		return e
	}
	if p.Hugepages {
		conn, e := repo.pool.Acquire(vm.NodeId)
		if e != nil {
			return e
		}
		plan.HugepageSizeKiB, e = selectHugepageSize(conn, vm.Memory.Bytes(), 0, false)
		repo.pool.Release(vm.NodeId)
		if e != nil {
			return e
		}
	}
	preview := &xmlmodel.Domain{UUID: vm.Id, OS: &xmlmodel.DomainOS{}}
	applyGuestSystem(preview, p)
	observed := observeGuestSystem(preview)
	plan.GuestSystem = &observed
	raw, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	if err = repo.ApplyHardware(vm, raw); err != nil {
		return err
	}
	return repo.validateHardwareCapabilities(vm, plan, false, true)
}
func (repo *VirtualMachineRepository) ApplyHardware(vm *compute.VirtualMachine, raw json.RawMessage) error {
	plan, err := hardware.DecodePlan(raw)
	if err != nil {
		return err
	}
	p := plan.Profile
	vm.AppliedPolicy = append(json.RawMessage(nil), raw...)
	vm.Firmware = p.Firmware
	if vm.Firmware == "auto" {
		vm.Firmware = ""
	}
	vm.DiskController = p.DiskController
	if p.ConfigDrive.IsEnabled() && p.ConfigDrive.Bus == "scsi" {
		vm.DiskController = "virtio-scsi"
	}
	vm.GuestAgent = p.GuestAgentEnabled()
	vm.Hugepages = p.Hugepages
	vm.Graphic.Type = compute.NewGraphicType(p.Graphics)
	vm.Graphic.BindingResolved = true
	vm.Graphic.Listen = ""
	if p.Graphics != "none" {
		vm.Graphic.Listen = "127.0.0.1"
	}
	vm.VideoModel = compute.NewVideoModel(p.VideoModel)
	for _, nic := range vm.Interfaces {
		if nic.Model != "" && nic.Model != p.NICModel {
			return &compute.ValidationError{Field: "network", Message: fmt.Sprintf("Network model %s conflicts with hardware profile %s model %s.", nic.Model, p.ID, p.NICModel)}
		}
		nic.Model = p.NICModel
	}
	vm.ConfigDriveAttachment = nil
	if !p.ConfigDrive.IsEnabled() {
		if vm.Config != nil && (len(vm.Config.Userdata) > 0 || len(vm.Config.Keys) > 0) {
			return &compute.ValidationError{Field: "hardware_profile", Message: fmt.Sprintf("Hardware profile %s disables config drive; provisioning data cannot be applied.", p.ID)}
		}
		vm.Config = nil
		return nil
	}
	if vm.Config == nil {
		vm.Config = &compute.VirtualMachineConfig{Hostname: vm.Name}
	}
	vm.Config.Initialization = p.ConfigDrive.Format
	vm.ConfigDriveAttachment = &compute.VirtualMachineAttachedVolume{Alias: "configdrive", GeneratedArtifact: true, DeviceType: compute.NewDeviceType(p.ConfigDrive.DeviceType), DeviceBus: compute.NewDeviceBus(p.ConfigDrive.Bus)}
	return nil
}
func (repo *VirtualMachineRepository) validateHardwareCapabilities(vm *compute.VirtualMachine, plan hardware.Plan, live, creation bool) error {
	conn, err := repo.pool.Acquire(vm.NodeId)
	if err != nil {
		return err
	}
	defer repo.pool.Release(vm.NodeId)
	raw, err := conn.GetDomainCapabilities(plan.Emulator, vm.Arch.String(), plan.Machine, plan.DomainType, 0)
	if err != nil {
		return repositoryError(err, "hardware profile capabilities")
	}
	capsXML := &xmlmodel.DomainCaps{}
	if err = capsXML.Unmarshal(raw); err != nil {
		return err
	}
	if err = validateMachineCapabilities(plan.Platform(), vm.Arch.String(), capsXML); err != nil {
		return err
	}
	if creation && plan.Profile.Machine != nil {
		if err = validateCreationCapabilities(vm, plan.Profile, capsXML); err != nil {
			return err
		}
	}
	if creation {
		if err = validateGuestSystemCapabilities(conn, plan.Profile, capsXML); err != nil {
			return err
		}
	}
	caps := imageCapabilities(capsXML)
	p := plan.Profile
	if caps.Arch != vm.Arch || caps.MaxVCPUs > 0 && uint(vm.VCpus) > caps.MaxVCPUs {
		return &compute.ValidationError{Field: "instance_type", Message: fmt.Sprintf("Hardware profile %s: guest architecture or CPU count is unsupported on this host.", p.ID)}
	}
	if err := validateProfileCapabilities(p, caps); err != nil {
		return err
	}
	for _, nic := range vm.Interfaces {
		if nic.Bridge {
			continue
		}
		if models, ok := caps.NetworkModels[nic.NetworkName]; ok && !slices.Contains(models, p.NICModel) {
			return &compute.ValidationError{Field: "network", Message: fmt.Sprintf("Network %s does not support hardware profile %s NIC model %s.", nic.NetworkName, p.ID, p.NICModel)}
		}
	}

	if p.Hugepages {
		_, err := selectHugepageSize(conn, vm.Memory.Bytes(), plan.HugepageSizeKiB, live)
		return err
	}
	return nil
}

// selectHugepageSize never combines incompatible page sizes into a fictitious
// capacity. The selected size is retained in the accepted plan and domain XML.
func selectHugepageSize(conn *vir.Connect, bytes, requiredSize uint64, live bool) (uint64, error) {
	raw, err := conn.GetCapabilities()
	if err != nil {
		return 0, err
	}
	var caps xmlmodel.Caps
	if err = caps.Unmarshal(raw); err != nil {
		return 0, err
	}
	available := map[uint64]uint64{}
	if caps.Host.NUMA != nil && caps.Host.NUMA.Cells != nil {
		for _, cell := range caps.Host.NUMA.Cells.Cells {
			for _, page := range cell.PageInfo {
				if page.Size <= 4 {
					continue
				}
				size := uint64(page.Size)
				if requiredSize > 0 && size != requiredSize {
					continue
				}
				free := []uint64{page.Count}
				if live {
					var err error
					free, err = conn.GetFreePages([]uint64{size}, int(cell.ID), 1, 0)
					if err != nil {
						return 0, repositoryError(err, "read prepared hugepage availability")
					}
				}
				for _, count := range free {
					if count > ^uint64(0)-available[size] {
						return 0, fmt.Errorf("hugepage capacity overflow")
					}
					available[size] += count
				}
			}
		}
	}
	sizes := []uint64{}
	for size := range available {
		sizes = append(sizes, size)
	}
	slices.Sort(sizes)
	for _, size := range sizes {
		pageBytes := size * 1024
		if bytes%pageBytes != 0 {
			continue
		}
		if !live || available[size] >= bytes/pageBytes {
			return size, nil
		}
	}
	return 0, &compute.ValidationError{Field: "memory_bytes", Message: fmt.Sprintf("Prepared host hugepages cannot satisfy %d bytes with a compatible page size; prepare host pools explicitly.", bytes)}
}
func (repo *VirtualMachineRepository) ValidateAppliedPolicy(vm *compute.VirtualMachine) error {
	if vm.Metadata == nil {
		return &compute.AdoptionRequiredError{Message: "VM is read-only; explicitly adopt it with a compatible hardware profile."}
	}
	plan, err := hardware.DecodePlan(vm.AppliedPolicy)
	if err != nil {
		return &compute.AdoptionRequiredError{Message: "VM requires adoption or re-adoption with an explicit compatible hardware profile; its applied policy is missing or unsupported."}
	}
	if len(vm.ObservedBackend) > 0 {
		var observed hardware.PlatformBinding
		if err := json.Unmarshal(vm.ObservedBackend, &observed); err != nil {
			return err
		}
		if err := hardware.ValidateSavedPlatform(plan.Platform(), observed); err != nil {
			return &compute.AdoptionRequiredError{Message: "VM platform differs from its applied policy; inspect and explicitly re-adopt it: " + err.Error()}
		}
	}
	return nil
}
func (repo *VirtualMachineRepository) ResolveDiskAttachment(vm *compute.VirtualMachine, disk *compute.VirtualMachineAttachedVolume) error {
	plan, err := hardware.DecodePlan(vm.AppliedPolicy)
	if err != nil {
		return err
	}
	if disk.Alias == "configdrive" && !disk.GeneratedArtifact {
		return &compute.ValidationError{Field: "alias", Message: "The configdrive alias is reserved for VM-owned generated artifacts."}
	}
	if repo.pool != nil && vm.Id != "" {
		config, e := repo.persistentHardwareConfiguration(vm.Id, vm.NodeId)
		if e != nil && !errors.Is(e, vir.ERR_NO_DOMAIN) {
			return e
		}
		if e == nil {
			if e = validateAdoptionControllers(config, plan); e != nil {
				return &compute.ValidationError{Field: "disk", Message: e.Error()}
			}
			selected := plan.Profile.DiskBus
			if disk.Alias == "configdrive" {
				selected = plan.Profile.ConfigDrive.Bus
			}
			if _, e = allocateDiskAddress(config, selected); e != nil {
				return &compute.ValidationError{Field: "disk", Message: e.Error()}
			}
		}
	}
	bus := compute.NewDeviceBus(plan.Profile.DiskBus)
	kind := compute.DeviceTypeDisk
	if disk.Alias == "configdrive" {
		if !plan.Profile.ConfigDrive.IsEnabled() {
			return fmt.Errorf("hardware profile disables config drive")
		}
		bus = compute.NewDeviceBus(plan.Profile.ConfigDrive.Bus)
		kind = compute.NewDeviceType(plan.Profile.ConfigDrive.DeviceType)
	}
	if disk.DeviceBus != compute.DeviceBusUnknown && disk.DeviceBus != bus {
		return &compute.ValidationError{Field: "bus", Message: fmt.Sprintf("Attachment bus %s conflicts with applied hardware profile %s (%s); per-attachment overrides are unsupported.", disk.DeviceBus, plan.Profile.ID, bus)}
	}
	if disk.DeviceType != compute.DeviceTypeUnknown && disk.DeviceType != kind {
		return &compute.ValidationError{Field: "device", Message: "Attachment device type conflicts with the applied hardware profile."}
	}
	disk.DeviceBus = bus
	disk.DeviceType = kind
	count := 0
	for _, d := range vm.Volumes {
		if d == disk || d.Path != "" && disk.Path != "" && d.Path == disk.Path {
			continue
		}
		if d.DeviceBus == bus {
			count++
		}
	}
	if bus == compute.DeviceBusIde && count >= 4 {
		return &compute.ValidationError{Field: "disk", Message: "IDE controller capacity is exhausted (four devices); no artifact was created."}
	}
	if bus == compute.DeviceBusScsi && count >= 16383 {
		return &compute.ValidationError{Field: "disk", Message: "SCSI controller capacity is exhausted."}
	}
	return nil
}
func (repo *VirtualMachineRepository) ValidateResourceChange(current, requested *compute.VirtualMachine) error {
	if err := repo.ValidateAppliedPolicy(current); err != nil {
		return err
	}
	plan, err := hardware.DecodePlan(current.AppliedPolicy)
	if err != nil {
		return err
	}
	if current.Cpupin != nil && current.VCpus != requested.VCpus {
		return fmt.Errorf("explicit CPU pinning requires preserving the current vCPU count")
	}
	config, err := repo.persistentHardwareConfiguration(current.Id, current.NodeId)
	if err != nil {
		return err
	}
	if err = validateResourceTopology(config, requested.VCpus); err != nil {
		return err
	}
	requested.AppliedPolicy = append(json.RawMessage(nil), current.AppliedPolicy...)
	requested.Hugepages = plan.Profile.Hugepages
	requested.Autostart = current.Autostart
	return repo.validateHardwareCapabilities(requested, plan, true, false)
}
func (repo *VirtualMachineRepository) PrepareAdoption(vm *compute.VirtualMachine, cloud, profile string) (json.RawMessage, error) {
	p, ok := repo.hardwareProfiles[cloud][profile]
	if !ok {
		return nil, &compute.ValidationError{Field: "hardware_profile", Message: fmt.Sprintf("Cloud %s: select a known hardware profile for adoption.", cloud)}
	}
	plan, err := hardware.Resolve(p, repo.settings[vm.NodeId].Emulator)
	if err != nil {
		return nil, err
	}
	if err = validateAdoptionPolicy(vm, plan); err != nil {
		return nil, err
	}
	config, err := repo.persistentHardwareConfiguration(vm.Id, vm.NodeId)
	if err != nil {
		return nil, err
	}
	if err = validateGuestSystemAdoption(config, plan.Profile); err != nil {
		return nil, err
	}
	if err = validateAdoptionControllers(config, plan); err != nil {
		return nil, &compute.ValidationError{Field: "hardware_profile", Message: err.Error()}
	}
	if config.Devices != nil {
		plan.Emulator = config.Devices.Emulator
	}
	plan.DomainType = config.Type
	if config.OS != nil && config.OS.Type != nil {
		plan.Machine = config.OS.Type.Machine
	}
	if config.CPU != nil && config.CPU.Topology != nil {
		plan.CPUThreads = config.CPU.Topology.Threads
	}
	if err = repo.validateAdoptionMachine(vm, plan); err != nil {
		return nil, err
	}

	if plan.Profile.Hugepages {
		plan.HugepageSizeKiB, err = adoptedHugepageSize(config)
		if err != nil {
			return nil, err
		}
	}
	observed := observeGuestSystem(config)
	plan.GuestSystem = &observed
	return json.Marshal(plan)
}
func validateAdoptionPolicy(vm *compute.VirtualMachine, plan hardware.Plan) error {
	p := plan.Profile
	conflict := func(field string) error {
		return &compute.ValidationError{Field: "hardware_profile", Message: fmt.Sprintf("Adoption hardware profile %s is incompatible with existing %s; resolve explicitly without automatic conversion.", p.ID, field)}
	}
	if p.Hugepages != vm.Hugepages {
		return conflict("hugepages")
	}
	if p.GuestAgentEnabled() != vm.GuestAgent {
		return conflict("guest-agent channel")
	}
	if p.Graphics != vm.Graphic.Type.String() || p.VideoModel != vm.VideoModel.String() {
		return conflict("graphics/video")
	}
	if p.Firmware != "auto" && p.Firmware != vm.Firmware {
		return conflict("firmware")
	}
	for _, nic := range vm.Interfaces {
		if nic.Model != "" && nic.Model != p.NICModel {
			return conflict("NIC model")
		}
	}
	hasConfigDrive := false
	for _, disk := range vm.Volumes {
		if disk.Alias == "configdrive" {
			hasConfigDrive = true
			if vm.Config == nil || vm.Config.Initialization != p.ConfigDrive.Format {
				return conflict("config-drive data format (unreadable or different)")
			}
			if !p.ConfigDrive.IsEnabled() || disk.DeviceBus.String() != p.ConfigDrive.Bus || disk.DeviceType.String() != p.ConfigDrive.DeviceType {
				return conflict("config-drive attachment")
			}
			continue
		}
		if disk.DeviceType == compute.DeviceTypeDisk && disk.DeviceBus.String() != p.DiskBus {
			return conflict("ordinary-disk bus")
		}
	}
	if p.ConfigDrive.IsEnabled() != hasConfigDrive {
		return conflict("config-drive presence")
	}
	return nil
}
func (repo *VirtualMachineRepository) AdoptWithPolicy(id, node string, m *compute.MachineMetadata, raw json.RawMessage) error {
	plan, err := hardware.DecodePlan(raw)
	if err != nil {
		return err
	}
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return err
	}
	defer repo.pool.Release(node)
	domain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return err
	}
	defer domain.Free()
	desc, err := domain.GetXMLDesc(vir.DOMAIN_XML_INACTIVE | vir.DOMAIN_XML_SECURE)
	if err != nil {
		return err
	}
	var config xmlmodel.Domain
	if err = config.Unmarshal(desc); err != nil {
		return err
	}
	info, err := domain.GetInfo()
	if err != nil {
		return err
	}
	vm, err := VirtualMachineFromDomainConfig(&config, info)
	if err != nil {
		return err
	}
	for _, disk := range vm.Volumes {
		if disk.Alias == "configdrive" {
			vm.Config, err = repo.parseConfigDrive(conn, disk.Path)
			if err != nil {
				return fmt.Errorf("cannot verify existing config-drive format: %w", err)
			}
		}
	}
	if err = validateAdoptionPolicy(vm, plan); err != nil {
		return err
	}
	if err = validateAdoptionControllers(&config, plan); err != nil {
		return err
	}
	if err = validateAdoptionBindings(&config, plan); err != nil {
		return err
	}
	if plan.Profile.HasGuestSystemConstraints() {
		caps, e := queryDomainCapabilities(conn)(plan.Emulator, vm.Arch.String(), plan.Machine, plan.DomainType)
		if e != nil {
			return e
		}
		if e = validateGuestSystemCapabilities(conn, plan.Profile, caps); e != nil {
			return e
		}
	}
	if err = writeSchedulerMetadata(&config, m); err != nil {
		return err
	}
	if err = writeHardwareMetadata(&config, raw); err != nil {
		return err
	}
	desc, err = config.Marshal()
	if err != nil {
		return err
	}
	defined, err := conn.DomainDefineXML(desc)
	if err != nil {
		return err
	}
	defer defined.Free()
	return nil
}
func (repo *VirtualMachineRepository) SetAutostart(id, node string, enabled bool) error {
	if err := repo.requireManagedHardware(id, node); err != nil {
		return err
	}
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return err
	}
	defer repo.pool.Release(node)
	domain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return err
	}
	defer domain.Free()
	if err = domain.SetAutostart(enabled); err != nil {
		return repositoryError(err, "synchronize autostart")
	}
	actual, err := domain.GetAutostart()
	if err != nil {
		return err
	}
	if actual != enabled {
		return fmt.Errorf("autostart synchronization was not confirmed")
	}
	return nil
}

func (repo *VirtualMachineRepository) persistentHardwareConfiguration(id, node string) (*xmlmodel.Domain, error) {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return nil, err
	}
	defer repo.pool.Release(node)
	d, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return nil, err
	}
	defer d.Free()
	raw, err := d.GetXMLDesc(vir.DOMAIN_XML_INACTIVE | vir.DOMAIN_XML_SECURE)
	if err != nil {
		return nil, err
	}
	c := &xmlmodel.Domain{}
	if err = c.Unmarshal(raw); err != nil {
		return nil, err
	}
	return c, nil
}
func adoptedHugepageSize(config *xmlmodel.Domain) (uint64, error) {
	if config.MemoryBacking == nil || config.MemoryBacking.MemoryHugePages == nil {
		return 0, fmt.Errorf("adoption requires existing hugepage backing")
	}
	pages := config.MemoryBacking.MemoryHugePages.Hugepages
	if len(pages) != 1 || pages[0].Nodeset != "" {
		return 0, fmt.Errorf("adoption requires one explicit hugepage size without NUMA-specific overrides; resolve the existing hugepage configuration explicitly")
	}
	page := pages[0]
	size := uint64(page.Size)
	switch page.Unit {
	case "KiB", "k", "":
	case "MiB", "M":
		size *= 1024
	case "GiB", "G":
		size *= 1024 * 1024
	default:
		return 0, fmt.Errorf("unsupported existing hugepage unit %q", page.Unit)
	}
	if size <= 4 {
		return 0, fmt.Errorf("existing hugepage size is invalid")
	}
	return size, nil
}
func (repo *VirtualMachineRepository) ValidateAdoptionPolicy(vm *compute.VirtualMachine, raw json.RawMessage) error {
	plan, err := hardware.DecodePlan(raw)
	if err != nil {
		return err
	}
	if err = validateAdoptionPolicy(vm, plan); err != nil {
		return err
	}
	c, err := repo.persistentHardwareConfiguration(vm.Id, vm.NodeId)
	if err != nil {
		return err
	}
	if err = validateAdoptionControllers(c, plan); err != nil {
		return err
	}
	if err = validateAdoptionBindings(c, plan); err != nil {
		return err
	}
	if err = repo.validateAdoptionMachine(vm, plan); err != nil {
		return err
	}
	if plan.Profile.Hugepages {
		size, e := adoptedHugepageSize(c)
		if e != nil {
			return e
		}
		if size != plan.HugepageSizeKiB {
			return fmt.Errorf("existing hugepage policy changed after adoption was accepted")
		}
	}
	return nil
}

func (repo *VirtualMachineRepository) requireManagedHardware(id, node string) error {
	vm, err := repo.Get(id, node)
	if err != nil {
		return err
	}
	return repo.ValidateAppliedPolicy(vm)
}

var _ compute.BackendMachineRepository = (*VirtualMachineRepository)(nil)
var _ compute.MachineAutostartRepository = (*VirtualMachineRepository)(nil)

// Structural support is independent of current host resource availability.
func validateProfileCapabilities(p hardware.Profile, caps compute.VirtualMachineCapabilities) error {
	invalid := func(message string) error {
		return &compute.ValidationError{Field: "hardware_profile", Message: fmt.Sprintf("Hardware profile %s: %s", p.ID, message)}
	}
	if p.Firmware != "auto" && !slices.Contains(caps.Firmwares, p.Firmware) {
		return invalid("the selected firmware is unsupported on this host.")
	}
	if !slices.Contains(caps.DiskBuses, compute.NewDeviceBus(p.DiskBus)) {
		return invalid("the ordinary disk protocol is unsupported on this host.")
	}
	if p.ConfigDrive.IsEnabled() && !slices.Contains(caps.DiskBuses, compute.NewDeviceBus(p.ConfigDrive.Bus)) {
		return invalid("the config-drive bus is unsupported on this host.")
	}
	if !slices.Contains(caps.NICModels, p.NICModel) || !slices.Contains(caps.Graphics, compute.NewGraphicType(p.Graphics)) || !slices.Contains(caps.VideoModels, compute.NewVideoModel(p.VideoModel)) {
		return invalid("the NIC, graphics or video model is unsupported on this host.")
	}
	return nil
}

func validateAdoptionBindings(config *xmlmodel.Domain, plan hardware.Plan) error {
	if err := validateGuestSystemAdoption(config, plan.Profile); err != nil {
		return err
	}
	incompatible := func() error {
		return &compute.ValidationError{Field: "hardware_profile", Message: "Existing platform bindings changed after adoption was accepted; inspect the VM and resubmit adoption."}
	}
	if plan.Emulator != "" && (config.Devices == nil || config.Devices.Emulator != plan.Emulator) {
		return incompatible()
	}
	if plan.DomainType != "" && config.Type != plan.DomainType {
		return incompatible()
	}
	if plan.Machine != "" && (config.OS == nil || config.OS.Type == nil || config.OS.Type.Machine != plan.Machine) {
		return incompatible()
	}
	if plan.CPUThreads > 0 && (config.CPU == nil || config.CPU.Topology == nil || config.CPU.Topology.Threads != plan.CPUThreads) {
		return incompatible()
	}
	if plan.Profile.Hugepages {
		size, err := adoptedHugepageSize(config)
		if err != nil {
			return err
		}
		if size != plan.HugepageSizeKiB {
			return incompatible()
		}
	}
	return nil
}

func (repo *VirtualMachineRepository) ResolveInterfaceAttachment(vm *compute.VirtualMachine, nic *compute.VirtualMachineAttachedInterface) error {
	plan, err := hardware.DecodePlan(vm.AppliedPolicy)
	if err != nil {
		return &compute.AdoptionRequiredError{Message: "Explicitly adopt or re-adopt the VM before attaching a network interface."}
	}
	if nic.Model != "" && nic.Model != plan.Profile.NICModel {
		return &compute.ValidationError{Field: "network", Message: fmt.Sprintf("Network interface model %s conflicts with applied hardware profile %s (%s).", nic.Model, plan.Profile.ID, plan.Profile.NICModel)}
	}
	nic.Model = plan.Profile.NICModel
	return nil
}
