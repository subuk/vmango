package compute_test

import (
	"encoding/json"
	"subuk/vmango/compute"
	"testing"
)

func TestCloudInterfaceAttachmentUsesAdministratorBinding(t *testing.T) {
	_, backend, _, cloud := cloudFixture(t, 4)
	cloud.Hosts[0].Networks["lan"] = compute.NetworkBinding{Network: "backend-net", Model: "virtio", VLAN: 7}
	backend.machines["a/v"] = compute.VirtualMachine{Id: "v", Name: "v", NodeId: "a", State: compute.StateStopped, Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z"}}
	vms := compute.NewVirtualMachineService(backend)
	handler := &compute.CommandHandler{Clouds: &compute.CloudService{Definitions: []compute.Cloud{cloud}}, VMs: vms}
	command, err := handler.Prepare(compute.NewCommand("machine.attach-interface", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "v", Interface: &compute.VirtualMachineAttachedInterface{NetworkName: "lan", Mac: "52:54:00:12:34:56", Model: "e1000", AccessVlan: 42}}))
	if err != nil {
		t.Fatal(err)
	}
	var prepared compute.InfrastructureCommand
	if err := json.Unmarshal(command.Payload, &prepared); err != nil {
		t.Fatal(err)
	}
	iface := prepared.Interface
	if iface.LogicalNetwork != "lan" || iface.NetworkName != "backend-net" || iface.Model != "virtio" || iface.AccessVlan != 7 || iface.Mac != "52:54:00:12:34:56" {
		t.Fatalf("administrator binding did not govern attachment: %+v", iface)
	}
}
