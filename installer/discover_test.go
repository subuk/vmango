//go:build linux || darwin

package installer

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"subuk/vmango/compute"
)

type observingResources struct {
	*fakeResources
	observe      func(context.Context, string, compute.HostPreparationSpec) (compute.HostPreparationState, error)
	observeCalls atomic.Int32
}

func (r *observingResources) Observe(ctx context.Context, node string, spec compute.HostPreparationSpec) (compute.HostPreparationState, error) {
	r.observeCalls.Add(1)
	return r.observe(ctx, node, spec)
}

func existingTestResources() compute.HostPreparationState {
	return compute.HostPreparationState{
		Network: compute.PreparationNetworkState{Exists: true, Mode: "nat", ShapeCompatible: true, Subnet: "10.48.3.0/24", DHCPStart: "10.48.3.10", DHCPEnd: "10.48.3.200"},
		Pool:    compute.PreparationPoolState{Exists: true, Type: "dir", Path: "/srv/existing-vms"},
	}
}

func TestDiscoverLoadsSelectedExistingSettingsWithoutMutations(t *testing.T) {
	p := &fakeProvisioner{}
	r := &observingResources{fakeResources: &fakeResources{}, observe: func(_ context.Context, node string, spec compute.HostPreparationSpec) (compute.HostPreparationState, error) {
		if node != "test-a" || spec.Network != "default" || spec.Pool != "default" {
			t.Fatalf("unexpected observation: %s %+v", node, spec)
		}
		return existingTestResources(), nil
	}}
	c, _ := newTestController(t, true, p, r.fakeResources)
	c.opts.Resources = func([]Host) Resources { return r }
	a := testAnswers(c)
	unselected := a.Hosts[0]
	unselected.ID = "unavailable"
	unselected.Address = "unavailable"
	unselected.Selected = false
	a.Hosts = append(a.Hosts, unselected)
	if err := c.Answers(a); err != nil {
		t.Fatal(err)
	}
	before := c.Snapshot()
	c.mu.Lock()
	c.data.State.Plan = &Review{ID: "old-approval", Stage: "resources"}
	c.data.Approved = true
	c.data.HostReady[a.Hosts[0].ID] = hostDigest(a.Hosts[0])
	c.data.ResourceReady[a.Hosts[0].ID] = hostDigest(a.Hosts[0])
	c.mu.Unlock()
	configBefore, err := os.ReadFile(c.document.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Discover(before.Revision); err != nil {
		t.Fatal(err)
	}
	s := waitInstaller(t, c)
	if s.Error != "" || s.Revision != before.Revision+1 || s.Plan != nil || s.CanResume {
		t.Fatalf("unexpected discovery state: %+v", s)
	}
	h := s.Hosts[0]
	if h.Subnet != "10.48.3.0/24" || h.DHCPStart != "10.48.3.10" || h.DHCPEnd != "10.48.3.200" || h.PoolPath != "/srv/existing-vms" {
		t.Fatalf("existing values were not loaded: %+v", h)
	}
	if !EqualAnswers(s.Hosts[1], unselected) || s.Hosts[1].Selected {
		t.Fatal("unselected host changed")
	}
	if len(s.AllowedStages) != 1 || s.AllowedStages[0] != "hosts" {
		t.Fatal("changed answers retained readiness", s.AllowedStages)
	}
	if r.observeCalls.Load() != 1 || r.prepareCalls.Load() != 0 || r.applyCalls.Load() != 0 || p.prepareCalls.Load() != 0 || p.applyCalls.Load() != 0 {
		t.Fatal("discovery performed preparation or mutations")
	}
	configAfter, err := os.ReadFile(c.document.Path())
	if err != nil || string(configAfter) != string(configBefore) {
		t.Fatal("discovery changed config", err)
	}
}

func TestDiscoverMissingResourcesPreserveDesiredAnswersAndRevision(t *testing.T) {
	r := &observingResources{fakeResources: &fakeResources{}, observe: func(context.Context, string, compute.HostPreparationSpec) (compute.HostPreparationState, error) {
		return compute.HostPreparationState{}, nil
	}}
	c, _ := newTestController(t, true, nil, r.fakeResources)
	c.opts.Resources = func([]Host) Resources { return r }
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	before := c.Snapshot()
	if err := c.Discover(before.Revision); err != nil {
		t.Fatal(err)
	}
	after := waitInstaller(t, c)
	if after.Error != "" || after.Revision != before.Revision || !EqualAnswers(before.Hosts[0], after.Hosts[0]) {
		t.Fatalf("missing resources changed answers: %+v", after)
	}
	if !strings.Contains(after.Hosts[0].Message, "absent") {
		t.Fatal("missing resources were not reported")
	}
}

func TestDiscoverIndependentHostFailureKeepsSuccessfulObservation(t *testing.T) {
	r := &observingResources{fakeResources: &fakeResources{}, observe: func(_ context.Context, node string, _ compute.HostPreparationSpec) (compute.HostPreparationState, error) {
		if node == "test-b" {
			return compute.HostPreparationState{}, errors.New("daemon unavailable")
		}
		return existingTestResources(), nil
	}}
	c, _ := newTestController(t, true, nil, r.fakeResources)
	c.opts.Resources = func([]Host) Resources { return r }
	a := testAnswers(c)
	b := a.Hosts[0]
	b.ID = "test-b"
	b.Address = "test-b"
	a.Hosts = append(a.Hosts, b)
	if err := c.Answers(a); err != nil {
		t.Fatal(err)
	}
	before := c.Snapshot()
	if err := c.Discover(before.Revision); err != nil {
		t.Fatal(err)
	}
	after := waitInstaller(t, c)
	if after.Error == "" || after.Hosts[0].PoolPath != "/srv/existing-vms" || !EqualAnswers(after.Hosts[1], b) || after.Hosts[1].Status != "failed" {
		t.Fatalf("partial read lost state: %+v", after)
	}
	if after.Revision != before.Revision+1 || !strings.Contains(after.Hosts[1].Message, "daemon unavailable") {
		t.Fatal("partial read result is incomplete")
	}
}

func TestDiscoverUnsupportedResourcesNeverReplaceDesiredFields(t *testing.T) {
	r := &observingResources{fakeResources: &fakeResources{}, observe: func(context.Context, string, compute.HostPreparationSpec) (compute.HostPreparationState, error) {
		s := existingTestResources()
		s.Network.Mode = "bridge"
		s.Pool.Type = "logical"
		return s, nil
	}}
	c, _ := newTestController(t, true, nil, r.fakeResources)
	c.opts.Resources = func([]Host) Resources { return r }
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	before := c.Snapshot()
	if err := c.Discover(before.Revision); err != nil {
		t.Fatal(err)
	}
	after := waitInstaller(t, c)
	if after.Error == "" || after.Revision != before.Revision || !EqualAnswers(before.Hosts[0], after.Hosts[0]) {
		t.Fatalf("unsupported resources replaced desired fields: %+v", after)
	}
}

func TestDiscoverRequiresCurrentAnswersAndRejectsDuplicateWork(t *testing.T) {
	started := make(chan struct{})
	r := &observingResources{fakeResources: &fakeResources{}, observe: func(ctx context.Context, _ string, _ compute.HostPreparationSpec) (compute.HostPreparationState, error) {
		close(started)
		<-ctx.Done()
		return compute.HostPreparationState{}, ctx.Err()
	}}
	c, _ := newTestController(t, true, nil, r.fakeResources)
	c.opts.Resources = func([]Host) Resources { return r }
	if err := c.Discover(c.Snapshot().Revision); err == nil {
		t.Fatal("discovery without hosts accepted")
	}
	if err := c.Answers(testAnswers(c)); err != nil {
		t.Fatal(err)
	}
	before := c.Snapshot()
	if err := c.Discover(before.Revision - 1); err == nil {
		t.Fatal("stale discovery accepted")
	}
	if err := c.Discover(before.Revision); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := c.Discover(before.Revision); err == nil {
		t.Fatal("duplicate discovery accepted")
	}
	if err := c.Cancel(); err != nil {
		t.Fatal(err)
	}
	after := waitInstaller(t, c)
	if after.Error == "" || after.CanResume || !EqualAnswers(before.Hosts[0], after.Hosts[0]) {
		t.Fatal("cancelled observation changed answers or became resumable")
	}
}
