package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"subuk/vmango/tasks"
)

type CloudVolumeSpec struct {
	Name         string `json:"name"`
	Zone         string `json:"zone"`
	StorageClass string `json:"storage_class"`
	SizeBytes    uint64 `json:"size_bytes"`
}

func (h *CommandHandler) prepareCloud(command tasks.Command, c InfrastructureCommand) (tasks.Command, InfrastructureCommand, error) {
	var cloud Cloud
	var err error
	if c.CloudID != "" {
		cloud, err = h.Clouds.Get(c.CloudID)
	} else {
		cloud, _, err = h.Clouds.ForNode(c.Node)
	}
	if err != nil {
		return command, c, err
	}
	if c.Catalog == nil {
		c.Catalog = &cloud
	}
	cloud = *c.Catalog
	if c.Node != "" {
		if _, ok := cloud.Host(c.Node); !ok {
			return command, c, ErrUnknownNode
		}
	}
	c.CloudID = cloud.ID
	if usesCloudCapacity(command.Kind) {
		command.ExecutionGroups = append(command.ExecutionGroups, "cloud:"+cloud.ID)
		command.Resources = append(command.Resources, tasks.ResourceRef{Kind: "cloud", Scope: cloud.ID, ID: "capacity", ReadOnly: true})
	}
	if c.AcceptedScopes == nil {
		c.AcceptedScopes = map[string]string{}
		for _, host := range cloud.Hosts {
			c.AcceptedScopes[host.Node] = h.Scopes[host.Node]
		}
	}
	if c.CreateSpec != nil {
		if command.Kind != "machine.create" {
			return command, c, ErrInvalid
		}
		if err = h.Clouds.ValidateCreate(cloud, *c.CreateSpec); err != nil {
			return command, c, err
		}
		image, err := h.Manager.images.Get(c.CreateSpec.Image)
		if err != nil {
			return command, c, err
		}
		c.Images = []Image{image}
		c.CreationBindings = map[string]json.RawMessage{}
		for _, host := range cloud.Hosts {
			binding, err := h.VMs.SnapshotCreationBinding(host.Node)
			if err != nil {
				return command, c, err
			}
			c.CreationBindings[host.Node] = binding
		}
		for _, id := range c.CreateSpec.KeyIDs {
			key, err := h.Keys.Get(id)
			if err != nil {
				return command, c, err
			}
			c.AcceptedKeys = append(c.AcceptedKeys, key)
		}
		c.CreateSpec.KeyIDs = nil
		command.Resources = append(command.Resources, tasks.ResourceRef{Kind: "machine-request", Scope: cloud.ID, ID: c.CreateSpec.ID})
		for _, disk := range c.CreateSpec.ExistingDisks {
			command.Resources = append(command.Resources, VolumeRef(disk.Host, disk.Path))
		}
	} else if c.VolumeSpec != nil {
		v := c.VolumeSpec
		if command.Kind != "volume.create" || !resourceName.MatchString(v.Name) || v.SizeBytes == 0 || !hasEntry(cloud.Zones, v.Zone) || !hasEntry(cloud.StorageClasses, v.StorageClass) {
			return command, c, ErrInvalid
		}
	} else if c.NewDisk != nil {
		if command.Kind != "machine.create-disk" || !resourceName.MatchString(c.NewDisk.Name) || c.NewDisk.SizeBytes == 0 {
			return command, c, ErrInvalid
		}
		if err = h.VMs.RequireManaged(c.VM, c.Node); err != nil {
			return command, c, err
		}
		vm, err := h.VMs.Get(c.VM, c.Node)
		if err != nil {
			return command, c, err
		}
		if vm.State != StateStopped {
			return command, c, ErrConflict
		}
		if _, ok := mustHost(cloud, c.Node).Storage[c.NewDisk.StorageClass]; !ok {
			return command, c, invalid("storage_class", "Storage class is unavailable on this host.")
		}
		for _, disk := range vm.Volumes {
			command.Resources = append(command.Resources, VolumeRef(c.Node, disk.Path))
		}
	} else if command.Kind == "machine.adopt" || command.Kind == "machine.change-type" {
		vm, err := h.VMs.Get(c.VM, c.Node)
		if err != nil {
			return command, c, err
		}
		host, ok := cloud.Host(c.Node)
		if !ok {
			return command, c, ErrInvalid
		}
		if command.Kind == "machine.adopt" {
			if vm.Metadata != nil {
				return command, c, fmt.Errorf("%w: machine is already managed", ErrConflict)
			}
			if _, ok := h.VMs.VirtualMachineRepository.(MachineAdoptionRepository); !ok {
				return command, c, ErrUnavailable
			}
			c.Adoption = &MachineMetadata{Cloud: cloud.ID, Zone: host.Zone, CPURequest: vm.VCpus, MemoryRequestBytes: vm.Memory.Bytes()}
		} else {
			if vm.Metadata == nil || vm.State != StateStopped {
				return command, c, fmt.Errorf("%w: type changes require a stopped managed machine", ErrConflict)
			}
			t, err := cloud.InstanceType(c.InstanceType)
			if err != nil {
				return command, c, err
			}
			c.SelectedType = &t
		}
		for _, d := range vm.Volumes {
			command.Resources = append(command.Resources, VolumeRef(c.Node, d.Path))
		}
	} else {
		if command.Kind == "machine.create" && (c.Machine == nil || c.Machine.Metadata == nil) {
			return command, c, invalid("image", "Create machines through the cloud catalog.")
		}
		if err = h.checkCloudMutation(command.Kind, &c); err != nil {
			return command, c, err
		}
		if command.Kind == "volume.create" {
			b, ok := mustHost(cloud, c.Node).Storage[c.StorageClass]
			if !ok {
				return command, c, invalid("storage_class", "Storage class is unavailable on this host.")
			}
			if c.CreateVolume == nil {
				return command, c, ErrInvalid
			}
			c.CreateVolume.Pool = b.Pool
			c.CreateVolume.Format = b.Format
			c.CreateVolume.Cloud = cloud.ID
			c.CreateVolume.StorageClass = c.StorageClass
		}
		if command.Kind == "volume.clone" {
			b, ok := mustHost(cloud, c.Node).Storage[c.StorageClass]
			if !ok {
				return command, c, invalid("storage_class", "Storage class is unavailable on this host.")
			}
			if c.CloneVolume == nil {
				return command, c, ErrInvalid
			}
			c.CloneVolume.NewPool = b.Pool
			c.CloneVolume.Format = b.Format
			c.CloneVolume.Cloud = cloud.ID
			c.CloneVolume.StorageClass = c.StorageClass
		}
		if command.Kind == "machine.attach-interface" {
			if c.Interface == nil {
				return command, c, ErrInvalid
			}
			b, ok := mustHost(cloud, c.Node).Networks[c.Interface.NetworkName]
			if !ok {
				return command, c, invalid("network", "Logical network is unavailable on this host.")
			}
			model := b.Model
			if model == "" {
				vm, err := h.VMs.Get(c.VM, c.Node)
				if err != nil {
					return command, c, err
				}
				if len(vm.Interfaces) > 0 {
					model = vm.Interfaces[0].Model
				}
			}
			c.Interface = &VirtualMachineAttachedInterface{LogicalNetwork: c.Interface.NetworkName, NetworkName: b.Network, Bridge: b.Bridge, Model: model, AccessVlan: b.VLAN, Mac: c.Interface.Mac}
		}
	}
	command.Payload, err = json.Marshal(c)
	return command, c, err
}
func mustHost(c Cloud, node string) CloudHost { h, _ := c.Host(node); return h }
func (h *CommandHandler) checkCloudMutation(kind string, c *InfrastructureCommand) error {
	if c.CloudID == "" {
		return invalid("cloud", "This queued command predates the cloud catalog. Inspect its task and submit a new cloud command.")
	}
	if kind == "machine.create" && (c.Machine == nil || c.Machine.Metadata == nil) {
		return invalid("image", "Create machines through the cloud catalog.")
	}

	for node, scope := range c.AcceptedScopes {
		if h.Scopes[node] != scope {
			return fmt.Errorf("accepted cloud host connection changed; inspect the task and submit again")
		}
	}
	if c.Node == "" {
		return nil
	}
	if kind != "machine.create" && len(kind) > 8 && kind[:8] == "machine." {
		if err := h.VMs.RequireManaged(c.VM, c.Node); err != nil {
			return err
		}
	}
	if kind == "volume.create" || kind == "volume.clone" {
		var pool string
		var bytes uint64
		if c.CreateVolume != nil {
			pool = c.CreateVolume.Pool
			bytes = c.CreateVolume.Size.Bytes()
		}
		if c.CloneVolume != nil {
			pool = c.CloneVolume.NewPool
			bytes = c.CloneVolume.NewSize.Bytes()
			if bytes == 0 {
				v, err := h.Volumes.Get(c.CloneVolume.OriginalPath, c.Node)
				if err != nil {
					return err
				}
				bytes = v.Size.Bytes()
			}
		}
		if pool != "" {
			pools, err := h.Clouds.Pools.List(VolumePoolListOptions{NodeIds: []string{c.Node}})
			if err != nil {
				return err
			}
			if err = storageFits(HostInventory{Pools: pools}, map[string]uint64{pool: bytes}); err != nil {
				return err
			}
		}
	}
	if kind == "volume.resize" || kind == "volume.delete" {
		v, err := h.Volumes.Get(c.Path, c.Node)
		if err != nil {
			return err
		}
		if v.Metadata.Hidden {
			return ErrVolumeNotFound
		}
		if !h.Volumes.Owned(v) {
			return fmt.Errorf("%w: volume ownership is not confirmed", ErrConflict)
		}
		if kind == "volume.resize" {
			pools, err := h.Clouds.Pools.List(VolumePoolListOptions{NodeIds: []string{c.Node}})
			if err != nil {
				return err
			}
			if c.Size.Bytes() > v.Size.Bytes() {
				return storageFits(HostInventory{Pools: pools}, map[string]uint64{v.Pool: c.Size.Bytes() - v.Size.Bytes()})
			}
		}
	}
	return nil
}

func (h *CommandHandler) executeCloud(ctx context.Context, command tasks.Command, c InfrastructureCommand, p *tasks.Execution) error {
	for node, scope := range c.AcceptedScopes {
		if h.Scopes[node] != scope {
			return fmt.Errorf("accepted host connection changed")
		}
	}
	if c.Catalog == nil {
		return ErrInvalid
	}
	cloud := *c.Catalog
	var inventory []HostInventory
	if err := p.Stage("cloud-inventory", nil, false, func() error { var err error; inventory, err = h.Clouds.Inventory(cloud); return err }); err != nil {
		return err
	}
	if c.CreateSpec != nil {
		for _, i := range inventory {
			for _, vm := range i.Machines {
				if vm.Id == c.CreateSpec.ID {
					return fmt.Errorf("%w: machine name already exists in cloud", ErrConflict)
				}
			}
		}
		images := h.Manager.images.Snapshot(c.Images, nil, true)
		var resolved InfrastructureCommand
		if err := p.Stage("schedule-machine", nil, false, func() error {
			var err error
			resolved, err = h.Clouds.ResolveCreate(cloud, *c.CreateSpec, images, inventory, c.CreationBindings)
			return err
		}); err != nil {
			return err
		}
		resolved.CloudID = cloud.ID
		resolved.Catalog = &cloud
		resolved.AcceptedScopes = c.AcceptedScopes
		if resolved.Machine.Config != nil {
			resolved.Machine.Config.Keys = c.AcceptedKeys
		}
		local := *h
		local.Manager = h.Manager.withImages(images)
		prepared, err := local.Prepare(NewCommand("machine.create", resolved))
		if err != nil {
			return err
		}
		if err = p.Protect(prepared.Resources); err != nil {
			return err
		}
		return local.Execute(ctx, prepared, p)
	}
	if c.VolumeSpec != nil {
		v := c.VolumeSpec
		var selected *CloudHost
		for _, i := range inventory {
			if i.Host.Zone != v.Zone {
				continue
			}
			b, ok := i.Host.Storage[v.StorageClass]
			if !ok {
				continue
			}
			if storageFits(i, map[string]uint64{b.Pool: v.SizeBytes}) != nil {
				continue
			}
			if selected == nil || i.Host.Node < selected.Node {
				host := i.Host
				selected = &host
			}
		}
		if selected == nil {
			return invalid("storage_class", "No host has the selected storage class and sufficient capacity.")
		}
		b := selected.Storage[v.StorageClass]
		resolved := InfrastructureCommand{CloudID: cloud.ID, Catalog: &cloud, AcceptedScopes: c.AcceptedScopes, Node: selected.Node, StorageClass: v.StorageClass, CreateVolume: &VolumeCreateParams{NodeId: selected.Node, Name: v.Name, Pool: b.Pool, Size: NewSize(v.SizeBytes, SizeUnitB), Format: b.Format, Cloud: cloud.ID, StorageClass: v.StorageClass}}
		prepared, err := h.Prepare(NewCommand("volume.create", resolved))
		if err != nil {
			return err
		}
		if err = p.Protect(prepared.Resources); err != nil {
			return err
		}
		return h.Execute(ctx, prepared, p)
	}
	ref := VMRef(c.Node, c.VM)
	vm, err := h.VMs.Get(c.VM, c.Node)
	if err != nil {
		return err
	}
	if c.NewDisk != nil {
		if vm.Metadata == nil || vm.State != StateStopped {
			return ErrConflict
		}
		d := c.NewDisk
		b, ok := mustHost(cloud, c.Node).Storage[d.StorageClass]
		if !ok {
			return ErrInvalid
		}
		for _, i := range inventory {
			if i.Host.Node == c.Node {
				if err = storageFits(i, map[string]uint64{b.Pool: d.SizeBytes}); err != nil {
					return err
				}
			}
		}
		params := VolumeCreateParams{NodeId: c.Node, Name: c.VM + "_" + d.Name, Pool: b.Pool, Format: b.Format, Size: NewSize(d.SizeBytes, SizeUnitB), Cloud: cloud.ID, StorageClass: d.StorageClass}
		path, err := h.Volumes.ResolvePath(c.Node, b.Pool, params.Name)
		if err != nil {
			return err
		}
		if err = p.Protect([]tasks.ResourceRef{VolumeRef(c.Node, path), ref}); err != nil {
			return err
		}
		var volume *Volume
		if err = p.Stage("create-volume", &ref, true, func() error {
			var err error
			volume, err = h.Volumes.Create(params)
			if err != nil {
				return err
			}
			return p.Effect(VolumeRef(c.Node, volume.Path), "created")
		}); err != nil {
			return err
		}
		bus := DeviceBusVirtio
		for _, disk := range vm.Volumes {
			if disk.DeviceType == DeviceTypeDisk {
				bus = disk.DeviceBus
				break
			}
		}
		return p.Stage("attach-disk", &ref, true, func() error {
			if err := h.Manager.AttachVolume(c.VM, c.Node, &VirtualMachineAttachedVolume{Path: volume.Path, Alias: d.Name, DeviceType: DeviceTypeDisk, DeviceBus: bus}); err != nil {
				return err
			}
			return p.Effect(VolumeRef(c.Node, volume.Path), "attached")
		})
	}
	if command.Kind == "machine.adopt" {
		if vm.Metadata != nil {
			return fmt.Errorf("%w: already managed", ErrConflict)
		}
		m := c.Adoption
		if m == nil {
			return ErrInvalid
		}
		m.CPURequest = vm.VCpus
		m.MemoryRequestBytes = vm.Memory.Bytes()
		backend, ok := h.VMs.VirtualMachineRepository.(MachineAdoptionRepository)
		if !ok {
			return ErrUnavailable
		}
		return p.Stage("adopt-machine", &ref, true, func() error {
			if err := backend.Adopt(c.VM, c.Node, m); err != nil {
				return err
			}
			return p.Effect(ref, "adopted")
		})
	}
	if c.SelectedType == nil || vm.Metadata == nil || vm.State != StateStopped {
		return fmt.Errorf("%w: type change requires a stopped managed machine", ErrConflict)
	}
	t := *c.SelectedType
	if err = p.Stage("admit-instance-type", &ref, false, func() error {
		if vm.Cpupin != nil && t.VCPUs != vm.VCpus {
			return invalid("instance_type", "Changing the vCPU count with explicit CPU pinning is unsupported; keep a compatible instance type.")
		}
		if t.MemoryBytes < vm.Metadata.MinimumMemoryBytes {
			return invalid("instance_type", "Memory is below the applied image minimum.")
		}
		found := false
		for _, i := range inventory {
			if i.Host.Node == c.Node {
				found = true
				if _, err := Fit(i, t, vm.Id); err != nil {
					return err
				}
				if t.Hugepages && !i.Node.Has2mPages() && !i.Node.Has1gPages() {
					return invalid("instance_type", "Hugepages are unavailable.")
				}
			}
		}
		if !found {
			return ErrUnknownNode
		}
		caps, err := h.VMs.Capabilities(c.Node, vm.Arch)
		if err != nil {
			return err
		}
		if caps.MaxVCPUs > 0 && uint(t.VCPUs) > caps.MaxVCPUs {
			return invalid("instance_type", "Too many vCPUs for the current host.")
		}
		return nil
	}); err != nil {
		return err
	}
	return p.Stage("change-instance-type", &ref, true, func() error {
		current, err := h.VMs.Get(c.VM, c.Node)
		if err != nil {
			return err
		}
		if current.State != StateStopped || current.Metadata == nil {
			return ErrConflict
		}
		current.VCpus = t.VCPUs
		current.Memory = NewSize(t.MemoryBytes, SizeUnitB)
		current.Hugepages = t.Hugepages
		current.Autostart = t.Autostart
		metadata := *current.Metadata
		metadata.InstanceType = t.ID
		metadata.CPURequest = t.CPURequest
		metadata.MemoryRequestBytes = t.MemoryBytes
		current.Metadata = &metadata
		if err = h.VMs.SaveResources(current); err != nil {
			return err
		}
		if err = p.Effect(ref, "instance-type-changed"); err != nil {
			return err
		}
		actual, err := h.VMs.Get(c.VM, c.Node)
		if err != nil {
			return err
		}
		if actual.Autostart != t.Autostart || actual.VCpus != t.VCPUs || actual.Memory.Bytes() != t.MemoryBytes || actual.Metadata == nil || actual.Metadata.CPURequest != t.CPURequest || actual.Metadata.InstanceType != t.ID {
			return fmt.Errorf("instance type change was not confirmed")
		}
		return nil
	})
}

// usesCloudCapacity includes allocation changes and storage mutations, whose
// uncertain outcomes must fence further admission. Power operations only change
// runtime state: stopped and unmanaged machines already count in inventory.
func usesCloudCapacity(kind string) bool {
	switch kind {
	case "machine.create", "machine.delete", "machine.adopt", "machine.change-type", "machine.create-disk",
		"volume.create", "volume.clone", "volume.resize", "volume.delete":
		return true
	default:
		return false
	}
}
