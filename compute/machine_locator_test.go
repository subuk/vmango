package compute

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type locatorBackend struct {
	VirtualMachineRepository
	mu                  sync.Mutex
	machines            map[string]VirtualMachine
	failed              map[string]bool
	lists, active, peak int
	started             chan string
	release             chan struct{}
}

func (r *locatorBackend) Get(id, node string) (*VirtualMachine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failed[node] {
		return nil, ErrUnavailable
	}
	v, ok := r.machines[node+"/"+id]
	if !ok {
		return nil, ErrNotFound
	}
	return &v, nil
}
func (r *locatorBackend) List(o VirtualMachineListOptions) ([]*VirtualMachine, error) {
	if !o.Strict || len(o.NodeIds) != 1 {
		panic("locator must read one complete host at a time")
	}
	node := o.NodeIds[0]
	r.mu.Lock()
	r.lists++
	r.active++
	r.peak = max(r.peak, r.active)
	r.mu.Unlock()
	if r.started != nil {
		r.started <- node
		<-r.release
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	defer func() { r.active-- }()
	if r.failed[node] {
		return nil, ErrUnavailable
	}
	result := []*VirtualMachine{}
	for _, vm := range r.machines {
		if vm.NodeId == node {
			v := vm
			result = append(result, &v)
		}
	}
	return result, nil
}
func locatorFixture() (*CloudService, *locatorBackend) {
	r := &locatorBackend{machines: map[string]VirtualMachine{"a/id": {Id: "id", Name: "same-name", NodeId: "a", State: StateRunning}}, failed: map[string]bool{}}
	s := &CloudService{Definitions: []Cloud{{ID: "c", Hosts: []CloudHost{{Node: "a"}, {Node: "b"}}}, {ID: "other", Hosts: []CloudHost{{Node: "outside"}}}}, VMs: NewVirtualMachineService(r)}
	return s, r
}

func TestMachineLocatorReadsLiveStateAndFindsMovedMachine(t *testing.T) {
	s, r := locatorFixture()
	ctx := context.Background()
	vm, err := s.GetMachine(ctx, "c", "id")
	if err != nil || vm.NodeId != "a" || r.lists != 2 {
		t.Fatalf("cold lookup: %+v %v reads=%d", vm, err, r.lists)
	}
	r.machines["a/id"] = VirtualMachine{Id: "id", Name: "renamed", NodeId: "a", State: StateStopped}
	vm, err = s.GetMachine(ctx, "c", "id")
	if err != nil || vm.Name != "renamed" || vm.State != StateStopped || r.lists != 2 {
		t.Fatalf("cached machine state or unnecessary scan: %+v %v", vm, err)
	}
	delete(r.machines, "a/id")
	r.machines["b/id"] = VirtualMachine{Id: "id", Name: "renamed", NodeId: "b"}
	vm, err = s.GetMachine(ctx, "c", "id")
	if err != nil || vm.NodeId != "b" || r.lists != 4 {
		t.Fatalf("stale routing hint: %+v %v", vm, err)
	}
	if _, err = s.GetMachine(ctx, "other", "id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("escaped cloud scope: %v", err)
	}
	delete(r.machines, "b/id")
	if _, err = s.GetMachine(ctx, "c", "id"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted VM: %v", err)
	}
}

func TestMachineListIsFreshAndFailurePreservesOnlyRoutingHint(t *testing.T) {
	s, r := locatorFixture()
	ctx := context.Background()
	first, err := s.ListMachines(ctx, "c")
	if err != nil || len(first.Machines) != 1 {
		t.Fatal(first, err)
	}
	r.failed["a"] = true
	r.machines["b/new"] = VirtualMachine{Id: "new", Name: "same-name", NodeId: "b"}
	second, err := s.ListMachines(ctx, "c")
	if err != nil || len(second.Machines) != 1 || second.Machines[0].Id != "new" || len(second.UnavailableHosts) != 1 || second.UnavailableHosts[0] != "a" {
		t.Fatal(second, err)
	}
	before := r.lists
	if _, err = s.GetMachine(ctx, "c", "id"); !errors.Is(err, ErrUnavailable) || before != r.lists {
		t.Fatalf("lost routing hint or scanned on connection failure: %v", err)
	}
	if _, err = s.GetMachine(ctx, "c", "unknown"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("incomplete inventory reported absence: %v", err)
	}
	r.failed["a"] = false
	delete(r.machines, "a/id")
	if _, err = s.ListMachines(ctx, "c"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetMachine(ctx, "c", "id"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestMachineLocatorRejectsDuplicateIDs(t *testing.T) {
	s, r := locatorFixture()
	r.machines["b/id"] = VirtualMachine{Id: "id", Name: "another-name", NodeId: "b"}
	if _, err := s.GetMachine(context.Background(), "c", "id"); !errors.Is(err, ErrConflict) {
		t.Fatalf("ambiguous routing accepted: %v", err)
	}
}

func TestMachineDiscoveryBoundsConcurrencyAndSharesInflightScan(t *testing.T) {
	s, r := locatorFixture()
	s.Definitions[0].Hosts = nil
	for n := range 45 {
		s.Definitions[0].Hosts = append(s.Definitions[0].Hosts, CloudHost{Node: fmt.Sprint(n)})
	}
	r.started, r.release = make(chan string, 45), make(chan struct{})
	done := make(chan error, 2)
	go func() { _, err := s.ListMachines(context.Background(), "c"); done <- err }()
	for range hostReadConcurrency {
		select {
		case <-r.started:
		case <-time.After(time.Second):
			t.Fatal("host reads did not run concurrently")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.ListMachines(ctx, "c"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	l := s.machineLocator()
	l.mu.Lock()
	flight := l.scans["c"]
	l.mu.Unlock()
	if flight == nil {
		t.Fatal("scan vanished while backend reads were blocked")
	}
	close(r.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if r.lists != 45 || r.peak > hostReadConcurrency {
		t.Fatalf("reads=%d peak=%d", r.lists, r.peak)
	}
}
