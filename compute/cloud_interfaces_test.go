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

func TestLaterInterfaceUsesAppliedPolicyInsteadOfMutableBindingsOrExistingNIC(t *testing.T) {
	for _, test := range []struct {
		name, hostModel, requestedModel string
		existing                        bool
		reject                          bool
	}{
		{name: "no existing interfaces"},
		{name: "existing interface is not policy", existing: true},
		{name: "compatible host constraint", hostModel: "virtio"},
		{name: "edited host constraint", hostModel: "e1000", reject: true},
		{name: "conflicting explicit request", requestedModel: "e1000", reject: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, base, _, cloud := cloudFixtureHandler(t, 4, nil)
			backend := &policyTestBackend{cloudVMBackend: base}
			h.VMs.VirtualMachineRepository = backend
			cloud.Hosts[0].Networks["lan"] = compute.NetworkBinding{Network: "backend-net", Model: test.hostModel, VLAN: 7}
			h.Clouds.Definitions = []compute.Cloud{cloud}
			vm := compute.VirtualMachine{Id: "v", Name: "v", NodeId: "a", State: compute.StateStopped, AppliedPolicy: json.RawMessage(`{"accepted":"virtio"}`), Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z"}}
			if test.existing {
				vm.Interfaces = []*compute.VirtualMachineAttachedInterface{{NetworkName: "existing", Model: "e1000"}}
			}
			base.machines["a/v"] = vm
			prepared, err := h.Prepare(compute.NewCommand("machine.attach-interface", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "v", Interface: &compute.VirtualMachineAttachedInterface{NetworkName: "lan", Model: test.requestedModel}}))
			if test.reject {
				if err == nil {
					t.Fatal("incompatible model accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var command compute.InfrastructureCommand
			if err := json.Unmarshal(prepared.Payload, &command); err != nil {
				t.Fatal(err)
			}
			if command.Interface.Model != "virtio" || command.Interface.NetworkName != "backend-net" || command.Interface.AccessVlan != 7 {
				t.Fatal("applied policy or host network binding lost", command.Interface)
			}
			if base.mutations != 0 {
				t.Fatal("interface preparation caused side effects")
			}
		})
	}
}
