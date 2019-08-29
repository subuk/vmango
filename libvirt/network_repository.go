package libvirt

import (
	"subuk/vmango/compute"
	"subuk/vmango/util"

	"github.com/libvirt/libvirt-go"

	"github.com/libvirt/libvirt-go-xml"
)

type NetworkRepository struct {
	pool    *ConnectionPool
	bridges []*compute.Network
}

func NewNetworkRepository(pool *ConnectionPool, bridgeNames []string) *NetworkRepository {
	bridges := []*compute.Network{}
	for _, name := range bridgeNames {
		bridge := &compute.Network{
			Type: compute.NetworkTypeBridge,
			Name: name,
		}
		bridges = append(bridges, bridge)
	}
	return &NetworkRepository{pool: pool, bridges: bridges}
}

func (repo *NetworkRepository) virNetworkToNetwork(virNetwork *libvirt.Network) (*compute.Network, error) {
	virNetworkXml, err := virNetwork.GetXMLDesc(0)
	if err != nil {
		return nil, util.NewError(err, "cannot get network xml")
	}
	virNetworkConfig := &libvirtxml.Network{}
	if err := virNetworkConfig.Unmarshal(virNetworkXml); err != nil {
		return nil, util.NewError(err, "cannot parse network xml")
	}
	network := &compute.Network{
		Name: virNetworkConfig.Name,
		Type: compute.NetworkTypeLibvirt,
	}
	return network, nil
}

func (repo *NetworkRepository) Get(name string) (*compute.Network, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	for _, bridge := range repo.bridges {
		if bridge.Name == name {
			return bridge, nil
		}
	}
	virNetwork, err := conn.LookupNetworkByName(name)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup network")
	}
	network, err := repo.virNetworkToNetwork(virNetwork)
	if err != nil {
		return nil, err
	}
	return network, nil
}

func (repo *NetworkRepository) List() ([]*compute.Network, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virNetworks, err := conn.ListAllNetworks(0)
	if err != nil {
		return nil, util.NewError(err, "list networks failed")
	}
	networks := []*compute.Network{}
	networks = append(networks, repo.bridges...)
	for _, virNetwork := range virNetworks {
		network, err := repo.virNetworkToNetwork(&virNetwork)
		if err != nil {
			return nil, err
		}
		networks = append(networks, network)
	}
	return networks, nil
}
