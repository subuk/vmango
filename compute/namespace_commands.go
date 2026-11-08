package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"subuk/vmango/tasks"
)

func (h *CommandHandler) ValidateContext(command tasks.Command) error {
	if h.Workloads == nil {
		return nil
	}
	var c InfrastructureCommand
	if json.Unmarshal(command.Payload, &c) != nil || c.NamespaceID == "" || string(c.NamespaceID) != command.NamespaceID || c.CloudID != command.CloudID {
		return invalid("namespace", "command requires reliable namespace and cloud context; inspect legacy tasks and resubmit")
	}
	return h.Workloads.Namespaces.Admit(c.NamespaceID, c.CloudID)
}
func (h *CommandHandler) Prepare(command tasks.Command) (tasks.Command, error) {
	if err := h.ValidateContext(command); err != nil {
		return command, err
	}
	if h.Workloads == nil {
		return h.prepare(command)
	}
	var c InfrastructureCommand
	if err := json.Unmarshal(command.Payload, &c); err != nil {
		return command, err
	}
	wc := WorkloadContext{Namespace: c.NamespaceID, Cloud: c.CloudID}
	if command.Kind == "machine.assign" || command.Kind == "volume.assign" {
		ids := []string{}
		disks := []LocalDiskRef{}
		if c.VM != "" {
			ids = append(ids, c.VM)
		}
		if c.Path != "" {
			disks = append(disks, LocalDiskRef{Host: c.Node, Path: c.Path})
		}
		p, err := h.Workloads.PreviewAssignment(wc, ids, disks)
		if err != nil {
			return command, err
		}
		c.Assignment = &p
		for _, vm := range p.Machines {
			command.Resources = append(command.Resources, VMRef(vm.NodeId, vm.Id))
		}
		for _, v := range p.Volumes {
			command.Resources = append(command.Resources, VolumeRef(v.NodeId, v.Path))
		}
		command.Payload, _ = json.Marshal(c)
		return command, nil
	}
	// Snapshot logical labels independently of generated backend names. Generated
	// values are accepted once and never regenerated during execution/recovery.
	if c.Label == "" {
		switch {
		case c.CreateSpec != nil:
			c.Label = c.CreateSpec.Name
			c.CreateSpec.Name = "vm-" + uuid.NewString()
		case c.VolumeSpec != nil:
			c.Label = c.VolumeSpec.Name
			c.VolumeSpec.Name = "vol-" + uuid.NewString()
		case c.CreateVolume != nil:
			c.Label = c.CreateVolume.Name
			c.CreateVolume.Name = "vol-" + uuid.NewString()
		case c.CloneVolume != nil:
			c.Label = c.CloneVolume.NewName
			c.CloneVolume.NewName = "vol-" + uuid.NewString()
		}
	}
	if c.Label != "" && !ValidResourceLabel(c.Label) {
		return command, invalid("name", "invalid display label")
	}
	if c.CreateSpec != nil {
		hostname, err := GuestHostname(c.CreateSpec.Hostname, c.Label, c.CreateSpec.Name)
		if err != nil {
			return command, err
		}
		c.CreateSpec.Hostname = hostname
		if err := h.Workloads.CheckMachineLabel(wc, c.Label, ""); err != nil {
			return command, err
		}
	}
	local := *h
	var err error
	if h.Keys != nil {
		local.Keys, err = h.Keys.ForNamespace(c.NamespaceID)
		if err != nil {
			return command, err
		}
	}
	if h.CloudInitTemplates != nil {
		local.CloudInitTemplates, err = h.CloudInitTemplates.ForNamespace(c.NamespaceID)
		if err != nil {
			return command, err
		}
	}
	if err := h.validateWorkloadCommand(command.Kind, &c, false); err != nil {
		return command, err
	}
	command.Payload, _ = json.Marshal(c)
	prepared, err := local.prepare(command)
	if err != nil {
		return prepared, err
	}
	if err = json.Unmarshal(prepared.Payload, &c); err != nil {
		return prepared, err
	}
	if c.CreateVolume != nil {
		c.CreateVolume.NamespaceID, c.CreateVolume.Label = c.NamespaceID, c.Label
	}
	if c.CloneVolume != nil {
		c.CloneVolume.NamespaceID, c.CloneVolume.Label = c.NamespaceID, c.Label
	}
	if c.Machine != nil {
		c.Machine.NamespaceID, c.Machine.Label = c.NamespaceID, c.Label
	}
	if err = h.validateWorkloadCommand(command.Kind, &c, false); err != nil {
		return prepared, err
	}
	// Only logical name reservations acquire namespace scope; physical locks keep
	// the exact same backend identities across all workspaces.
	for i, r := range prepared.Resources {
		if r.Kind == "machine-request" {
			prepared.Resources[i].Scope = string(c.NamespaceID) + "\x00" + c.CloudID
			prepared.Resources[i].ID = c.Label
		}
	}
	prepared.NamespaceID, prepared.CloudID = string(c.NamespaceID), c.CloudID
	prepared.Payload, err = json.Marshal(c)
	return prepared, err
}
func (h *CommandHandler) validateWorkloadCommand(kind string, c *InfrastructureCommand, executing bool) error {
	s := h.Workloads
	wc := WorkloadContext{Namespace: c.NamespaceID, Cloud: c.CloudID}
	if err := s.Namespaces.Admit(c.NamespaceID, c.CloudID); err != nil {
		return err
	}
	unassigned := kind == "machine.adopt"
	before := c.AcceptedVolumes
	if !executing {
		c.AcceptedVolumes = map[string]string{}
	}
	checkVolume := func(node, path string) error {
		v, err := s.Clouds.Volumes.Get(path, node)
		if err != nil {
			return err
		}
		if err = s.CheckVolume(wc, v, unassigned); err != nil {
			return err
		}
		key := VolumePolicyKey(node, path)
		if executing {
			if expected, ok := before[key]; !ok || expected != v.Identity {
				return fmt.Errorf("%w: accepted disk identity changed", ErrConflict)
			}
		} else {
			c.AcceptedVolumes[key] = v.Identity
		}
		return nil
	}
	if c.Node != "" {
		if _, _, err := s.Clouds.ForHost(c.CloudID, c.Node); err != nil {
			return err
		}
	}
	if c.VM != "" && kind != "machine.create" {
		vm, err := s.Clouds.GetMachine(context.Background(), c.CloudID, c.VM)
		if err != nil {
			return err
		}
		if c.Node != "" && c.Node != vm.NodeId {
			return ErrNotFound
		}
		c.Node = vm.NodeId
		if err = s.CheckMachine(wc, vm, unassigned); err != nil {
			return err
		}
		for _, d := range vm.Volumes {
			if err = checkVolume(vm.NodeId, d.Path); err != nil {
				return err
			}
		}
		if kind == "machine.adopt" {
			p, err := s.PreviewAssignment(wc, []string{c.VM}, nil)
			if err != nil {
				return err
			}
			if !executing {
				c.Assignment = &p
			}
		}
	}
	if c.CreateSpec != nil {
		for _, d := range c.CreateSpec.ExistingDisks {
			if err := checkVolume(d.Host, d.Path); err != nil {
				return err
			}
		}
	}
	if c.Machine != nil {
		for _, d := range c.Machine.Volumes {
			if err := checkVolume(c.Node, d.Path); err != nil {
				return err
			}
		}
	}
	if c.Path != "" {
		if err := checkVolume(c.Node, c.Path); err != nil {
			return err
		}
	}
	if c.Disk != nil {
		if err := checkVolume(c.Node, c.Disk.Path); err != nil {
			return err
		}
	}
	if c.CloneVolume != nil {
		if err := checkVolume(c.Node, c.CloneVolume.OriginalPath); err != nil {
			return err
		}
	}
	for _, clone := range c.Clones {
		if clone.ImageID == "" {
			if err := checkVolume(c.Node, clone.OriginalPath); err != nil {
				return err
			}
		}
	}
	if c.CreateVolume != nil && c.CreateVolume.Pool != "" {
		if err := s.CheckVolumeLabel(wc, c.Node, c.CreateVolume.Pool, c.Label, ""); err != nil {
			return err
		}
	}
	if c.CloneVolume != nil && c.CloneVolume.NewPool != "" {
		if err := s.CheckVolumeLabel(wc, c.Node, c.CloneVolume.NewPool, c.Label, ""); err != nil {
			return err
		}
	}
	if kind == "machine.create" && c.Label != "" {
		if err := s.CheckMachineLabel(wc, c.Label, c.VM); err != nil {
			return err
		}
	}
	return nil
}
func (h *CommandHandler) Execute(ctx context.Context, command tasks.Command, p *tasks.Execution) error {
	if err := h.CheckCommandCompatibility(command); err != nil {
		return err
	}
	if err := h.ValidateContext(command); err != nil {
		return err
	}
	if h.Workloads == nil {
		return h.execute(ctx, command, p)
	}
	var c InfrastructureCommand
	if err := json.Unmarshal(command.Payload, &c); err != nil {
		return err
	}
	if strings.HasSuffix(command.Kind, ".assign") {
		if c.Assignment == nil || c.Assignment.Context.Namespace != c.NamespaceID || c.Assignment.Context.Cloud != c.CloudID {
			return ErrInvalid
		}
		if _, err := h.Workloads.validateAssignment(*c.Assignment); err != nil {
			return err
		}
		return p.Stage("assign-namespace", nil, true, func() error { return h.Workloads.ApplyAssignmentWithEffects(*c.Assignment, p.Effect) })
	}
	if err := h.validateWorkloadCommand(command.Kind, &c, true); err != nil {
		return err
	}
	// Cloud scheduling has to observe every workload. It binds namespace services
	// only after a physical placement has been selected.
	if c.CreateSpec != nil || c.VolumeSpec != nil || c.NewDisk != nil || command.Kind == "machine.adopt" || command.Kind == "machine.change-type" {
		return h.executeCloud(ctx, command, c, p)
	}
	local, err := h.scopedHandler(c)
	if err != nil {
		return err
	}
	command.Payload, _ = json.Marshal(c)
	return local.execute(ctx, command, p)
}
func (h *CommandHandler) scopedHandler(c InfrastructureCommand) (*CommandHandler, error) {
	local := *h
	sources := map[string]bool{}
	for _, clone := range c.Clones {
		if clone.ImageID != "" {
			sources[VolumePolicyKey(c.Node, clone.OriginalPath)] = true
		}
	}
	var err error
	local.Manager, local.VMs, local.Volumes, err = h.Workloads.bindWithSources(WorkloadContext{Namespace: c.NamespaceID, Cloud: c.CloudID}, h.Manager, sources, c.VM)
	return &local, err
}
