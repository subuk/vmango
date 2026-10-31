package api

import (
	"encoding/json"
	"github.com/rs/zerolog"
	"reflect"
	"sort"
	"subuk/vmango/compute"
	"sync"
	"testing"
)

type listNodes struct {
	compute.NodeRepository
	nodes []string
}

func (r *listNodes) Get(node string, _ compute.NodeGetOptions) (*compute.Node, error) {
	for _, n := range r.nodes {
		if n == node {
			return &compute.Node{Id: node}, nil
		}
	}
	return nil, compute.ErrUnknownNode
}

type listMachines struct {
	mu sync.Mutex
	compute.VirtualMachineRepository
	nodes []string
	fail  bool
}

func (r *listMachines) List(options compute.VirtualMachineListOptions) ([]*compute.VirtualMachine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes = append(r.nodes, options.NodeIds...)
	if r.fail {
		return nil, compute.ErrUnavailable
	}
	if options.NodeIds[0] != "a" {
		return []*compute.VirtualMachine{}, nil
	}
	return []*compute.VirtualMachine{{Id: "vm", Name: "example", NodeId: "a", Memory: compute.NewSize(1, compute.SizeUnitG)}}, nil
}

func TestCloudListsScopeHostsAndReportFailures(t *testing.T) {
	nodes := compute.NewNodeService(&listNodes{nodes: []string{"a", "b"}})
	repo := &listMachines{}
	manager := &compute.VirtualMachineManager{Clouds: &compute.CloudService{Definitions: []compute.Cloud{{ID: "local", Hosts: []compute.CloudHost{{Node: "a"}, {Node: "b"}}}}}}
	h := newTestAPI(t, apiTestConfig(t), zerolog.Nop(), nil, nil, nil, nodes, nil, compute.NewVirtualMachineService(repo), manager, nil)
	w := apiRequest(h, "GET", "/api/v1/clouds/local/machines", "", true)
	sort.Strings(repo.nodes)
	if w.Code != 200 || !reflect.DeepEqual(repo.nodes, []string{"a", "b"}) {
		t.Fatalf("cloud host filter lost: %d %s %+v", w.Code, w.Body, repo.nodes)
	}
	repo.fail = true
	w = apiRequest(h, "GET", "/api/v1/clouds/local/machines", "", true)
	var listed apiMachineList
	json.Unmarshal(w.Body.Bytes(), &listed)
	if w.Code != 200 || len(listed.Items) != 0 || len(listed.UnavailableHosts) != 2 {
		t.Fatal("failed backend appeared as successful empty list", w.Code, w.Body)
	}
	for _, route := range []string{"/api/v1/clouds/missing/machines", "/api/v1/clouds/local/machines/other/v", "/api/v1/clouds/local/volumes/other/dm9s", "/api/v1/clouds/local/machines/other/v/consoles/serial/ws"} {
		w = apiRequest(h, "GET", route, "", true)
		if w.Code != 404 {
			t.Fatalf("cloud scope bypass at %s: %d %s", route, w.Code, w.Body)
		}
	}
	for _, route := range []string{"/api/v1/clouds/local/machines/other/v/actions", "/api/v1/clouds/local/volumes/other/dm9s/resize"} {
		w = apiRequest(h, "POST", route, `{}`, true)
		if w.Code != 404 {
			t.Fatalf("mutation escaped cloud scope at %s: %d %s", route, w.Code, w.Body)
		}
	}
	for _, route := range []string{"/api/v1/nodes/a/vms/v", "/api/v1/vms", "/api/v1/volumes", "/api/v1/networks"} {
		w = apiRequest(h, "GET", route, "", true)
		if w.Code != 404 {
			t.Fatalf("retired API route remains at %s: %d", route, w.Code)
		}
	}
}

func TestMachineReadsUseCurrentHostCloudAndZone(t *testing.T) {
	for _, storedCloud := range []string{"personal", "other", "lanst", ""} {
		t.Run("stored-cloud="+storedCloud, func(t *testing.T) {
			vm := compute.VirtualMachine{Id: "vm", NodeId: "lanst", State: compute.StateRunning, VCpus: 8, Memory: compute.NewSize(16, compute.SizeUnitG)}
			if storedCloud != "" {
				vm.Metadata = &compute.MachineMetadata{Cloud: storedCloud, Zone: "old-zone", InstanceType: "retired-type", Image: "retired-image", CPURequest: 4, MemoryRequestBytes: 16 << 30}
			}
			repo := &apiVMRepo{vm: vm}
			clouds := &compute.CloudService{Definitions: []compute.Cloud{
				{ID: "lanst", Hosts: []compute.CloudHost{{Node: "lanst", Zone: "ber1"}}},
				{ID: "other", Hosts: []compute.CloudHost{{Node: "other-host", Zone: "other-zone"}}},
			}}
			h := newTestAPI(t, apiTestConfig(t), zerolog.Nop(), nil, nil, nil, nil, nil, compute.NewVirtualMachineService(repo), &compute.VirtualMachineManager{Clouds: clouds}, nil)
			for _, path := range []string{"/api/v1/clouds/lanst/machines", "/api/v1/clouds/lanst/machines/vm"} {
				w := apiRequest(h, "GET", path, "", true)
				if w.Code != 200 {
					t.Fatalf("%s: %d %s", path, w.Code, w.Body)
				}
				var got apiVM
				if path == "/api/v1/clouds/lanst/machines" {
					var list apiMachineList
					if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
						t.Fatal(err)
					}
					if len(list.Items) != 1 || len(list.UnavailableHosts) != 0 {
						t.Fatalf("machine disappeared from inventory: %s", w.Body)
					}
					got = list.Items[0]
				} else if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.Cloud != "lanst" || got.Zone != "ber1" || got.State != "running" {
					t.Fatalf("%s: machine location/state is not current: %+v", path, got)
				}
				if got.Managed != (vm.Metadata != nil) {
					t.Fatal("machine management status changed")
				}
				if m := vm.Metadata; m != nil {
					if got.InstanceType != m.InstanceType || got.Image != m.Image || got.CPURequest != m.CPURequest || got.MemoryRequestBytes != m.MemoryRequestBytes {
						t.Fatal("applied allocation snapshot changed", got)
					}
					if m.Cloud != storedCloud || m.Zone != "old-zone" {
						t.Fatal("read mutated stored metadata")
					}
				} else if got.CPURequest != vm.VCpus || got.MemoryRequestBytes != vm.Memory.Bytes() {
					t.Fatal("unmanaged resource requests lost", got)
				}
			}
			w := apiRequest(h, "GET", "/api/v1/clouds/other/machines", "", true)
			var other apiMachineList
			if err := json.Unmarshal(w.Body.Bytes(), &other); err != nil || w.Code != 200 || len(other.Items) != 0 {
				t.Fatalf("machine appeared in another cloud: %d %s", w.Code, w.Body)
			}
			if repo.saves != 0 {
				t.Fatal("read persisted changes")
			}
		})
	}
}

func TestReadModelKeepsInterfaceAddressesPinningAndKeyAvailability(t *testing.T) {
	v := &compute.VirtualMachine{Memory: compute.NewSize(1, compute.SizeUnitG), Graphic: compute.VirtualMachineGraphic{Listen: "127.0.0.1"}, Interfaces: []*compute.VirtualMachineAttachedInterface{{NetworkName: "default", Mac: "52:54:00:12:34:56", IpAddressList: []string{"192.0.2.4"}}}, Cpupin: &compute.VirtualMachineCpuPin{Emulator: []uint{3}, Vcpus: map[uint][]uint{2: {4}, 0: {1, 2}}}}
	d := vmDTO(v)
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["graphics_listen"] != "127.0.0.1" || body["configured_keys"] != nil {
		t.Fatal("edit field/unknown keys lost", string(raw))
	}
	interfaces := body["interfaces"].([]any)
	iface := interfaces[0].(map[string]any)
	if iface["network"] != "default" || iface["mac"] != "52:54:00:12:34:56" || len(iface["ip_addresses"].([]any)) != 1 {
		t.Fatal("interface details lost", string(raw))
	}
	if d.CPUPinning.VCPUs[0].VCPU != 0 || d.CPUPinning.VCPUs[1].VCPU != 2 {
		t.Fatal("pin identity changed on sorting")
	}
	v.Config = &compute.VirtualMachineConfig{}
	if vmDTO(v).ConfiguredKeys == nil {
		t.Fatal("known-empty keys became unknown")
	}
	problem := apiError(&compute.ProvisioningError{Stage: "save", Cause: &compute.ValidationError{Field: "memory_bytes", Message: "Safe validation message"}})
	if problem.(interface{ GetStatus() int }).GetStatus() != 500 {
		t.Fatal("partial provisioning was incorrectly reported as validation-only")
	}
}
