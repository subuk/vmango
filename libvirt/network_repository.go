package libvirt

import (
	"subuk/vmango/compute"
	"subuk/vmango/util"

	"github.com/libvirt/libvirt-go"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

type NetworkRepository struct {
	pool *ConnectionPool
}

func NewNetworkRepository(pool *ConnectionPool) *NetworkRepository {
	return &NetworkRepository{pool: pool}
}

func (repo *NetworkRepository) virNetworkToNetwork(virNetwork *libvirt.Network, nodeId string) (*compute.Network, error) {
	virNetworkXml, err := virNetwork.GetXMLDesc(0)
	if err != nil {
		return nil, util.NewError(err, "cannot get network xml")
	}
	virNetworkConfig := &libvirtxml.Network{}
	if err := virNetworkConfig.Unmarshal(virNetworkXml); err != nil {
		return nil, util.NewError(err, "cannot parse network xml")
	}
	network := &compute.Network{
		NodeId: nodeId,
		Name:   virNetworkConfig.Name,
		Type:   compute.NetworkTypeLibvirt,
	}
	return network, nil
}

func (repo *NetworkRepository) Get(name, nodeId string) (*compute.Network, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	virNetwork, err := conn.LookupNetworkByName(name)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup network")
	}
	network, err := repo.virNetworkToNetwork(virNetwork, nodeId)
	if err != nil {
		return nil, err
	}
	return network, nil
}

func (repo *NetworkRepository) List(options compute.NetworkListOptions) ([]*compute.Network, error) {
	networks := []*compute.Network{}
	for _, nodeId := range repo.pool.Nodes() {
		if options.NodeId != "" && options.NodeId != nodeId {
			continue
		}
		conn, err := repo.pool.Acquire(nodeId)
		if err != nil {
			return nil, util.NewError(err, "cannot acquire connection")
		}
		defer repo.pool.Release(nodeId)

		virNetworks, err := conn.ListAllNetworks(0)
		if err != nil {
			return nil, util.NewError(err, "list networks failed")
		}
		for _, virNetwork := range virNetworks {
			network, err := repo.virNetworkToNetwork(&virNetwork, nodeId)
			if err != nil {
				return nil, err
			}
			networks = append(networks, network)
		}
	}
	return networks, nil
}
