package compute

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

const hostReadConcurrency = 20

// MachineList contains fresh reads only. Failed hosts never appear as empty hosts.
type MachineList struct {
	Machines         []*VirtualMachine
	UnavailableHosts []string
}

type machineScan struct {
	done   chan struct{}
	result MachineList
}

// machineLocator caches routing hints, never machine state. A successful host
// read replaces that host's hints; a failed read preserves its last known hints.
type machineLocator struct {
	mu      sync.Mutex
	hosts   map[string]map[string]map[string]bool
	scans   map[string]*machineScan
	workers chan struct{}
}

func (s *CloudService) machineLocator() *machineLocator {
	s.locatorOnce.Do(func() {
		s.locator = &machineLocator{hosts: map[string]map[string]map[string]bool{}, scans: map[string]*machineScan{}, workers: make(chan struct{}, hostReadConcurrency)}
	})
	return s.locator
}

// ListMachines reads all configured hosts with bounded parallelism. Concurrent
// readers share only an in-flight scan; completed machine state is not cached.
func (s *CloudService) ListMachines(ctx context.Context, cloudID string) (MachineList, error) {
	c, err := s.Get(cloudID)
	if err != nil {
		return MachineList{}, err
	}
	l := s.machineLocator()
	l.mu.Lock()
	scan := l.scans[c.ID]
	if scan == nil {
		scan = &machineScan{done: make(chan struct{})}
		l.scans[c.ID] = scan
		go s.scanMachines(c, l, scan)
	}
	l.mu.Unlock()
	select {
	case <-ctx.Done():
		return MachineList{}, ctx.Err()
	case <-scan.done:
		return scan.result, nil
	}
}

func (s *CloudService) scanMachines(c Cloud, l *machineLocator, scan *machineScan) {
	type hostRead struct {
		machines []*VirtualMachine
		err      error
	}
	reads := make([]hostRead, len(c.Hosts))
	var wg sync.WaitGroup
	for n, host := range c.Hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.workers <- struct{}{}
			defer func() { <-l.workers }()
			machines, err := s.VMs.List(VirtualMachineListOptions{NodeIds: []string{host.Node}, Strict: true})
			for _, vm := range machines {
				if vm == nil || vm.Id == "" || vm.NodeId != host.Node {
					err = fmt.Errorf("invalid machine inventory for host %s", host.Node)
					break
				}
			}
			reads[n] = hostRead{machines, err}
		}()
	}
	wg.Wait()
	result := MachineList{Machines: []*VirtualMachine{}, UnavailableHosts: []string{}}
	l.mu.Lock()
	defer l.mu.Unlock()
	previous := l.hosts[c.ID]
	current := map[string]map[string]bool{}
	for n, host := range c.Hosts {
		r := reads[n]
		if r.err != nil {
			current[host.Node] = previous[host.Node]
			result.UnavailableHosts = append(result.UnavailableHosts, host.Node)
			continue
		}
		ids := map[string]bool{}
		for _, vm := range r.machines {
			ids[vm.Id] = true
		}
		current[host.Node] = ids
		result.Machines = append(result.Machines, r.machines...)
	}
	l.hosts[c.ID] = current
	scan.result = result
	delete(l.scans, c.ID)
	close(scan.done)
}

func (l *machineLocator) host(c Cloud, id string) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	node := ""
	for _, host := range c.Hosts {
		if l.hosts[c.ID][host.Node][id] {
			if node != "" {
				return "", fmt.Errorf("%w: machine ID occurs on multiple hosts", ErrConflict)
			}
			node = host.Node
		}
	}
	return node, nil
}

// GetMachine verifies the routing hint with a live UUID lookup. Only a missing
// hint or a definite not-found triggers discovery; connection failures remain
// availability errors. Mutations must not be retried after an uncertain result.
func (s *CloudService) GetMachine(ctx context.Context, cloudID, id string) (*VirtualMachine, error) {
	c, err := s.Get(cloudID)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return nil, ErrInvalid
	}
	l := s.machineLocator()
	node, err := l.host(c, id)
	if err != nil {
		return nil, err
	}
	if node != "" {
		vm, err := s.VMs.Get(id, node)
		if !errors.Is(err, ErrNotFound) {
			return vm, err
		}
	}
	result, err := s.ListMachines(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	node, err = l.host(c, id)
	if err != nil {
		return nil, err
	}
	if node == "" {
		if len(result.UnavailableHosts) > 0 {
			return nil, ErrUnavailable
		}
		return nil, ErrNotFound
	}
	return s.VMs.Get(id, node)
}
