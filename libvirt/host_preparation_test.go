package libvirt

import (
	xmlmodel "libvirt.org/go/libvirtxml"
	"strings"
	"subuk/vmango/compute"
	"testing"
)

func TestHostPreparationXMLAndObservation(t *testing.T) {
	s := compute.HostPreparationSpec{Network: "isolated-test", Subnet: "192.168.174.0/24", DHCPStart: "192.168.174.20", DHCPEnd: "192.168.174.200", Pool: "test-dir", PoolPath: "/var/lib/test-dir", NetworkUUID: "90c07808-b401-4879-ade9-44254c6a0cf7", PoolUUID: "1410e169-b5f1-4449-9f97-ef7ae9bc5290"}
	raw, err := preparationNetworkXML(s)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &xmlmodel.Network{}
	if err := cfg.Unmarshal(raw); err != nil {
		t.Fatal(err)
	}
	n := preparationNetworkState(cfg)
	if n.UUID != s.NetworkUUID || n.Subnet != s.Subnet || n.Gateway != "192.168.174.1" || n.Mode != "nat" || n.DHCPStart != s.DHCPStart || n.DHCPEnd != s.DHCPEnd || !n.ShapeCompatible {
		t.Fatalf("incorrect network: %+v", n)
	}
	raw, err = preparationPoolXML(s)
	if err != nil {
		t.Fatal(err)
	}
	pool := &xmlmodel.StoragePool{}
	if err := pool.Unmarshal(raw); err != nil {
		t.Fatal(err)
	}
	p := preparationPoolState(pool)
	if p.UUID != s.PoolUUID || p.Type != "dir" || p.Path != s.PoolPath {
		t.Fatal(p)
	}
	if strings.Contains(raw, "permissions") || strings.Contains(raw, "device") {
		t.Fatal("pool XML manages unrelated storage settings")
	}
}
func TestHostPreparationXMLDigestIgnoresRuntimeAllocation(t *testing.T) {
	pool := &xmlmodel.StoragePool{Type: "dir", Name: "data", Target: &xmlmodel.StoragePoolTarget{Path: "/data"}}
	before := preparationPoolState(pool)
	pool.Capacity = &xmlmodel.StoragePoolSize{Value: 1000}
	pool.Allocation = &xmlmodel.StoragePoolSize{Value: 20}
	pool.Target.Permissions = &xmlmodel.StoragePoolTargetPermissions{Mode: "0755"}
	if before.ConfigDigest != preparationPoolState(pool).ConfigDigest {
		t.Fatal("runtime allocation changed persistent digest")
	}
	pool.Target.Path = "/other"
	if before.ConfigDigest == preparationPoolState(pool).ConfigDigest {
		t.Fatal("path change not detected")
	}
}
func TestHostPreparationNetworkNetmaskAndUnsupportedShape(t *testing.T) {
	n := &xmlmodel.Network{Forward: &xmlmodel.NetworkForward{Mode: "nat"}, IPs: []xmlmodel.NetworkIP{{Address: "192.168.122.1", Netmask: "255.255.255.0", DHCP: &xmlmodel.NetworkDHCP{Ranges: []xmlmodel.NetworkDHCPRange{{Start: "192.168.122.2", End: "192.168.122.254"}}}}}}
	observed := preparationNetworkState(n)
	if observed.Subnet != "192.168.122.0/24" || !observed.ShapeCompatible {
		t.Fatal(observed)
	}
	n.IPs = append(n.IPs, xmlmodel.NetworkIP{Address: "fd00::1", Prefix: 64})
	if preparationNetworkState(n).ShapeCompatible {
		t.Fatal("multiple subnets silently accepted")
	}
}

func TestHostPreparationRecognizesLibvirtLiveNATDefaults(t *testing.T) {
	// Persistent and live forms observed on AlmaLinux 10.2 after the reviewed
	// network was defined and activated. The UUID must remain part of the digest.
	persistent := `<network>
  <name>vmango-krg110-test</name>
  <uuid>c1de0d38-3098-473f-ac5d-a1320b8e2c91</uuid>
  <forward mode='nat'/>
  <bridge name='virbr0' stp='on' delay='0'/>
  <mac address='52:54:00:65:97:bb'/>
  <ip address='192.168.177.1' prefix='24'>
    <dhcp><range start='192.168.177.2' end='192.168.177.254'/></dhcp>
  </ip>
</network>`
	live := strings.Replace(persistent, `<forward mode='nat'/>`, `<forward mode='nat'><nat><port start='1024' end='65535'/></nat></forward>`, 1)
	parse := func(s string) *xmlmodel.Network {
		t.Helper()
		cfg := &xmlmodel.Network{}
		if err := cfg.Unmarshal(s); err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	desired := preparationNetworkState(parse(persistent))
	liveConfig := parse(live)
	actual := preparationNetworkState(liveConfig)
	if desired.ConfigDigest != actual.ConfigDigest || actual.Subnet != "192.168.177.0/24" || !actual.ShapeCompatible {
		t.Fatalf("libvirt runtime defaults conflict: before=%+v live=%+v", desired, actual)
	}
	if len(liveConfig.Forward.NAT.Ports) != 1 {
		t.Fatal("digest normalization mutated original XML model")
	}
	for _, different := range []string{
		strings.Replace(live, `start='1024'`, `start='2048'`, 1),
		strings.Replace(live, `<nat>`, `<nat ipv6='yes'>`, 1),
		strings.Replace(live, `<nat>`, `<nat><address start='203.0.113.1' end='203.0.113.2'/>`, 1),
		strings.Replace(live, `192.168.177.254`, `192.168.177.200`, 1),
		strings.Replace(live, `c1de0d38-3098-473f-ac5d-a1320b8e2c91`, `00000000-0000-0000-0000-000000000001`, 1),
	} {
		if preparationNetworkState(parse(different)).ConfigDigest == desired.ConfigDigest {
			t.Fatal("unreviewed configuration or identity change was ignored")
		}
	}
}
