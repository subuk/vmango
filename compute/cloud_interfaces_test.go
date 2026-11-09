package compute_test

import (
	"encoding/json"
	"subuk/vmango/compute"
	"testing"
)

func TestLaterInterfaceUsesAppliedPolicyInsteadOfMutableBindingsOrExistingNIC(t *testing.T) {
	for _, test := range []struct {
		name, requestedModel string
		existing             bool
		reject               bool
	}{
		{name: "no existing interfaces"},
		{name: "existing interface is not policy", existing: true},
		{name: "conflicting explicit request", requestedModel: "e1000", reject: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, base, _, cloud := cloudFixtureHandler(t, 4, nil)
			backend := &policyTestBackend{cloudVMBackend: base}
			h.VMs.VirtualMachineRepository = backend
			cloud.Hosts[0].Networks["lan"] = compute.NetworkBinding{Type: compute.NetworkLibvirt, Network: "backend-net"}
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
			if command.Interface.Model != "virtio" || command.Interface.NetworkName != "backend-net" || command.Interface.AccessVlan != 0 || command.Interface.Binding.Kind() != compute.NetworkLibvirt {
				t.Fatal("applied policy or host network binding lost", command.Interface)
			}
			if base.mutations != 0 {
				t.Fatal("interface preparation caused side effects")
			}
		})
	}
}
