package compute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"subuk/vmango/tasks"
)

func VMRef(node, id string) tasks.ResourceRef {
	return tasks.ResourceRef{Kind: "machine", Scope: node, ID: id}
}
func VolumeRef(node, path string) tasks.ResourceRef {
	return tasks.ResourceRef{Kind: "volume", Scope: node, ID: path}
}

// InfrastructureCommand is a private, versioned desired command. Read models
// and command payloads are deliberately separate from the public task schema.
type InfrastructureCommand struct {
	CreationBindings map[string]json.RawMessage `json:",omitempty"`
	CloudID          string
	Catalog          *Cloud             `json:",omitempty"`
	CreateSpec       *MachineCreateSpec `json:",omitempty"`
	VolumeSpec       *CloudVolumeSpec   `json:",omitempty"`
	InstanceType     string
	StorageClass     string
	AcceptedScopes   map[string]string `json:",omitempty"`
	AcceptedKeys     []*Key            `json:",omitempty"`
	SelectedType     *InstanceType     `json:",omitempty"`
	Adoption         *MachineMetadata  `json:",omitempty"`
	NewDisk          *DataDiskSpec     `json:",omitempty"`
	Version          int
	Node             string
	VM               string
	Machine          *VirtualMachine `json:",omitempty"`
	Defaults         ImageDefaults
	Clones           []VirtualMachineManagerClonedVolumeParams
	NewVolumes       []VirtualMachineManagerCreatedVolumeParams
	Start            bool
	KeyIDs           []string
	DeleteVolumes    bool
	Disks            []*VirtualMachineAttachedVolume
	Disk             *VirtualMachineAttachedVolume    `json:",omitempty"`
	Interface        *VirtualMachineAttachedInterface `json:",omitempty"`
	InterfaceCount   int
	MAC              string
	Path             string
	CreateVolume     *VolumeCreateParams `json:",omitempty"`
	CloneVolume      *VolumeCloneParams  `json:",omitempty"`
	Size             Size
	Settings         VirtualMachineManagerNodeSettings
	Images           []Image                 `json:",omitempty"`
	ImageBindings    map[string]ImageBinding `json:",omitempty"`
	AutoImageSizes   []bool                  `json:",omitempty"`
}

func NewCommand(kind string, c InfrastructureCommand) tasks.Command {
	c.Version = 2
	if len(c.KeyIDs) == 0 {
		c.KeyIDs = nil
	}
	if c.Machine != nil && c.Machine.Config != nil {
		if len(c.Machine.Config.Userdata) == 0 {
			c.Machine.Config.Userdata = nil
		}
		if len(c.Machine.Config.Keys) == 0 {
			c.Machine.Config.Keys = nil
		}
	}

	payload, _ := json.Marshal(c)
	refs := []tasks.ResourceRef{}
	if c.CreateSpec != nil {
		refs = append(refs, tasks.ResourceRef{Kind: "machine-request", Scope: c.CloudID, ID: c.CreateSpec.Name})
	}
	if c.CloudID != "" && usesCloudCapacity(kind) {
		refs = append(refs, tasks.ResourceRef{Kind: "cloud", Scope: c.CloudID, ID: "capacity", ReadOnly: true})
	}
	if c.VM != "" && c.Node != "" {
		refs = append(refs, VMRef(c.Node, c.VM))
	}
	if c.Path != "" {
		refs = append(refs, VolumeRef(c.Node, c.Path))
	}
	if c.Disk != nil {
		refs = append(refs, VolumeRef(c.Node, c.Disk.Path))
	}
	return tasks.Command{Kind: kind, Payload: payload, Resources: refs}
}

type CommandHandler struct {
	Clouds  *CloudService
	Manager *VirtualMachineManager
	VMs     *VirtualMachineService
	Volumes *VolumeService
	Nodes   *NodeService
	Keys    *KeyService
	Scopes  map[string]string
}

func (h *CommandHandler) LockRefs(refs []tasks.ResourceRef) []tasks.ResourceRef {
	result := append([]tasks.ResourceRef(nil), refs...)
	for i := range result {
		if canonical := h.Scopes[result[i].Scope]; canonical != "" && (result[i].Kind == "machine" || result[i].Kind == "volume") {
			result[i].Scope = canonical
		}
	}
	return result
}

func (h *CommandHandler) destination(node, pool, name string) (tasks.ResourceRef, error) {
	path, err := h.Volumes.ResolvePath(node, pool, name)
	return VolumeRef(node, path), err
}

func (h *CommandHandler) Prepare(command tasks.Command) (tasks.Command, error) {
	var c InfrastructureCommand
	if err := json.Unmarshal(command.Payload, &c); err != nil || c.Version != 2 {
		return command, ErrInvalid
	}
	if h.Clouds != nil {
		var err error
		command, c, err = h.prepareCloud(command, c)
		if err != nil {
			return command, err
		}
		if c.CreateSpec != nil || c.VolumeSpec != nil || c.NewDisk != nil || command.Kind == "machine.adopt" || command.Kind == "machine.change-type" {
			return command, nil
		}
	}
	if c.Node == "" {
		return command, ErrInvalid
	}
	add := func(ref tasks.ResourceRef) { command.Resources = append(command.Resources, ref) }
	destination := func(pool, name string) error {
		ref, err := h.destination(c.Node, pool, name)
		if err == nil {
			add(ref)
		}
		return err
	}
	if command.Kind == "machine.create" {
		if c.Machine == nil || c.Machine.Id != c.VM || c.Machine.NodeId != c.Node {
			return command, ErrInvalid
		}
		if c.Machine.Arch == ArchUnknown && len(c.Clones) == 0 {
			node, err := h.Nodes.Get(c.Node, NodeGetOptions{NoPins: true})
			if err != nil {
				return command, err
			}
			c.Machine.Arch = node.CpuArch
		}
		if c.Machine.Config != nil {
			for _, id := range c.KeyIDs {
				key, err := h.Keys.Get(id)
				if err != nil {
					return command, err
				}
				c.Machine.Config.Keys = append(c.Machine.Config.Keys, key)
			}
		}
		images := h.Manager.images
		c.ImageBindings = map[string]ImageBinding{}
		if images != nil {
			seen := map[string]bool{}
			for _, clone := range c.Clones {
				if clone.ImageID == "" || seen[clone.ImageID] {
					continue
				}
				i, err := images.Get(clone.ImageID)
				if err != nil {
					return command, err
				}
				seen[i.ID] = true
				c.Images = append(c.Images, i)
				if i.Managed() {
					if images.ManagedSources == nil {
						return command, fmt.Errorf("managed image storage is unavailable")
					}
					b, err := images.ManagedSources.Bind(c.Node, i)
					if err != nil {
						return command, err
					}
					c.ImageBindings[i.ID] = b
				}
			}
		}
		for _, clone := range c.Clones {
			c.AutoImageSizes = append(c.AutoImageSizes, clone.ImageID != "" && clone.NewSize.Value == 0)
		}
		manager := h.Manager
		if len(c.Images) > 0 {
			manager = manager.withImages(images.Snapshot(c.Images, c.ImageBindings, true))
		}
		vm, clones, err := manager.PrepareCreation(c.Machine, c.Clones, c.NewVolumes, c.Defaults)
		if err != nil {
			return command, err
		}
		c.Machine, c.Clones, c.KeyIDs = vm, clones, nil
		// Shared-image followers stay queued rather than consuming workers
		// waiting for a transfer. Unrelated artifacts/nodes can still run.
		for _, binding := range c.ImageBindings {
			command.ExecutionGroups = append(command.ExecutionGroups, "image:"+artifactKey(binding))
		}
		c.Settings = h.Manager.settings[c.Node]
		for _, clone := range clones {
			ref := VolumeRef(c.Node, clone.OriginalPath)
			ref.ReadOnly = clone.ImageID != ""
			add(ref)
			if err := destination(clone.NewPool, clone.NewName); err != nil {
				return command, err
			}
		}
		for _, disk := range c.NewVolumes {
			if err := destination(disk.Pool, disk.Name); err != nil {
				return command, err
			}
		}
		for _, disk := range vm.Volumes {
			add(VolumeRef(c.Node, disk.Path))
		}
		if vm.Config != nil {
			if err := destination(c.Settings.CdPool, vm.Name+c.Settings.CdSuffix); err != nil {
				return command, err
			}
		}
	} else if strings.HasPrefix(command.Kind, "machine.") {
		vm, err := h.VMs.Get(c.VM, c.Node)
		if err != nil {
			return command, err
		}
		// Every VM command protects its complete current disk set. This also
		// excludes volume mutations while start, deletion or config changes run.
		for _, disk := range vm.Volumes {
			add(VolumeRef(c.Node, disk.Path))
		}
		switch command.Kind {
		case "machine.delete":
			c.Disks = vm.Volumes
		case "machine.start":
			if vm.IsRunning() {
				return command, ErrConflict
			}
		case "machine.reboot", "machine.poweroff":
			if !vm.IsRunning() {
				return command, ErrConflict
			}
		case "machine.attach-disk":
			if vm.State != StateStopped {
				return command, ErrConflict
			}
			if c.Disk == nil {
				return command, ErrInvalid
			}
			if err := ValidateVolumeAttachment(c.Disk); err != nil {
				return command, err
			}
			if err := h.Volumes.ValidateAttachable(c.Disk.Path, c.Node, h.Manager.images); err != nil {
				return command, err
			}
		case "machine.detach-disk":
			if vm.State != StateStopped {
				return command, ErrConflict
			}
			if vm.AttachmentInfo(c.Path) == nil {
				return command, ErrNotFound
			}
		case "machine.attach-interface":
			c.InterfaceCount = len(vm.Interfaces)
			if vm.State != StateStopped {
				return command, ErrConflict
			}
			if c.Interface == nil {
				return command, ErrInvalid
			}
			if err := ValidateInterface(c.Interface); err != nil {
				return command, err
			}
		case "machine.detach-interface":
			c.InterfaceCount = len(vm.Interfaces)
			if vm.State != StateStopped {
				return command, ErrConflict
			}
			found := false
			for _, iface := range vm.Interfaces {
				if strings.EqualFold(iface.Mac, c.MAC) {
					c.MAC = iface.Mac
					found = true
				}
			}
			if !found {
				return command, ErrInterfaceNotFound
			}
		default:
			return command, ErrInvalid
		}
	} else {
		switch command.Kind {
		case "volume.create":
			if c.CreateVolume == nil {
				return command, ErrInvalid
			}
			if err := validateVolumeCreate(*c.CreateVolume); err != nil {
				return command, err
			}
			if err := h.Volumes.ValidateCreate(*c.CreateVolume); err != nil {
				return command, err
			}
			if err := destination(c.CreateVolume.Pool, c.CreateVolume.Name); err != nil {
				return command, err
			}
		case "volume.clone":
			if c.CloneVolume == nil {
				return command, ErrInvalid
			}
			p := c.CloneVolume
			source, err := h.Volumes.Get(p.OriginalPath, c.Node)
			if err != nil {
				return command, err
			}
			if source.Metadata.Hidden {
				return command, ErrVolumeNotFound
			}
			if source.AttachedTo != "" {
				return command, ErrConflict
			}
			if p.NewSize.Value == 0 {
				p.NewSize = source.Size
			}
			if p.Format == VolumeFormatUnknown {
				p.Format = source.Format
			}
			create := VolumeCreateParams{NodeId: c.Node, Name: p.NewName, Pool: p.NewPool, Size: p.NewSize, Format: p.Format}
			if err := validateVolumeCreate(create); err != nil {
				return command, err
			}
			if p.NewSize.Bytes() < source.Size.Bytes() {
				return command, ErrInvalid
			}
			if err := h.Volumes.ValidateCreate(create); err != nil {
				return command, err
			}
			add(VolumeRef(c.Node, p.OriginalPath))
			if err := destination(p.NewPool, p.NewName); err != nil {
				return command, err
			}
		case "volume.resize", "volume.delete":
			v, err := h.Volumes.Get(c.Path, c.Node)
			if err != nil {
				return command, err
			}
			if v.Metadata.Hidden {
				return command, ErrVolumeNotFound
			}
			if v.Metadata.Protected || v.AttachedTo != "" {
				return command, ErrConflict
			}
			if command.Kind == "volume.resize" {
				b, err := sizeBytes(c.Size)
				if err != nil || b == 0 || b < v.Size.Bytes() {
					return command, ErrInvalid
				}
			}
		default:
			return command, ErrInvalid
		}
	}
	payload, err := json.Marshal(c)
	command.Payload = payload
	return command, err
}

func (h *CommandHandler) Execute(ctx context.Context, command tasks.Command, progress *tasks.Execution) error {
	var c InfrastructureCommand
	if err := json.Unmarshal(command.Payload, &c); err != nil || c.Version != 2 {
		return ErrInvalid
	}
	if h.Clouds != nil {
		if c.CreateSpec != nil || c.VolumeSpec != nil || c.NewDisk != nil || command.Kind == "machine.adopt" || command.Kind == "machine.change-type" {
			return h.executeCloud(ctx, command, c, progress)
		}
		if err := h.checkCloudMutation(command.Kind, &c); err != nil {
			return err
		}
	}
	if command.Kind == "machine.create" {
		if len(c.Images) > 0 {
			manager := h.Manager.withImages(h.Manager.images.Snapshot(c.Images, c.ImageBindings, false))
			manager.settings = map[string]VirtualMachineManagerNodeSettings{c.Node: c.Settings}
			for _, i := range c.Images {
				if i.Managed() {
					if manager.images.ManagedSources == nil {
						return fmt.Errorf("managed image storage unavailable")
					}
					if err := manager.images.ManagedSources.Ensure(ctx, c.ImageBindings[i.ID], i, progress); err != nil {
						return err
					}
				}
			}
			for n, auto := range c.AutoImageSizes {
				if auto {
					c.Clones[n].NewSize = Size{}
				}
			}
			var err error
			if err = progress.Stage("validate-image-creation", nil, false, func() error {
				c.Machine, c.Clones, err = manager.PrepareCreation(c.Machine, c.Clones, c.NewVolumes, c.Defaults)
				if err == nil && h.Clouds != nil {
					pools, poolErr := h.Clouds.Pools.List(VolumePoolListOptions{NodeIds: []string{c.Node}})
					if poolErr != nil {
						return poolErr
					}
					required := map[string]uint64{}
					for _, d := range c.Clones {
						if e := addStorage(required, d.NewPool, d.NewSize.Bytes()); e != nil {
							return e
						}
					}
					for _, d := range c.NewVolumes {
						if e := addStorage(required, d.Pool, d.Size.Bytes()); e != nil {
							return e
						}
					}
					if c.Machine.Config != nil {
						if e := addStorage(required, c.Settings.CdPool, 8<<20); e != nil {
							return e
						}
					}
					return storageFits(HostInventory{Pools: pools}, required)
				}
				return err
			}); err != nil {
				return err
			}
			return manager.createPrepared(c.Machine, c.Clones, c.NewVolumes, c.Start, c.Settings, progress)
		}
		return h.Manager.createPrepared(c.Machine, c.Clones, c.NewVolumes, c.Start, c.Settings, progress)
	}
	ref := VMRef(c.Node, c.VM)
	if strings.HasPrefix(command.Kind, "volume.") {
		ref = VolumeRef(c.Node, c.Path)
	}
	stage := func(name string, run func() error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return progress.Stage(name, &ref, true, run)
	}
	effect := func(r tasks.ResourceRef, action string) error { return progress.Effect(r, action) }
	checkVM := func(check func(*VirtualMachine) bool) error {
		return stage("verify-machine", func() error {
			vm, err := h.VMs.Get(c.VM, c.Node)
			if err != nil {
				return err
			}
			if !check(vm) {
				return fmt.Errorf("requested machine change is not confirmed by the backend")
			}
			return nil
		})
	}
	switch command.Kind {
	case "machine.delete":
		if err := stage("delete-machine", func() error {
			if err := h.VMs.Delete(c.VM, c.Node); err != nil {
				return err
			}
			return effect(ref, "deleted")
		}); err != nil {
			return err
		}
		if err := stage("verify-machine-deleted", func() error {
			_, err := h.VMs.Get(c.VM, c.Node)
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("machine still exists")
		}); err != nil {
			return err
		}
		if c.DeleteVolumes {
			for _, disk := range c.Disks {
				if h.Clouds != nil {
					volume, err := h.Volumes.Get(disk.Path, c.Node)
					if err != nil {
						return err
					}
					if !h.Volumes.Owned(volume) {
						continue
					}
				}
				diskRef := VolumeRef(c.Node, disk.Path)
				if err := progress.Stage("delete-attached-volume", &diskRef, true, func() error {
					if err := h.Volumes.Delete(disk.Path, c.Node); err != nil {
						return err
					}
					return effect(diskRef, "deleted")
				}); err != nil {
					return err
				}
				if err := h.verifyDeleted(progress, diskRef, c.Node, disk.Path); err != nil {
					return err
				}
			}
		}
		return nil
	case "machine.start", "machine.reboot", "machine.poweroff":
		action := strings.TrimPrefix(command.Kind, "machine.")
		if err := stage(action+"-machine", func() error {
			if err := h.VMs.Action(c.VM, c.Node, action); err != nil {
				return err
			}
			return effect(ref, action+"-request-completed")
		}); err != nil {
			return err
		}
		if action == "poweroff" {
			return checkVM(func(vm *VirtualMachine) bool { return vm.State == StateStopped })
		}
		return nil
	case "machine.attach-disk":
		if err := stage("attach-disk", func() error {
			if err := h.Manager.AttachVolume(c.VM, c.Node, c.Disk); err != nil {
				return err
			}
			return effect(VolumeRef(c.Node, c.Disk.Path), "attached")
		}); err != nil {
			return err
		}
		return checkVM(func(vm *VirtualMachine) bool { return vm.AttachmentInfo(c.Disk.Path) != nil })
	case "machine.detach-disk":
		if err := stage("detach-disk", func() error {
			if err := h.VMs.DetachVolume(c.VM, c.Node, c.Path); err != nil {
				return err
			}
			return effect(VolumeRef(c.Node, c.Path), "detached")
		}); err != nil {
			return err
		}
		return checkVM(func(vm *VirtualMachine) bool { return vm.AttachmentInfo(c.Path) == nil })
	case "machine.attach-interface", "machine.detach-interface":
		attach := command.Kind == "machine.attach-interface"
		if err := stage(strings.TrimPrefix(command.Kind, "machine."), func() error {
			var err error
			if attach {
				err = h.VMs.AttachInterface(c.VM, c.Node, c.Interface)
			} else {
				err = h.VMs.DetachInterface(c.VM, c.Node, c.MAC)
			}
			if err != nil {
				return err
			}
			return effect(ref, strings.TrimPrefix(command.Kind, "machine."))
		}); err != nil {
			return err
		}
		return checkVM(func(vm *VirtualMachine) bool {
			found := false
			for _, iface := range vm.Interfaces {
				if attach {
					found = found || (iface.NetworkName == c.Interface.NetworkName && (c.Interface.Mac == "" || strings.EqualFold(iface.Mac, c.Interface.Mac)) && iface.Model == c.Interface.Model && iface.AccessVlan == c.Interface.AccessVlan)
				} else {
					found = found || strings.EqualFold(iface.Mac, c.MAC)
				}
			}
			expectedCount := c.InterfaceCount - 1
			if attach {
				expectedCount = c.InterfaceCount + 1
			}
			return found == attach && len(vm.Interfaces) == expectedCount
		})
	case "volume.create", "volume.clone":
		var volume *Volume
		if err := stage(strings.ReplaceAll(command.Kind, ".", "-"), func() error {
			var err error
			if c.CreateVolume != nil {
				volume, err = h.Volumes.Create(*c.CreateVolume)
			} else {
				volume, err = h.Volumes.Clone(*c.CloneVolume)
			}
			if err != nil {
				return err
			}
			return effect(VolumeRef(c.Node, volume.Path), "created")
		}); err != nil {
			return err
		}
		expected := volume.Size.Bytes()
		if c.CreateVolume != nil {
			expected = c.CreateVolume.Size.Bytes()
		} else {
			expected = c.CloneVolume.NewSize.Bytes()
		}
		return h.verifyVolume(progress, c.Node, volume.Path, expected)
	case "volume.resize":
		if err := stage("resize-volume", func() error {
			if err := h.Volumes.Resize(c.Path, c.Node, c.Size); err != nil {
				return err
			}
			return effect(ref, "resized")
		}); err != nil {
			return err
		}
		return h.verifyVolume(progress, c.Node, c.Path, c.Size.Bytes())
	case "volume.delete":
		if err := stage("delete-volume", func() error {
			if err := h.Volumes.Delete(c.Path, c.Node); err != nil {
				return err
			}
			return effect(ref, "deleted")
		}); err != nil {
			return err
		}
		return h.verifyDeleted(progress, ref, c.Node, c.Path)
	}
	return ErrInvalid
}

func (h *CommandHandler) verifyVolume(e *tasks.Execution, node, path string, size uint64) error {
	ref := VolumeRef(node, path)
	return e.Stage("verify-volume", &ref, true, func() error {
		v, err := h.Volumes.Get(path, node)
		if err != nil {
			return err
		}
		if v.Size.Bytes() < size {
			return fmt.Errorf("requested volume size is not confirmed")
		}
		return nil
	})
}
func (h *CommandHandler) verifyDeleted(e *tasks.Execution, ref tasks.ResourceRef, node, path string) error {
	return e.Stage("verify-volume-deleted", &ref, true, func() error {
		_, err := h.Volumes.Get(path, node)
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrVolumeNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("volume still exists")
	})
}
