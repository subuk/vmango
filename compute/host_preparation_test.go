package compute

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
)

type preparationFake struct {
	state     HostPreparationState
	calls     []string
	failAfter string
}

func (r *preparationFake) Observe(ctx context.Context, _ string, _ HostPreparationSpec) (HostPreparationState, error) {
	return r.state, ctx.Err()
}
func (r *preparationFake) Execute(ctx context.Context, _ string, spec HostPreparationSpec, action string) error {
	r.calls = append(r.calls, action)
	switch action {
	case "create-network":
		r.state.Network = PreparationNetworkState{Exists: true, UUID: spec.NetworkUUID, Persistent: true, Mode: "nat", Subnet: spec.Subnet, Gateway: spec.Gateway(), DHCPStart: spec.DHCPStart, DHCPEnd: spec.DHCPEnd, ShapeCompatible: true, LiveCompatible: true, ConfigDigest: "network"}
	case "start-network":
		r.state.Network.Active = true
	case "autostart-network":
		r.state.Network.Autostart = true
	case "create-pool":
		r.state.Pool = PreparationPoolState{Exists: true, UUID: spec.PoolUUID, Persistent: true, Type: "dir", Path: spec.PoolPath, LiveCompatible: true, ConfigDigest: "pool"}
	case "start-pool":
		r.state.Pool.Active = true
	case "autostart-pool":
		r.state.Pool.Autostart = true
	}
	if r.failAfter == action {
		r.failAfter = ""
		return errors.New("interrupted after effect")
	}
	return nil
}
func preparationSpec() HostPreparationSpec {
	return HostPreparationSpec{Network: "test-net", Subnet: "192.168.174.0/24", DHCPStart: "192.168.174.20", DHCPEnd: "192.168.174.200", Pool: "test-pool", PoolPath: "/var/lib/test-pool"}
}
func TestHostPreparationCreateResumeAndNoOp(t *testing.T) {
	repo := &preparationFake{failAfter: "create-network"}
	svc := NewHostPreparationService(repo)
	p, err := svc.Prepare(context.Background(), "host", preparationSpec())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Actions) != 6 || p.Spec.NetworkUUID == "" || p.Spec.PoolUUID == "" {
		t.Fatal(p)
	}
	if err := svc.Apply(context.Background(), p); err == nil {
		t.Fatal("interruption ignored")
	}
	if err := svc.Apply(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(repo.calls, p.Actions) {
		t.Fatalf("completed creation repeated: %v", repo.calls)
	}
	again, err := svc.Prepare(context.Background(), "host", preparationSpec())
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Actions) != 0 {
		t.Fatal(again.Actions)
	}
	if err := svc.Apply(context.Background(), again); err != nil {
		t.Fatal(err)
	}
	if len(repo.calls) != 6 {
		t.Fatal("no-op mutated resources")
	}
}
func TestHostPreparationRejectsConflicts(t *testing.T) {
	for _, kind := range []string{"network subnet", "network inactive config", "network transient", "pool path", "pool type", "pool inactive config", "other subnet", "other pool"} {
		t.Run(kind, func(t *testing.T) {
			r := &preparationFake{}
			s := NewHostPreparationService(r)
			p, _ := s.Prepare(context.Background(), "host", preparationSpec())
			if err := s.Apply(context.Background(), p); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "network subnet":
				r.state.Network.Subnet = "192.168.175.0/24"
			case "network inactive config":
				r.state.Network.LiveCompatible = false
			case "network transient":
				r.state.Network.Persistent = false
			case "pool path":
				r.state.Pool.Path = "/existing-disks"
			case "pool type":
				r.state.Pool.Type = "logical"
			case "pool inactive config":
				r.state.Pool.LiveCompatible = false
			case "other subnet":
				r.state.OtherNetworks = []PreparationNetworkSubnet{{Name: "occupied", Subnet: "192.168.174.128/25"}}
			case "other pool":
				r.state.OtherPools = []PreparationPoolPath{{Name: "occupied", Path: preparationSpec().PoolPath}}
			}
			if _, err := s.Prepare(context.Background(), "host", preparationSpec()); !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
		})
	}
}
func TestHostPreparationRejectsStaleApprovalAndActionChanges(t *testing.T) {
	for _, kind := range []string{"network replacement", "pool replacement", "network config", "pool disappeared", "network stopped", "action appended", "new foreign resource"} {
		t.Run(kind, func(t *testing.T) {
			r := &preparationFake{}
			s := NewHostPreparationService(r)
			p, _ := s.Prepare(context.Background(), "host", preparationSpec())
			if err := s.Apply(context.Background(), p); err != nil {
				t.Fatal(err)
			}
			if kind != "new foreign resource" {
				p, _ = s.Prepare(context.Background(), "host", preparationSpec())
			}
			switch kind {
			case "network replacement", "new foreign resource":
				r.state.Network.UUID = "unreviewed"
			case "pool replacement":
				r.state.Pool.UUID = "unreviewed"
			case "network config":
				r.state.Network.ConfigDigest = "changed"
			case "pool disappeared":
				r.state.Pool = PreparationPoolState{}
			case "network stopped":
				r.state.Network.Active = false
			case "action appended":
				p.Actions = append(p.Actions, "start-network")
			}
			calls := len(r.calls)
			if err := s.Apply(context.Background(), p); !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
			if len(r.calls) != calls {
				t.Fatal("stale plan changed resources")
			}
		})
	}
}
func TestHostPreparationValidation(t *testing.T) {
	for _, kind := range []string{"name", "subnet", "noncanonical", "ipv6", "range order", "range gateway", "half range", "pool path"} {
		t.Run(kind, func(t *testing.T) {
			s := preparationSpec()
			switch kind {
			case "name":
				s.Network = "../bad"
			case "subnet":
				s.Subnet = "not-a-cidr"
			case "noncanonical":
				s.Subnet = "192.168.174.1/24"
			case "ipv6":
				s.Subnet = "fd00::/64"
			case "range order":
				s.DHCPStart = "192.168.174.220"
			case "range gateway":
				s.DHCPStart = "192.168.174.1"
			case "half range":
				s.DHCPEnd = ""
			case "pool path":
				s.PoolPath = "/var/lib/../data"
			}
			if err := s.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
	s := preparationSpec()
	s.DHCPStart = ""
	s.DHCPEnd = ""
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if lastPreparationAddress(mustPreparationPrefix(t, "192.168.174.0/24")).String() != "192.168.174.255" {
		t.Fatal("wrong broadcast")
	}
}

func mustPreparationPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	p, e := netip.ParsePrefix(s)
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func TestHostPreparationObserveDoesNotRequireDesiredSettings(t *testing.T) {
	repo := &preparationFake{state: HostPreparationState{Pool: PreparationPoolState{Exists: true, Path: "/existing/data"}}}
	service := NewHostPreparationService(repo)
	observed, err := service.Observe(context.Background(), "host", HostPreparationSpec{Network: "existing", Pool: "data"})
	if err != nil || observed.Pool.Path != "/existing/data" || len(repo.calls) != 0 {
		t.Fatal(observed, err, repo.calls)
	}
	if _, err := service.Observe(context.Background(), "host", HostPreparationSpec{Pool: "../data"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
