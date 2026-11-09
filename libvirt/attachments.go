package libvirt

import (
	"encoding/json"
	"fmt"
	"strings"
	"subuk/vmango/compute"

	vir "libvirt.org/go/libvirt"
	x "libvirt.org/go/libvirtxml"
)

// Raw QEMU devices remain observable regardless of their names. Names never
// establish logical network membership or resource ownership.
func observeRawInterfaces(c *x.Domain) []*compute.VirtualMachineAttachedInterface {
	result := []*compute.VirtualMachineAttachedInterface{}
	if c.QEMUCommandline == nil {
		return result
	}
	netdevs := map[string]string{}
	args := c.QEMUCommandline.Args
	for i := 0; i+1 < len(args); i++ {
		if args[i].Value == "-netdev" {
			p := parseQemuDeviceArg(args[i+1].Value)
			netdevs[p["id"]] = args[i+1].Value
			i++
		}
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i].Value != "-device" {
			continue
		}
		raw := args[i+1].Value
		i++
		p := parseQemuDeviceArg(raw)
		backend, ok := netdevs[p["netdev"]]
		if !ok {
			continue
		}
		model := strings.SplitN(raw, ",", 2)[0]
		if model == "virtio-net-pci" || model == "virtio-net-device" {
			model = "virtio"
		}
		id := p["id"]
		if id == "" {
			id = p["netdev"]
		}
		n := &compute.VirtualMachineAttachedInterface{RawBackend: true, DeviceID: "qemu:" + id, NetworkName: p["netdev"], Mac: strings.ToLower(p["mac"]), Model: model, Observation: backend + "\n" + raw}
		switch rawBackendType(backend) {
		case "user":
			n.Binding = &compute.NetworkBinding{Type: compute.NetworkUser}
		case "stream":
			p := parseQemuDeviceArg(backend)
			if p["addr.type"] == "unix" {
				n.Binding = &compute.NetworkBinding{Type: compute.NetworkSocketVMNet, Socket: p["addr.path"]}
			}
		case "socket":
			n.Binding = &compute.NetworkBinding{Type: compute.NetworkSocketVMNet}
		}
		result = append(result, n)
	}
	return result
}

func observedAttachments(c *x.Domain) (*compute.VirtualMachine, error) {
	return VirtualMachineFromDomainConfig(c, &vir.DomainInfo{State: vir.DOMAIN_SHUTOFF})
}

// recordAttachments is called with the accepted device set, never a live catalog.
func recordAttachments(c *x.Domain, desired *compute.VirtualMachine) error {
	if desired.Metadata == nil {
		return fmt.Errorf("managed VM metadata is required")
	}
	observed, err := observedAttachments(c)
	if err != nil {
		return err
	}
	m := &compute.AttachmentMetadata{Version: compute.AttachmentVersion, Disks: []compute.DiskAttachment{}, Networks: []compute.NetworkAttachment{}}
	for _, d := range observed.Volumes {
		for _, want := range desired.Volumes {
			if d.Path == want.Path {
				d.Managed = want.Managed || want.GeneratedArtifact
				d.Purpose = want.Purpose
				if want.GeneratedArtifact {
					d.Purpose = compute.PurposeConfigDrive
				}
			}
		}
		r := compute.DiskRecord(d)
		if r.Purpose == compute.PurposeConfigDrive {
			if m.ConfigDrive != "" {
				return fmt.Errorf("multiple config drives")
			}
			m.ConfigDrive = r.ID
		}
		m.Disks = append(m.Disks, r)
	}
	for _, n := range observed.Interfaces {
		var wanted *compute.VirtualMachineAttachedInterface
		for _, want := range desired.Interfaces {
			if strings.EqualFold(want.Mac, n.Mac) {
				wanted = want
				break
			}
		}
		if wanted == nil || wanted.Binding == nil || wanted.LogicalNetwork == "" {
			return fmt.Errorf("network attachment lacks an accepted logical network and binding")
		}
		m.Networks = append(m.Networks, compute.NetworkAttachment{ID: n.DeviceID, MAC: n.Mac, Model: n.Model, LogicalNetwork: wanted.LogicalNetwork, Binding: *wanted.Binding, Observation: n.Observation, Managed: true})
	}
	if err := compute.ValidateAttachments(observed, m); err != nil {
		return err
	}
	desired.Metadata.Attachments = m
	return writeSchedulerMetadata(c, desired.Metadata)
}

func (repo *VirtualMachineRepository) PrepareAdoptionAttachments(vm *compute.VirtualMachine, m *compute.AttachmentMetadata) error {
	if err := compute.ApplyAttachmentMetadata(vm, m); err != nil {
		return err
	}
	if vm.Metadata == nil {
		vm.Metadata = &compute.MachineMetadata{}
	}
	copyMetadata := *vm.Metadata
	copyMetadata.Attachments = m
	vm.Metadata = &copyMetadata
	vm.Config = nil
	if m.ConfigDrive == "" {
		return nil
	}
	conn, err := repo.pool.Acquire(vm.NodeId)
	if err != nil {
		return err
	}
	defer repo.pool.Release(vm.NodeId)
	return repo.readSelectedConfigDrive(conn, vm)
}

func (repo *VirtualMachineRepository) readSelectedConfigDrive(conn *vir.Connect, vm *compute.VirtualMachine) error {
	for _, d := range vm.Volumes {
		if d.Purpose == compute.PurposeConfigDrive {
			config, err := repo.parseConfigDrive(conn, d.Path)
			if err != nil {
				return &compute.ValidationError{Field: "config_drive", Message: "Selected disk is not a readable supported config drive; select a valid NoCloud/OpenStack disk or No config drive with a compatible profile."}
			}
			vm.Config = config
			return nil
		}
	}
	return nil
}

// Normalized source data excludes libvirt-assigned bridge names and port IDs.
func interfaceObservation(c x.DomainInterface) string {
	source := c.Source
	if source != nil && source.Network != nil {
		source = &x.DomainInterfaceSource{Network: &x.DomainInterfaceSourceNetwork{Network: source.Network.Network, PortGroup: source.Network.PortGroup}}
	}
	raw, _ := json.Marshal(struct {
		Source *x.DomainInterfaceSource
		VLAN   *x.DomainInterfaceVLan
	}{source, c.VLan})
	return string(raw)
}

func updateAttachmentRecords(c *x.Domain, disk *compute.VirtualMachineAttachedVolume, nic *compute.VirtualMachineAttachedInterface) error {
	vm, err := observedAttachments(c)
	if err != nil {
		return err
	}
	if vm.Metadata == nil || vm.Metadata.Attachments == nil {
		return fmt.Errorf("attachment records are missing; explicitly re-adopt the VM")
	}
	old := vm.Metadata.Attachments
	for _, d := range vm.Volumes {
		for _, r := range old.Disks {
			if d.DeviceID == r.ID && d.Path == r.Source {
				d.Purpose, d.Managed = r.Purpose, r.Managed
			}
		}
		if disk != nil && disk.Path == d.Path {
			d.Managed = disk.Managed || disk.GeneratedArtifact
			d.Purpose = disk.Purpose
			if disk.GeneratedArtifact {
				d.Purpose = compute.PurposeConfigDrive
			}
		}
	}
	for _, n := range vm.Interfaces {
		for _, r := range old.Networks {
			if r.ID == n.DeviceID && r.MAC == n.Mac {
				b := r.Binding
				n.Binding, n.LogicalNetwork, n.Managed = &b, r.LogicalNetwork, r.Managed
			}
		}
		if nic != nil && strings.EqualFold(nic.Mac, n.Mac) {
			n.Binding, n.LogicalNetwork, n.Managed = nic.Binding, nic.LogicalNetwork, true
		}
	}
	return recordAttachments(c, vm)
}

// Remove only the observed NIC and its backend, leaving unrelated QEMU arguments intact.
func detachRawInterface(c *x.Domain, mac string) error {
	if c.QEMUCommandline == nil {
		return compute.ErrInterfaceNotFound
	}
	args := c.QEMUCommandline.Args
	backend := ""
	for i := 0; i+1 < len(args); i++ {
		if args[i].Value != "-device" {
			continue
		}
		p := parseQemuDeviceArg(args[i+1].Value)
		if strings.EqualFold(p["mac"], mac) && p["netdev"] != "" {
			backend = p["netdev"]
			break
		}
	}
	if backend == "" {
		return compute.ErrInterfaceNotFound
	}
	result := []x.DomainQEMUCommandlineArg{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if (arg.Value == "-device" || arg.Value == "-netdev") && i+1 < len(args) {
			next := args[i+1]
			p := parseQemuDeviceArg(next.Value)
			i++
			if arg.Value == "-device" && p["netdev"] == backend && strings.EqualFold(p["mac"], mac) || arg.Value == "-netdev" && p["id"] == backend {
				continue
			}
			result = append(result, arg, next)
		} else {
			result = append(result, arg)
		}
	}
	c.QEMUCommandline.Args = result
	return nil
}
