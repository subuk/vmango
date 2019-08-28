package libvirt

import (
	"subuk/vmango/compute"
	"subuk/vmango/util"

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
		networks = append(networks, network)
	}
	return networks, nil
}
