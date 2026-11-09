package compute

import (
	"strings"
	"testing"
)

func TestAdoptionMatchesConfiguredNetworksAndRequiresDiskChoice(t *testing.T) {
	nic := (NetworkBinding{Type: NetworkLibvirt, Network: "backend"}).Interface("")
	nic.DeviceID, nic.Mac, nic.Model = "mac:one", "one", "virtio"
	vm := &VirtualMachine{Interfaces: []*VirtualMachineAttachedInterface{nic}, Volumes: []*VirtualMachineAttachedVolume{{DeviceID: "vda", Path: "/unrelated_config.iso", Format: "raw", DeviceType: DeviceTypeDisk, DeviceBus: DeviceBusVirtio}}}
	host := CloudHost{Node: "host", Networks: map[string]NetworkBinding{"lan": {Type: NetworkLibvirt, Network: "backend"}}}
	if _, err := AdoptionAttachments(vm, host, nil, nil); err == nil {
		t.Fatal("missing choice accepted")
	}
	none := ""
	m, err := AdoptionAttachments(vm, host, &none, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.ConfigDrive != "" || m.Disks[0].Purpose != "disk" || m.Disks[0].Managed || m.Networks[0].LogicalNetwork != "lan" {
		t.Fatal(m)
	}
	host.Networks["other"] = host.Networks["lan"]
	if _, err := AdoptionAttachments(vm, host, &none, nil); err == nil {
		t.Fatal("ambiguous binding guessed")
	}
	if _, err := AdoptionAttachments(vm, host, &none, map[string]string{"mac:one": "other"}); err != nil {
		t.Fatal(err)
	}
	host.Networks = map[string]NetworkBinding{}
	if _, err := AdoptionAttachments(vm, host, &none, nil); err == nil || !strings.Contains(err.Error(), "configure a matching network") {
		t.Fatal(err)
	}
	// Applied metadata is independent of later catalog removal.
	if err := ApplyAttachmentMetadata(vm, m); err != nil || vm.Interfaces[0].LogicalNetwork != "lan" {
		t.Fatal(err)
	}
}

func TestAdoptionRecordsConfigDriveWithoutClaimingDisk(t *testing.T) {
	vm := &VirtualMachine{Volumes: []*VirtualMachineAttachedVolume{{DeviceID: "sda", Path: "/external.iso", Format: "raw", DeviceType: DeviceTypeCdrom, DeviceBus: DeviceBusScsi}}}
	selected := "sda"
	m, err := AdoptionAttachments(vm, CloudHost{}, &selected, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.ConfigDrive != "sda" || m.Disks[0].Managed {
		t.Fatal("claimed disk")
	}
	if err := ApplyAttachmentMetadata(vm, m); err != nil {
		t.Fatal(err)
	}
	if vm.Volumes[0].Purpose != PurposeConfigDrive || vm.Volumes[0].Managed {
		t.Fatal("incorrect designation")
	}
	selected = "missing"
	if _, err := AdoptionAttachments(vm, CloudHost{}, &selected, nil); err == nil {
		t.Fatal("missing disk accepted")
	}
}
