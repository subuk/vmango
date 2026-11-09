package libvirt

import (
	"encoding/json"
	vir "libvirt.org/go/libvirt"
	x "libvirt.org/go/libvirtxml"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"
	"testing"
)

func fixtureAttachments(vm *compute.VirtualMachine) *compute.AttachmentMetadata {
	m := &compute.AttachmentMetadata{Version: compute.AttachmentVersion, Disks: []compute.DiskAttachment{}, Networks: []compute.NetworkAttachment{}}
	for _, d := range vm.Volumes {
		m.Disks = append(m.Disks, compute.DiskRecord(d))
	}
	for _, n := range vm.Interfaces {
		b := compute.NetworkBinding{Type: compute.NetworkLibvirt, Network: n.NetworkName}
		if n.Binding != nil {
			b = *n.Binding
		}
		m.Networks = append(m.Networks, compute.NetworkAttachment{ID: n.DeviceID, MAC: n.Mac, Model: n.Model, Binding: b, LogicalNetwork: "lan", Observation: n.Observation, Managed: true})
	}
	return m
}

func TestAttachmentMetadataPreservesIdentityOwnershipAndBindings(t *testing.T) {
	c := newDomainConfig(&compute.VirtualMachine{Id: "vm"}, &x.DomainCaps{Arch: "aarch64"})
	c.Memory = &x.DomainMemory{Value: 1024, Unit: "MiB"}
	c.VCPU = &x.DomainVCPU{Value: 1}
	c.Devices.Disks = []x.DomainDisk{{Device: "disk", Driver: &x.DomainDiskDriver{Type: "raw"}, Source: &x.DomainDiskSource{File: &x.DomainDiskSourceFile{File: "/external_config.iso"}}, Target: &x.DomainDiskTarget{Dev: "vda", Bus: "virtio"}, Alias: &x.DomainAlias{Name: "ua-configdrive"}}}
	nic := (compute.NetworkBinding{Type: compute.NetworkUser}).Interface("internet")
	nic.Model = "virtio"
	if err := (&VirtualMachineRepository{}).attachInterface(c, nic); err != nil {
		t.Fatal(err)
	}
	vm, err := observedAttachments(c)
	if err != nil {
		t.Fatal(err)
	}
	if vm.Volumes[0].Purpose != "" || vm.Volumes[0].Managed || vm.Interfaces[0].LogicalNetwork != "" {
		t.Fatal("alias or filename inferred ownership")
	}
	selection := "vda"
	m, err := compute.AdoptionAttachments(vm, compute.CloudHost{Node: "host", Networks: map[string]compute.NetworkBinding{"internet": {Type: compute.NetworkUser}}}, &selection, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.Disks[0].Managed || m.Disks[0].Purpose != compute.PurposeConfigDrive {
		t.Fatal("designation claimed external disk")
	}
	before := *c.Devices
	if err := writeSchedulerMetadata(c, &compute.MachineMetadata{Cloud: "cloud", Zone: "zone", CPURequest: 1, MemoryRequestBytes: 1 << 30, Attachments: m}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, *c.Devices) {
		t.Fatal("adoption rewrote hardware")
	}
	raw, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var decoded x.Domain
	if err := decoded.Unmarshal(raw); err != nil {
		t.Fatal(err)
	}
	restarted, err := VirtualMachineFromDomainConfig(&decoded, &vir.DomainInfo{State: vir.DOMAIN_SHUTOFF})
	if err != nil {
		t.Fatal(err)
	}
	if err := compute.ValidateAttachments(restarted, restarted.Metadata.Attachments); err != nil {
		t.Fatal(err)
	}
	if restarted.Interfaces[0].LogicalNetwork != "internet" || restarted.Interfaces[0].Binding.Kind() != compute.NetworkUser || restarted.Volumes[0].Managed || restarted.Volumes[0].Purpose != compute.PurposeConfigDrive {
		t.Fatal("persisted records lost")
	}
	decoded.Devices.Interfaces[0].Model.Type = "e1000"
	drifted, _ := observedAttachments(&decoded)
	if compute.ValidateAttachments(drifted, m) == nil {
		t.Fatal("device drift accepted")
	}
}

func TestRawNetworkObservationDoesNotInferLogicalIdentity(t *testing.T) {
	c := &x.Domain{QEMUCommandline: &x.DomainQEMUCommandline{Args: []x.DomainQEMUCommandlineArg{{Value: "-netdev"}, {Value: "user,id=vmngmcos1"}, {Value: "-device"}, {Value: "virtio-net-device,netdev=vmngmcos1,mac=52:54:00:00:00:01"}, {Value: "-device"}}}}
	n := observeRawInterfaces(c)
	if len(n) != 1 || n[0].Managed || n[0].LogicalNetwork != "" || n[0].NetworkName != "vmngmcos1" || !n[0].RawBackend || !strings.Contains(n[0].Observation, "netdev=vmngmcos1") {
		t.Fatal(n)
	}
}

func TestAcceptedCommandRejectsIncompleteNestedPlansAndBindings(t *testing.T) {
	handler := &compute.CommandHandler{VMs: compute.NewVirtualMachineService(&VirtualMachineRepository{})}
	check := func(c compute.InfrastructureCommand) error {
		raw, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		return handler.CheckCommandCompatibility(tasks.Command{Kind: "machine.create", Payload: raw})
	}
	plan := fixtureHardwarePolicy(t, &compute.VirtualMachine{Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone})
	c := compute.InfrastructureCommand{Version: compute.InfrastructureCommandVersion, Machine: &compute.VirtualMachine{AppliedPolicy: plan}}
	if err := check(c); err != nil {
		t.Fatal(err)
	}
	var nested map[string]any
	if err := json.Unmarshal(plan, &nested); err != nil {
		t.Fatal(err)
	}
	delete(nested["profile"].(map[string]any), "presentation")
	c.Machine.AppliedPolicy, _ = json.Marshal(nested)
	if check(c) == nil {
		t.Fatal("missing presentation accepted")
	}
	c.Machine.AppliedPolicy = plan
	c.Machine.Interfaces = []*compute.VirtualMachineAttachedInterface{{NetworkName: "qemu-usernet"}}
	if check(c) == nil {
		t.Fatal("implicit network name accepted")
	}
	nic := (compute.NetworkBinding{Type: compute.NetworkUser}).Interface("internet")
	c.Machine.Interfaces = []*compute.VirtualMachineAttachedInterface{nic}
	if err := check(c); err != nil {
		t.Fatal(err)
	}
	nic.AccessVlan = 7
	if check(c) == nil {
		t.Fatal("removed VLAN accepted")
	}
}

func TestNativeNetworkRecordsRetainLogicalIdentity(t *testing.T) {
	for _, binding := range []compute.NetworkBinding{{Type: compute.NetworkLibvirt, Network: "backend"}, {Type: compute.NetworkBridge, Bridge: "br0"}} {
		vm := &compute.VirtualMachine{Id: "vm", Metadata: &compute.MachineMetadata{Cloud: "cloud", Zone: "zone", CPURequest: 1, MemoryRequestBytes: 512 << 20}}
		c := newDomainConfig(vm, &x.DomainCaps{Arch: "aarch64"})
		c.Memory = &x.DomainMemory{Value: 512, Unit: "MiB"}
		c.VCPU = &x.DomainVCPU{Value: 1}
		nic := binding.Interface("logical")
		nic.Model = "virtio"
		vm.Interfaces = []*compute.VirtualMachineAttachedInterface{nic}
		if err := (&VirtualMachineRepository{}).attachInterface(c, nic); err != nil {
			t.Fatal(err)
		}
		if err := recordAttachments(c, vm); err != nil {
			t.Fatal(err)
		}
		if source := c.Devices.Interfaces[0].Source.Network; source != nil {
			source.Bridge, source.PortID = "virbr0", "runtime-port"
		}
		raw, _ := c.Marshal()
		var restart x.Domain
		if err := restart.Unmarshal(raw); err != nil {
			t.Fatal(err)
		}
		observed, err := observedAttachments(&restart)
		if err != nil {
			t.Fatal(err)
		}
		if err := compute.ValidateAttachments(observed, observed.Metadata.Attachments); err != nil {
			t.Fatal(err)
		}
		if observed.Interfaces[0].LogicalNetwork != "logical" || *observed.Interfaces[0].Binding != binding {
			t.Fatal(observed.Interfaces[0])
		}
	}
}

func TestRawInterfaceRemovalPreservesUnrelatedArguments(t *testing.T) {
	c := &x.Domain{QEMUCommandline: &x.DomainQEMUCommandline{Args: []x.DomainQEMUCommandlineArg{{Value: "-netdev"}, {Value: "user,id=private"}, {Value: "-device"}, {Value: "virtio-net-device,netdev=private,mac=52:54:00:00:00:01"}, {Value: "-device"}, {Value: "virtio-blk-device,drive=disk"}, {Value: "-device"}}}}
	before, _ := json.Marshal(c)
	if err := detachRawInterface(c, "missing"); err != compute.ErrInterfaceNotFound {
		t.Fatal(err)
	}
	after, _ := json.Marshal(c)
	if string(before) != string(after) {
		t.Fatal("unknown NIC removed unrelated devices")
	}
	if err := detachRawInterface(c, "52:54:00:00:00:01"); err != nil {
		t.Fatal(err)
	}
	args := c.QEMUCommandline.Args
	if len(args) != 3 || args[1].Value != "virtio-blk-device,drive=disk" {
		t.Fatal(args)
	}
}
