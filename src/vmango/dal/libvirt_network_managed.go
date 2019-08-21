package dal

import (
	"encoding/xml"
	"fmt"
	"vmango/domain"

	"github.com/Sirupsen/logrus"
	libvirt "github.com/libvirt/libvirt-go"
)

type LibvirtManagedNetwork struct {
	conn        *libvirt.Connect
	networkName string
}

func NewLibvirtManagedNetwork(conn *libvirt.Connect, networkName string) *LibvirtManagedNetwork {
	return &LibvirtManagedNetwork{
		conn:        conn,
		networkName: networkName,
	}
}

func (backend *LibvirtManagedNetwork) Name() string {
	return backend.networkName
}

func (backend *LibvirtManagedNetwork) LookupIP(vm *domain.VirtualMachine) error {
	network, err := backend.conn.LookupNetworkByName(backend.networkName)
	if err != nil {
		return err
	}
	networkXMLString, err := network.GetXMLDesc(0)
	if err != nil {
		return err
	}
	networkConfig := netXMLConfig{}
	if err := xml.Unmarshal([]byte(networkXMLString), &networkConfig); err != nil {
		return fmt.Errorf("failed to parse network xml: %s", err)
	}
	for _, host := range networkConfig.IP.Hosts {
		if host.HWAddr == vm.HWAddr {
			vm.Ip = &domain.IP{Address: host.IPAddr}
			return nil
		}
	}
	return nil
}

func (backend *LibvirtManagedNetwork) AssignIP(vm *domain.VirtualMachine) error {
	network, err := backend.conn.LookupNetworkByName(backend.networkName)
	if err != nil {
		return err
	}
	xmlString, err := network.GetXMLDesc(0)
	if err != nil {
		return err
	}
	networkConfig := netXMLConfig{}
	if err := xml.Unmarshal([]byte(xmlString), &networkConfig); err != nil {
		return fmt.Errorf("failed to parse network xml: %s", err)
	}
	addrs, err := listIPRange(
		networkConfig.IP.DHCPRange.Start,
		networkConfig.IP.DHCPRange.End,
		networkConfig.IP.Address,
		networkConfig.IP.Netmask,
	)
	if err != nil {
		return err
	}
	var ip *domain.IP
	for _, addr := range addrs {
		if has := networkConfig.HasHost(addr); !has {
			ip = &domain.IP{Address: addr}
			break
		}
	}
	if ip == nil {
		return fmt.Errorf("failed to find free IP address")
	}

	return network.Update(
		libvirt.NETWORK_UPDATE_COMMAND_ADD_LAST,
		libvirt.NETWORK_SECTION_IP_DHCP_HOST,
		-1,
		fmt.Sprintf(
			`<host mac="%s" name="%s" ip="%s" />`,
			vm.HWAddr, vm.Name, ip.Address,
		),
		libvirt.NETWORK_UPDATE_AFFECT_LIVE|libvirt.NETWORK_UPDATE_AFFECT_CONFIG,
	)
}

func (backend *LibvirtManagedNetwork) ReleaseIP(vm *domain.VirtualMachine) error {
	network, err := backend.conn.LookupNetworkByName(backend.networkName)
	if err != nil {
		return err
	}
	if vm.Ip == nil {
		logrus.WithField("machine", vm.Name).Warn("no ip to release")
		return nil
	}
	return network.Update(
		libvirt.NETWORK_UPDATE_COMMAND_DELETE,
		libvirt.NETWORK_SECTION_IP_DHCP_HOST,
		-1,
		fmt.Sprintf(
			`<host mac="%s" name="%s" ip="%s" />`,
			vm.HWAddr, vm.Name, vm.Ip.Address,
		),
		libvirt.NETWORK_UPDATE_AFFECT_LIVE|libvirt.NETWORK_UPDATE_AFFECT_CONFIG,
	)
}
