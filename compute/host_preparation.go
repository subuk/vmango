package compute

import (
	"context"
	"fmt"
	"net/netip"
	"path"
	"reflect"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type HostPreparationSpec struct {
	Network     string `json:"network"`
	Subnet      string `json:"subnet"`
	DHCPStart   string `json:"dhcp_start"`
	DHCPEnd     string `json:"dhcp_end"`
	Pool        string `json:"pool"`
	PoolPath    string `json:"pool_path"`
	NetworkUUID string `json:"network_uuid"`
	PoolUUID    string `json:"pool_uuid"`
}

type PreparationNetworkState struct {
	Exists          bool   `json:"exists"`
	UUID            string `json:"uuid"`
	Persistent      bool   `json:"persistent"`
	Active          bool   `json:"active"`
	Autostart       bool   `json:"autostart"`
	Mode            string `json:"mode"`
	Subnet          string `json:"subnet"`
	Gateway         string `json:"gateway"`
	DHCPStart       string `json:"dhcp_start"`
	DHCPEnd         string `json:"dhcp_end"`
	ShapeCompatible bool   `json:"shape_compatible"`
	LiveCompatible  bool   `json:"live_compatible"`
	ConfigDigest    string `json:"config_digest"`
}

type PreparationPoolState struct {
	Exists         bool   `json:"exists"`
	UUID           string `json:"uuid"`
	Persistent     bool   `json:"persistent"`
	Active         bool   `json:"active"`
	Autostart      bool   `json:"autostart"`
	Type           string `json:"type"`
	Path           string `json:"path"`
	LiveCompatible bool   `json:"live_compatible"`
	ConfigDigest   string `json:"config_digest"`
}

type PreparationNetworkSubnet struct {
	Name   string `json:"name"`
	Subnet string `json:"subnet"`
}
type PreparationPoolPath struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type HostPreparationState struct {
	Network       PreparationNetworkState    `json:"network"`
	Pool          PreparationPoolState       `json:"pool"`
	OtherNetworks []PreparationNetworkSubnet `json:"other_networks"`
	OtherPools    []PreparationPoolPath      `json:"other_pools"`
}
type HostPreparationPlan struct {
	Node    string               `json:"node"`
	Spec    HostPreparationSpec  `json:"spec"`
	Before  HostPreparationState `json:"before"`
	Actions []string             `json:"actions"`
	Summary []string             `json:"summary"`
}
type HostPreparationRepository interface {
	Observe(context.Context, string, HostPreparationSpec) (HostPreparationState, error)
	Execute(context.Context, string, HostPreparationSpec, string) error
}
type HostPreparationService struct{ repo HostPreparationRepository }

func NewHostPreparationService(repo HostPreparationRepository) *HostPreparationService {
	return &HostPreparationService{repo: repo}
}

// Observe returns existing resource settings before the operator chooses their
// desired subnet or storage path. It performs no resource preparation.
func (s *HostPreparationService) Observe(ctx context.Context, node string, spec HostPreparationSpec) (HostPreparationState, error) {
	if node == "" || (spec.Network != "" && !preparationName.MatchString(spec.Network)) || (spec.Pool != "" && !preparationName.MatchString(spec.Pool)) {
		return HostPreparationState{}, fmt.Errorf("%w: valid host and resource identifiers are required", ErrInvalid)
	}
	return s.repo.Observe(ctx, node, spec)
}

var preparationName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

// ValidateHostPreparationSpec checks requested resource inputs before any OS
// preparation is started. Existing resource values come from Observe.
func ValidateHostPreparationSpec(spec HostPreparationSpec) error { return spec.Validate() }

func (s HostPreparationSpec) Validate() error {
	if !preparationName.MatchString(s.Network) || !preparationName.MatchString(s.Pool) {
		return fmt.Errorf("%w: network and pool names must use 1..63 letters, numbers, dots, underscores or hyphens", ErrInvalid)
	}
	p, err := netip.ParsePrefix(s.Subnet)
	if err != nil || !p.Addr().Is4() || p != p.Masked() || p.Bits() < 8 || p.Bits() > 30 || p.Addr().IsLoopback() || p.Addr().IsMulticast() || p.Addr().IsUnspecified() {
		return fmt.Errorf("%w: NAT subnet must be a canonical IPv4 CIDR with prefix 8..30", ErrInvalid)
	}
	if (s.DHCPStart == "") != (s.DHCPEnd == "") {
		return fmt.Errorf("%w: supply both DHCP range endpoints or neither", ErrInvalid)
	}
	if s.DHCPStart != "" {
		first, e1 := netip.ParseAddr(s.DHCPStart)
		last, e2 := netip.ParseAddr(s.DHCPEnd)
		broadcast := lastPreparationAddress(p)
		if e1 != nil || e2 != nil || !p.Contains(first) || !p.Contains(last) || first.Compare(last) > 0 || first.Compare(p.Addr().Next()) <= 0 || last.Compare(broadcast) >= 0 {
			return fmt.Errorf("%w: DHCP range must be ordered inside the subnet, excluding network, gateway and broadcast", ErrInvalid)
		}
	}
	if !path.IsAbs(s.PoolPath) || path.Clean(s.PoolPath) != s.PoolPath || s.PoolPath == "/" || strings.ContainsAny(s.PoolPath, "\x00\r\n") {
		return fmt.Errorf("%w: directory pool needs a clean absolute target-host path other than /", ErrInvalid)
	}
	return nil
}
func lastPreparationAddress(p netip.Prefix) netip.Addr {
	b := p.Addr().As4()
	bits := uint32(1)<<(32-p.Bits()) - 1
	n := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	n |= bits
	return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)})
}
func (s HostPreparationSpec) Gateway() string {
	p, e := netip.ParsePrefix(s.Subnet)
	if e != nil {
		return ""
	}
	return p.Masked().Addr().Next().String()
}

func (s *HostPreparationService) Prepare(ctx context.Context, node string, spec HostPreparationSpec) (*HostPreparationPlan, error) {
	if node == "" {
		return nil, fmt.Errorf("%w: host identifier is required", ErrInvalid)
	}
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	state, err := s.repo.Observe(ctx, node, spec)
	if err != nil {
		return nil, err
	}
	if err := compatiblePreparation(spec, state); err != nil {
		return nil, err
	}
	if state.Network.Exists {
		spec.NetworkUUID = state.Network.UUID
	} else {
		spec.NetworkUUID = uuid.NewString()
	}
	if state.Pool.Exists {
		spec.PoolUUID = state.Pool.UUID
	} else {
		spec.PoolUUID = uuid.NewString()
	}
	actions := preparationActions(state)
	summary := make([]string, 0, len(actions))
	for _, a := range actions {
		summary = append(summary, fmt.Sprintf("%s: %s (network %s; pool %s at %s)", node, a, spec.Network, spec.Pool, spec.PoolPath))
	}
	if len(actions) == 0 {
		summary = append(summary, fmt.Sprintf("%s: reuse compatible active persistent network %s and pool %s", node, spec.Network, spec.Pool))
	}
	return &HostPreparationPlan{Node: node, Spec: spec, Before: state, Actions: actions, Summary: summary}, nil
}
func preparationActions(s HostPreparationState) []string {
	a := []string{}
	if !s.Network.Exists {
		a = append(a, "create-network")
	}
	if !s.Network.Active {
		a = append(a, "start-network")
	}
	if !s.Network.Autostart {
		a = append(a, "autostart-network")
	}
	if !s.Pool.Exists {
		a = append(a, "create-pool")
	}
	if !s.Pool.Active {
		a = append(a, "start-pool")
	}
	if !s.Pool.Autostart {
		a = append(a, "autostart-pool")
	}
	return a
}
func compatiblePreparation(spec HostPreparationSpec, state HostPreparationState) error {
	n := state.Network
	if n.Exists && (!n.Persistent || !n.ShapeCompatible || !n.LiveCompatible || n.Mode != "nat" || n.Subnet != spec.Subnet || n.Gateway != spec.Gateway() || n.DHCPStart != spec.DHCPStart || n.DHCPEnd != spec.DHCPEnd) {
		return fmt.Errorf("%w: network %s is incompatible; existing resources are never redefined", ErrConflict, spec.Network)
	}
	p := state.Pool
	if p.Exists && (!p.Persistent || !p.LiveCompatible || p.Type != "dir" || p.Path != spec.PoolPath) {
		return fmt.Errorf("%w: pool %s is incompatible; existing pools are never repointed", ErrConflict, spec.Pool)
	}
	subnet, _ := netip.ParsePrefix(spec.Subnet)
	for _, other := range state.OtherNetworks {
		p, err := netip.ParsePrefix(other.Subnet)
		if err != nil {
			continue
		}
		if subnet.Overlaps(p) {
			return fmt.Errorf("%w: subnet overlaps network %s", ErrConflict, other.Name)
		}
	}
	for _, other := range state.OtherPools {
		if path.Clean(other.Path) == spec.PoolPath {
			return fmt.Errorf("%w: directory is already assigned to pool %s", ErrConflict, other.Name)
		}
	}
	return nil
}
func validatePreparationIdentity(plan *HostPreparationPlan, state HostPreparationState) error {
	if err := compatiblePreparation(plan.Spec, state); err != nil {
		return err
	}
	n, p := state.Network, state.Pool
	if n.Exists && n.UUID != plan.Spec.NetworkUUID {
		return fmt.Errorf("%w: network identity changed after review", ErrConflict)
	}
	if p.Exists && p.UUID != plan.Spec.PoolUUID {
		return fmt.Errorf("%w: pool identity changed after review", ErrConflict)
	}
	if plan.Before.Network.Exists && (!n.Exists || n.ConfigDigest != plan.Before.Network.ConfigDigest) {
		return fmt.Errorf("%w: reviewed network configuration changed or disappeared", ErrConflict)
	}
	if plan.Before.Pool.Exists && (!p.Exists || p.ConfigDigest != plan.Before.Pool.ConfigDigest) {
		return fmt.Errorf("%w: reviewed pool configuration changed or disappeared", ErrConflict)
	}
	if plan.Before.Network.Active && !n.Active || plan.Before.Network.Autostart && !n.Autostart || plan.Before.Pool.Active && !p.Active || plan.Before.Pool.Autostart && !p.Autostart {
		return fmt.Errorf("%w: previously ready resource state changed; prepare a new plan", ErrConflict)
	}
	return nil
}
func preparationActionDone(a string, state HostPreparationState) bool {
	switch a {
	case "create-network":
		return state.Network.Exists
	case "start-network":
		return state.Network.Active
	case "autostart-network":
		return state.Network.Autostart
	case "create-pool":
		return state.Pool.Exists
	case "start-pool":
		return state.Pool.Active
	case "autostart-pool":
		return state.Pool.Autostart
	}
	return false
}
func (s *HostPreparationService) Apply(ctx context.Context, p *HostPreparationPlan) error {
	if p == nil || p.Node == "" {
		return fmt.Errorf("%w: missing host preparation plan", ErrInvalid)
	}
	if err := p.Spec.Validate(); err != nil {
		return err
	}
	if _, err := uuid.Parse(p.Spec.NetworkUUID); err != nil {
		return fmt.Errorf("%w: missing prepared network identity", ErrInvalid)
	}
	if _, err := uuid.Parse(p.Spec.PoolUUID); err != nil {
		return fmt.Errorf("%w: missing prepared pool identity", ErrInvalid)
	}
	if !reflect.DeepEqual(p.Actions, preparationActions(p.Before)) {
		return fmt.Errorf("%w: reviewed action set was changed", ErrConflict)
	}
	for _, a := range p.Actions {
		if err := ctx.Err(); err != nil {
			return err
		}
		state, err := s.repo.Observe(ctx, p.Node, p.Spec)
		if err != nil {
			return err
		}
		if err := validatePreparationIdentity(p, state); err != nil {
			return err
		}
		if preparationActionDone(a, state) {
			continue
		}
		if err := s.repo.Execute(ctx, p.Node, p.Spec, a); err != nil {
			return err
		}
		state, err = s.repo.Observe(ctx, p.Node, p.Spec)
		if err != nil {
			return err
		}
		if err := validatePreparationIdentity(p, state); err != nil {
			return err
		}
		if !preparationActionDone(a, state) {
			return fmt.Errorf("%w: %s did not reach its requested state", ErrUnavailable, a)
		}
	}
	state, err := s.repo.Observe(ctx, p.Node, p.Spec)
	if err != nil {
		return err
	}
	if err := validatePreparationIdentity(p, state); err != nil {
		return err
	}
	if !state.Network.Active || !state.Network.Autostart || !state.Pool.Active || !state.Pool.Autostart {
		return fmt.Errorf("%w: prepared resources are not ready", ErrUnavailable)
	}
	return nil
}
