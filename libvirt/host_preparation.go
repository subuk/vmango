package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"subuk/vmango/compute"

	vir "libvirt.org/go/libvirt"
	xmlmodel "libvirt.org/go/libvirtxml"
)

type HostPreparationRepository struct{ pool *ConnectionPool }

func NewHostPreparationRepository(pool *ConnectionPool) *HostPreparationRepository {
	return &HostPreparationRepository{pool: pool}
}

func (r *HostPreparationRepository) Observe(ctx context.Context, node string, spec compute.HostPreparationSpec) (compute.HostPreparationState, error) {
	var result compute.HostPreparationState
	if err := ctx.Err(); err != nil {
		return result, err
	}
	conn, err := r.pool.Acquire(node)
	if err != nil {
		return result, err
	}
	defer r.pool.Release(node)
	networks, err := conn.ListAllNetworks(0)
	if err != nil {
		return result, repositoryError(err, "cannot inspect networks")
	}
	defer func() {
		for i := range networks {
			networks[i].Free()
		}
	}()
	for i := range networks {
		n := &networks[i]
		name, err := n.GetName()
		if err != nil {
			return result, err
		}
		persistent, err := n.IsPersistent()
		if err != nil {
			return result, err
		}
		flags := vir.NetworkXMLFlags(0)
		if persistent {
			flags = vir.NETWORK_XML_INACTIVE
		}
		raw, err := n.GetXMLDesc(flags)
		if err != nil {
			return result, err
		}
		cfg := &xmlmodel.Network{}
		if err = cfg.Unmarshal(raw); err != nil {
			return result, err
		}
		if name != spec.Network {
			for _, ip := range cfg.IPs {
				if subnet, err := preparationNetworkPrefix(ip); err == nil {
					result.OtherNetworks = append(result.OtherNetworks, compute.PreparationNetworkSubnet{Name: name, Subnet: subnet})
				}
			}
			// Include live addresses when a persistent network has pending changes.
			active, err := n.IsActive()
			if err != nil {
				return result, err
			}
			if active && persistent {
				liveRaw, err := n.GetXMLDesc(0)
				if err != nil {
					return result, err
				}
				live := &xmlmodel.Network{}
				if err = live.Unmarshal(liveRaw); err != nil {
					return result, err
				}
				for _, ip := range live.IPs {
					if subnet, err := preparationNetworkPrefix(ip); err == nil {
						result.OtherNetworks = append(result.OtherNetworks, compute.PreparationNetworkSubnet{Name: name, Subnet: subnet})
					}
				}
			}
			continue
		}
		state := preparationNetworkState(cfg)
		state.Exists = true
		state.Persistent = persistent
		state.UUID, err = n.GetUUIDString()
		if err != nil {
			return result, err
		}
		state.Active, err = n.IsActive()
		if err != nil {
			return result, err
		}
		state.Autostart, err = n.GetAutostart()
		if err != nil {
			return result, err
		}
		state.LiveCompatible = true
		if state.Active && persistent {
			liveRaw, err := n.GetXMLDesc(0)
			if err != nil {
				return result, err
			}
			live := &xmlmodel.Network{}
			if err = live.Unmarshal(liveRaw); err != nil {
				return result, err
			}
			state.LiveCompatible = preparationNetworkState(live).ConfigDigest == state.ConfigDigest
		}
		result.Network = state
	}
	pools, err := conn.ListAllStoragePools(0)
	if err != nil {
		return result, repositoryError(err, "cannot inspect pools")
	}
	defer func() {
		for i := range pools {
			pools[i].Free()
		}
	}()
	for i := range pools {
		p := &pools[i]
		name, err := p.GetName()
		if err != nil {
			return result, err
		}
		persistent, err := p.IsPersistent()
		if err != nil {
			return result, err
		}
		flags := vir.StorageXMLFlags(0)
		if persistent {
			flags = vir.STORAGE_XML_INACTIVE
		}
		raw, err := p.GetXMLDesc(flags)
		if err != nil {
			return result, err
		}
		cfg := &xmlmodel.StoragePool{}
		if err = cfg.Unmarshal(raw); err != nil {
			return result, err
		}
		if name != spec.Pool {
			if cfg.Target != nil {
				result.OtherPools = append(result.OtherPools, compute.PreparationPoolPath{Name: name, Path: cfg.Target.Path})
			}
			active, err := p.IsActive()
			if err != nil {
				return result, err
			}
			if active && persistent {
				liveRaw, err := p.GetXMLDesc(0)
				if err != nil {
					return result, err
				}
				live := &xmlmodel.StoragePool{}
				if err = live.Unmarshal(liveRaw); err != nil {
					return result, err
				}
				if live.Target != nil {
					result.OtherPools = append(result.OtherPools, compute.PreparationPoolPath{Name: name, Path: live.Target.Path})
				}
			}
			continue
		}
		state := preparationPoolState(cfg)
		state.Exists = true
		state.Persistent = persistent
		state.UUID, err = p.GetUUIDString()
		if err != nil {
			return result, err
		}
		state.Active, err = p.IsActive()
		if err != nil {
			return result, err
		}
		state.Autostart, err = p.GetAutostart()
		if err != nil {
			return result, err
		}
		state.LiveCompatible = true
		if state.Active && persistent {
			liveRaw, err := p.GetXMLDesc(0)
			if err != nil {
				return result, err
			}
			live := &xmlmodel.StoragePool{}
			if err = live.Unmarshal(liveRaw); err != nil {
				return result, err
			}
			state.LiveCompatible = preparationPoolState(live).ConfigDigest == state.ConfigDigest
		}
		result.Pool = state
	}
	return result, ctx.Err()
}

func preparationNetworkPrefix(ip xmlmodel.NetworkIP) (string, error) {
	address, err := netip.ParseAddr(ip.Address)
	if err != nil {
		return "", err
	}
	bits := int(ip.Prefix)
	if ip.Netmask != "" {
		mask := net.ParseIP(ip.Netmask).To4()
		if mask == nil {
			return "", fmt.Errorf("invalid network mask")
		}
		ones, size := net.IPMask(mask).Size()
		if size != 32 {
			return "", fmt.Errorf("invalid network mask")
		}
		bits = ones
	}
	p := netip.PrefixFrom(address, bits)
	if !p.IsValid() {
		return "", fmt.Errorf("invalid network prefix")
	}
	return p.Masked().String(), nil
}
func preparationDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
func preparationNetworkState(cfg *xmlmodel.Network) compute.PreparationNetworkState {
	state := compute.PreparationNetworkState{UUID: cfg.UUID, ShapeCompatible: len(cfg.IPs) == 1}
	if cfg.Forward != nil {
		state.Mode = cfg.Forward.Mode
	}
	if len(cfg.IPs) == 1 {
		ip := cfg.IPs[0]
		state.Gateway = ip.Address
		state.Subnet, _ = preparationNetworkPrefix(ip)
		if ip.DHCP != nil {
			if len(ip.DHCP.Ranges) > 1 {
				state.ShapeCompatible = false
			}
			if len(ip.DHCP.Ranges) == 1 {
				state.DHCPStart = ip.DHCP.Ranges[0].Start
				state.DHCPEnd = ip.DHCP.Ranges[0].End
			}
		}
	}
	// libvirt may allocate a bridge MAC during activation. It is not a requested
	// network policy and must not invalidate an otherwise unchanged reviewed plan.
	copy := *cfg
	copy.MAC = nil
	// On activation libvirt expands an omitted NAT port policy to its default
	// 1024..65535 range in live XML, while retaining the shorter persistent XML.
	// Compare those equivalent representations without discarding custom policy.
	if cfg.Forward != nil && cfg.Forward.Mode == "nat" && cfg.Forward.NAT != nil {
		forward := *cfg.Forward
		nat := *cfg.Forward.NAT
		if len(nat.Ports) == 1 && nat.Ports[0].Start == 1024 && nat.Ports[0].End == 65535 {
			nat.Ports = nil
		}
		forward.NAT = &nat
		if nat.IPv6 == "" && len(nat.Addresses) == 0 && len(nat.Ports) == 0 {
			forward.NAT = nil
		}
		copy.Forward = &forward
	}
	canonical, _ := copy.Marshal()
	state.ConfigDigest = preparationDigest(canonical)
	return state
}
func preparationPoolState(cfg *xmlmodel.StoragePool) compute.PreparationPoolState {
	state := compute.PreparationPoolState{UUID: cfg.UUID, Type: cfg.Type}
	if cfg.Target != nil {
		state.Path = cfg.Target.Path
	}
	copy := *cfg
	copy.Capacity = nil
	copy.Allocation = nil
	copy.Available = nil
	if cfg.Target != nil {
		target := *cfg.Target
		target.Timestamps = nil
		target.Permissions = nil
		copy.Target = &target
	}
	canonical, _ := copy.Marshal()
	state.ConfigDigest = preparationDigest(canonical)
	return state
}
func preparationNetworkXML(spec compute.HostPreparationSpec) (string, error) {
	prefix, err := netip.ParsePrefix(spec.Subnet)
	if err != nil {
		return "", err
	}
	ip := xmlmodel.NetworkIP{Address: spec.Gateway(), Prefix: uint(prefix.Bits())}
	if spec.DHCPStart != "" {
		ip.DHCP = &xmlmodel.NetworkDHCP{Ranges: []xmlmodel.NetworkDHCPRange{{Start: spec.DHCPStart, End: spec.DHCPEnd}}}
	}
	cfg := &xmlmodel.Network{Name: spec.Network, UUID: spec.NetworkUUID, Forward: &xmlmodel.NetworkForward{Mode: "nat"}, IPs: []xmlmodel.NetworkIP{ip}}
	return cfg.Marshal()
}
func preparationPoolXML(spec compute.HostPreparationSpec) (string, error) {
	cfg := &xmlmodel.StoragePool{Type: "dir", Name: spec.Pool, UUID: spec.PoolUUID, Target: &xmlmodel.StoragePoolTarget{Path: spec.PoolPath}}
	return cfg.Marshal()
}

func (r *HostPreparationRepository) Execute(ctx context.Context, node string, spec compute.HostPreparationSpec, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := spec.Validate(); err != nil {
		return err
	}
	conn, err := r.pool.Acquire(node)
	if err != nil {
		return err
	}
	defer r.pool.Release(node)
	switch action {
	case "create-network":
		existing, err := conn.LookupNetworkByName(spec.Network)
		if err == nil {
			defer existing.Free()
			id, e := existing.GetUUIDString()
			if e != nil {
				return e
			}
			if id == spec.NetworkUUID {
				return nil
			}
			return fmt.Errorf("%w: network name became occupied", compute.ErrConflict)
		}
		if !errors.Is(err, vir.ERR_NO_NETWORK) {
			return err
		}
		raw, err := preparationNetworkXML(spec)
		if err != nil {
			return err
		}
		created, err := conn.NetworkDefineXML(raw)
		if err != nil {
			return err
		}
		return created.Free()
	case "start-network", "autostart-network":
		n, err := conn.LookupNetworkByName(spec.Network)
		if err != nil {
			return err
		}
		defer n.Free()
		id, err := n.GetUUIDString()
		if err != nil {
			return err
		}
		if id != spec.NetworkUUID {
			return fmt.Errorf("%w: network identity changed", compute.ErrConflict)
		}
		if action == "start-network" {
			return n.Create()
		}
		return n.SetAutostart(true)
	case "create-pool":
		existing, err := conn.LookupStoragePoolByName(spec.Pool)
		if err == nil {
			defer existing.Free()
			id, e := existing.GetUUIDString()
			if e != nil {
				return e
			}
			if id == spec.PoolUUID {
				return nil
			}
			return fmt.Errorf("%w: pool name became occupied", compute.ErrConflict)
		}
		if !errors.Is(err, vir.ERR_NO_STORAGE_POOL) {
			return err
		}
		raw, err := preparationPoolXML(spec)
		if err != nil {
			return err
		}
		created, err := conn.StoragePoolDefineXML(raw, 0)
		if err != nil {
			return err
		}
		return created.Free()
	case "start-pool", "autostart-pool":
		p, err := conn.LookupStoragePoolByName(spec.Pool)
		if err != nil {
			return err
		}
		defer p.Free()
		id, err := p.GetUUIDString()
		if err != nil {
			return err
		}
		if id != spec.PoolUUID {
			return fmt.Errorf("%w: pool identity changed", compute.ErrConflict)
		}
		// Directory preparation belongs to the reviewed OS stage. Never build,
		// format, delete or recursively change an existing storage location here.
		if action == "start-pool" {
			return p.Create(0)
		}
		return p.SetAutostart(true)
	default:
		return fmt.Errorf("%w: unsupported host preparation action", compute.ErrInvalid)
	}
}
