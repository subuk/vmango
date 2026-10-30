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
