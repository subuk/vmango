package libvirt

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"

	vir "libvirt.org/go/libvirt"
	x "libvirt.org/go/libvirtxml"
)

func storagePolicy(p hardware.Profile, alias string) hardware.Disks {
	if alias == "configdrive" || alias == "ua-configdrive" {
		return p.ConfigDrivePolicy()
	}
	return p.DiskPolicy()
}

// Only initial dependency resolution creates controllers. Allocation never adds
// a controller, including when another compatible controller is full.
func ensureStorageControllers(c *x.Domain, p hardware.Profile) {
	if c.Devices == nil {
		c.Devices = &x.DomainDeviceList{}
	}
	policies := []hardware.Disks{p.DiskPolicy()}
	if p.ConfigDrive.IsEnabled() {
		policies = append(policies, p.ConfigDrivePolicy())
	}
	for _, d := range policies {
		if d.Bus == "virtio" {
			continue
		}
		found := false
		used := map[uint]bool{}
		for _, controller := range c.Devices.Controllers {
			if controller.Type != d.Bus {
				continue
			}
			used[valueOrZero(controller.Index)] = true
			if d.Bus != "scsi" || controller.Model == d.ControllerModel {
				found = true
			}
		}
		if found {
			continue
		}
		index := uint(0)
		for used[index] {
			index++
		}
		c.Devices.Controllers = append(c.Devices.Controllers, x.DomainController{Type: d.Bus, Model: d.ControllerModel, Index: uintPointer(index)})
	}
}

func allocateStorageAddress(c *x.Domain, d hardware.Disks, initial bool) (*x.DomainAddress, error) {
	if d.Bus == "virtio" {
		if initial {
			return nil, nil
		} // Exact-domain preflight checks initial PCI/MMIO placement.
		return allocateVirtioTransport(c)
	}
	geometry, err := hardware.Geometry(d)
	if err != nil {
		return nil, err
	}
	if c.Devices == nil {
		return nil, fmt.Errorf("no %s/%s controller exists", d.Bus, d.ControllerModel)
	}
	indices := []uint{}
	for _, controller := range c.Devices.Controllers {
		if controller.Type == d.Bus && (d.Bus != "scsi" || controller.Model == d.ControllerModel) {
			indices = append(indices, valueOrZero(controller.Index))
		}
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	used := map[string]bool{}
	for _, disk := range c.Devices.Disks {
		if disk.Target == nil || disk.Target.Bus != d.Bus {
			continue
		}
		if disk.Address == nil || disk.Address.Drive == nil {
			return nil, fmt.Errorf("disk %s has no explicit drive address; inspect and re-adopt before attaching", disk.Target.Dev)
		}
		key := driveAddressKey(disk.Address.Drive)
		if used[key] {
			return nil, fmt.Errorf("duplicate %s drive address %s", d.Bus, key)
		}
		used[key] = true
	}
	for _, index := range indices {
		// Prefer one LUN per target; then use additional LUNs only where supported.
		for unit := uint(0); unit < geometry.Units; unit++ {
			for channel := uint(0); channel < geometry.Channels; channel++ {
				for target := uint(0); target < geometry.Targets; target++ {
					if !geometry.Valid(channel, target, unit) {
						continue
					}
					a := &x.DomainAddressDrive{Controller: uintPointer(index), Bus: uintPointer(channel), Target: uintPointer(target), Unit: uintPointer(unit)}
					if !used[driveAddressKey(a)] {
						return &x.DomainAddress{Drive: a}, nil
					}
				}
			}
		}
	}
	if len(indices) == 0 {
		return nil, fmt.Errorf("no compatible %s/%s controller exists; attachment cannot create a controller", d.Bus, d.ControllerModel)
	}
	usage := 0
	for key := range used {
		for _, index := range indices {
			if strings.HasPrefix(key, fmt.Sprintf("%d/", index)) {
				usage++
			}
		}
	}
	return nil, fmt.Errorf("%s/%s controllers %v are full: supported capacity %d per controller, %d addresses in use; no additional controller will be created", d.Bus, d.ControllerModel, indices, geometry.Capacity(), usage)
}

func validateAdoptionControllers(c *x.Domain, plan hardware.Plan) error {
	if c.Devices == nil {
		return nil
	}
	controllers := map[string]x.DomainController{}
	for _, controller := range c.Devices.Controllers {
		key := fmt.Sprintf("%s/%d", controller.Type, valueOrZero(controller.Index))
		if _, exists := controllers[key]; exists {
			return fmt.Errorf("duplicate existing controller identifier %s", key)
		}
		controllers[key] = controller
	}
	targets, addresses := map[string]bool{}, map[string]bool{}
	for _, disk := range c.Devices.Disks {
		if disk.Target == nil {
			return fmt.Errorf("existing disk has no target; inspect before adoption")
		}
		if targets[disk.Target.Dev] {
			return fmt.Errorf("duplicate existing disk target %s", disk.Target.Dev)
		}
		targets[disk.Target.Dev] = true
		alias := ""
		if disk.Alias != nil {
			alias = disk.Alias.Name
		}
		if disk.Device != "disk" && alias != "ua-configdrive" {
			continue
		}
		d := storagePolicy(plan.Profile, alias)
		if disk.Target.Bus != d.Bus {
			return fmt.Errorf("disk %s bus %s conflicts with applied %s policy; no conversion is performed", disk.Target.Dev, disk.Target.Bus, d.Bus)
		}
		if d.Bus == "virtio" {
			continue
		}
		index := uint(0)
		if disk.Address != nil && disk.Address.Drive != nil {
			index = valueOrZero(disk.Address.Drive.Controller)
		} else if plan.Profile.Disks != nil {
			return fmt.Errorf("disk %s has no explicit controller address", disk.Target.Dev)
		}
		controller, ok := controllers[fmt.Sprintf("%s/%d", d.Bus, index)]
		if !ok || d.Bus == "scsi" && controller.Model != d.ControllerModel {
			return fmt.Errorf("disk %s requires compatible %s/%s controller %d; existing hardware is preserved", disk.Target.Dev, d.Bus, d.ControllerModel, index)
		}
		if disk.Address != nil && disk.Address.Drive != nil {
			a := disk.Address.Drive
			geometry, err := hardware.Geometry(d)
			if err != nil {
				return err
			}
			// Old virtio-scsi plans used LUN addresses. They remain valid, including
			// their old reserved-address convention; new allocation does not move them.
			if !geometry.Valid(valueOrZero(a.Bus), valueOrZero(a.Target), valueOrZero(a.Unit)) {
				return fmt.Errorf("disk %s address %s is outside supported %s/%s address space", disk.Target.Dev, driveAddressKey(a), d.Bus, d.ControllerModel)
			}
			key := d.Bus + "/" + driveAddressKey(a)
			if addresses[key] {
				return fmt.Errorf("duplicate existing disk address %s", key)
			}
			addresses[key] = true
		}
	}
	return nil
}

func observeStorage(c *x.Domain) hardware.StorageObservation {
	result := hardware.StorageObservation{Controllers: []hardware.StorageController{}, Disks: []hardware.StorageDisk{}}
	if c.Devices == nil {
		return result
	}
	models := map[string]string{}
	for _, ctl := range c.Devices.Controllers {
		if ctl.Type != "scsi" && ctl.Type != "sata" && ctl.Type != "ide" && ctl.Type != "pci" {
			continue
		}
		idx := valueOrZero(ctl.Index)
		result.Controllers = append(result.Controllers, hardware.StorageController{Type: ctl.Type, Model: ctl.Model, Index: idx})
		models[fmt.Sprintf("%s/%d", ctl.Type, idx)] = ctl.Model
	}
	for _, disk := range c.Devices.Disks {
		v := hardware.StorageDisk{Device: disk.Device}
		if disk.Target != nil {
			v.Target, v.Bus = disk.Target.Dev, disk.Target.Bus
		}
		if disk.Alias != nil {
			v.Alias = strings.TrimPrefix(disk.Alias.Name, "ua-")
		}
		if disk.Address != nil {
			if a := disk.Address.Drive; a != nil {
				v.Address = &hardware.DiskAddress{Type: "drive", Controller: valueOrZero(a.Controller), Bus: valueOrZero(a.Bus), Target: valueOrZero(a.Target), Unit: valueOrZero(a.Unit)}
				v.ControllerModel = models[fmt.Sprintf("%s/%d", v.Bus, v.Address.Controller)]
			}
			if a := disk.Address.PCI; a != nil {
				v.Address = &hardware.DiskAddress{Type: "pci", Bus: valueOrZero(a.Bus), Slot: valueOrZero(a.Slot), Function: valueOrZero(a.Function)}
			}
			if disk.Address.VirtioMMIO != nil {
				v.Address = &hardware.DiskAddress{Type: "virtio-mmio"}
			}
		}
		result.Disks = append(result.Disks, v)
	}
	return result
}

// Allocate only on an existing PCI bus. Never insert root ports/bridges or enable
// multifunction on a device owned by another policy. Unknown transports fail closed.
func allocateVirtioTransport(c *x.Domain) (*x.DomainAddress, error) {
	if c.QEMUCommandline != nil {
		for i, arg := range c.QEMUCommandline.Args {
			if arg.Value == "-device" && i+1 < len(c.QEMUCommandline.Args) && strings.Contains(c.QEMUCommandline.Args[i+1].Value, "-pci") {
				return nil, fmt.Errorf("virtio-blk transport capacity is unknown for a raw QEMU PCI device; existing device placement will not be changed")
			}
		}
	}
	raw, err := c.Marshal()
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	decoder := xml.NewDecoder(strings.NewReader(raw))
	sourceDepth, depth := 0, 0
	for {
		tok, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Local == "source" {
				sourceDepth = depth
			}
			if t.Name.Local != "address" || sourceDepth != 0 {
				continue
			}
			attrs := map[string]string{}
			for _, a := range t.Attr {
				attrs[a.Name.Local] = a.Value
			}
			if attrs["type"] != "pci" {
				continue
			}
			bus, _ := strconv.ParseUint(attrs["bus"], 0, 32)
			slot, _ := strconv.ParseUint(attrs["slot"], 0, 32)
			used[fmt.Sprintf("%d/%d", bus, slot)] = true
		case xml.EndElement:
			if depth == sourceDepth {
				sourceDepth = 0
			}
			depth--
		}
	}
	if c.Devices != nil {
		for _, ctl := range c.Devices.Controllers {
			if ctl.Type != "pci" {
				continue
			}
			bus := valueOrZero(ctl.Index)
			start, end := uint(1), uint(31)
			switch ctl.Model {
			case "pcie-root-port", "pcie-switch-downstream-port":
				start, end = 0, 0
			case "pci-root", "pci-bridge", "pcie-to-pci-bridge":
			case "pcie-root":
				start = 2 // chipset functions are never borrowed
			default:
				continue
			}
			for slot := start; slot <= end; slot++ {
				if !used[fmt.Sprintf("%d/%d", bus, slot)] {
					return &x.DomainAddress{PCI: &x.DomainAddressPCI{Domain: uintPointer(0), Bus: uintPointer(bus), Slot: uintPointer(slot), Function: uintPointer(0)}}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("virtio-blk transport capacity exhausted or unknown: no free slot on an existing compatible PCI/PCIe bus; PCI topology will not be rebuilt")
}

func storageProbeDisk(d *compute.VirtualMachineAttachedVolume, namer *DeviceNamer) *x.DomainDisk {
	copy := *d
	copy.Path = "/dev/null"
	return DomainDiskConfigFromVirtualMachineAttachedVolume(&copy, "raw", "file", namer)
}

func validateStorageNative(conn *vir.Connect, c *x.Domain) error {
	raw, err := c.Marshal()
	if err != nil {
		return err
	}
	_, err = conn.DomainXMLToNative("qemu-argv", raw, 0)
	if err != nil {
		return fmt.Errorf("storage topology is unsupported by this machine/emulator or lacks device capacity: %w", err)
	}
	return nil
}

func validateStorageProfile(conn *vir.Connect, p hardware.Profile, caps *x.DomainCaps) error {
	if p.Disks == nil {
		return nil
	}
	c := firmwareProbe(caps)
	applyDomainPowerPolicy(c)
	ensureStorageControllers(c, p)
	disks := []*compute.VirtualMachineAttachedVolume{{Alias: "root", DeviceBus: compute.NewDeviceBus(p.DiskBus), DeviceType: compute.DeviceTypeDisk}}
	if p.ConfigDrive.IsEnabled() {
		disks = append(disks, &compute.VirtualMachineAttachedVolume{Alias: "configdrive", DeviceBus: compute.NewDeviceBus(p.ConfigDrive.Bus), DeviceType: compute.NewDeviceType(p.ConfigDrive.DeviceType)})
	}
	namer := NewDeviceNamer()
	for _, disk := range disks {
		probe := storageProbeDisk(disk, namer)
		var err error
		probe.Address, err = allocateStorageAddress(c, storagePolicy(p, disk.Alias), true)
		if err != nil {
			return err
		}
		c.Devices.Disks = append(c.Devices.Disks, *probe)
	}
	return validateStorageNative(conn, c)
}

func (repo *VirtualMachineRepository) ValidateDiskAttachments(vm *compute.VirtualMachine, disks []*compute.VirtualMachineAttachedVolume) error {
	plan, err := hardware.DecodePlan(vm.AppliedPolicy)
	if err != nil {
		return err
	}
	if plan.Profile.Disks == nil {
		return nil
	} // Accepted older payloads keep their existing semantics.
	caps := &x.DomainCaps{Path: plan.Emulator, Arch: vm.Arch.String(), Machine: plan.Machine, Domain: plan.DomainType}
	c := newDomainConfig(vm, caps)
	c.Memory = &x.DomainMemory{Unit: "bytes", Value: uint(vm.Memory.Bytes())}
	c.VCPU = &x.DomainVCPU{Value: uint(vm.VCpus)}
	namer := NewDeviceNamer()
	for _, disk := range disks {
		d := storageProbeDisk(disk, namer)
		d.Address, err = allocateStorageAddress(c, storagePolicy(plan.Profile, disk.Alias), true)
		if err != nil {
			return err
		}
		c.Devices.Disks = append(c.Devices.Disks, *d)
	}
	// Include the other requested PCI consumers in the same native conversion.
	for _, nic := range vm.Interfaces {
		if !nic.Bridge && (nic.NetworkName == "qemu-usernet" || nic.NetworkName == "macos-socket-vmnet") {
			copy := *nic
			if err := repo.attachInterface(c, &copy); err != nil {
				return err
			}
			continue
		}
		c.Devices.Interfaces = append(c.Devices.Interfaces, x.DomainInterface{Model: &x.DomainInterfaceModel{Type: nic.Model}, Source: &x.DomainInterfaceSource{User: &x.DomainInterfaceSourceUser{}}})
	}
	conn, err := repo.pool.Acquire(vm.NodeId)
	if err != nil {
		return err
	}
	defer repo.pool.Release(vm.NodeId)
	if err = validateStorageNative(conn, c); err != nil {
		return err
	}
	observed := observeStorage(c)
	plan.Storage = &observed
	vm.AppliedPolicy, err = json.Marshal(plan)
	return err
}
