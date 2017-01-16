package dal

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"github.com/libvirt/libvirt-go"
	"net"
	"vmango/models"
)

type LibvirtIPPool struct {
	conn    *libvirt.Connect
	network string
}

type netIPHost struct {
	Name   string `xml:"name,attr"`
	HWAddr string `xml:"mac,attr"`
	IPAddr string `xml:"ip,attr"`
}

type netDHCPRangeIPConfig struct {
	Start string `xml:"start,attr"`
	End   string `xml:"end,attr"`
}

type netIPConfig struct {
	Address   string               `xml:"address,attr"`
	Netmask   string               `xml:"netmask,attr"`
	Hosts     []netIPHost          `xml:"dhcp>host"`
	DHCPRange netDHCPRangeIPConfig `xml:"dhcp>range"`
}

func (n *netIPConfig) HasHost(ip string) (bool, *netIPHost) {
	for _, host := range n.Hosts {
		if host.IPAddr == ip {
			return true, &host
		}
	}
	return false, nil
}

type netXMLConfig struct {
	XMLName xml.Name    `xml:"network"`
	Name    string      `xml:"name"`
	IP      netIPConfig `xml:"ip"`
}

func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}

func ipMaskToInt(raw string) int {
	mask := net.IPMask(net.ParseIP(raw).To4())
	size, _ := mask.Size()
	return size
}

func getFirstSubnetIP(rawSubnetIP, rawSubnetMask string) (string, error) {
	ip, ipnet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", rawSubnetIP, ipMaskToInt(rawSubnetMask)))
	if err != nil {
		return "", err
	}
	ip = ip.To4()
	ip[len(ip)-1]++
	return ip.Mask(ipnet.Mask).String(), nil
}

func listIPRange(start, end, rawSubnetIP, rawSubnetMask string) ([]string, error) {
	ip, ipnet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", rawSubnetIP, ipMaskToInt(rawSubnetMask)))
	if err != nil {
		return nil, err
	}
	startIP := net.ParseIP(start)
	endIP := net.ParseIP(end)

	var result []string
	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); incIP(ip) {
		if !(bytes.Compare(ip.To16(), startIP.To16()) >= 0 && bytes.Compare(ip.To16(), endIP.To16()) <= 0) {
			continue
		}
		result = append(result, ip.String())
	}
	return result, nil
}

func NewLibvirtIPPool(conn *libvirt.Connect, network string) *LibvirtIPPool {
	return &LibvirtIPPool{
		conn:    conn,
		network: network,
	}
}

func (pool *LibvirtIPPool) List(ips *models.IPList) error {
	network, err := pool.conn.LookupNetworkByName(pool.network)
	if err != nil {
		return err
	}
	xmlString, err := network.GetXMLDesc(0)
	if err != nil {
		return err
	}
	netConfig := &netXMLConfig{}
	if err := xml.Unmarshal([]byte(xmlString), netConfig); err != nil {
		return fmt.Errorf("failed to parse network xml:", err)
	}
	addrs, err := listIPRange(
		netConfig.IP.DHCPRange.Start,
		netConfig.IP.DHCPRange.End,
		netConfig.IP.Address,
		netConfig.IP.Netmask,
	)
	if err != nil {
		return err
	}
	gw, err := getFirstSubnetIP(netConfig.IP.Address, netConfig.IP.Netmask)
	if err != nil {
		return err
	}
	for _, addr := range addrs {
		usedBy := ""
		if has, host := netConfig.IP.HasHost(addr); has {
			usedBy = host.Name
		}
		ips.Add(&models.IP{
			Address: addr,
			Netmask: ipMaskToInt(netConfig.IP.Netmask),
			Gateway: gw,
			UsedBy:  usedBy,
		})
	}
	return nil
}

func (pool *LibvirtIPPool) Get(ip *models.IP) (bool, error) {
	ips := &models.IPList{}
	if err := pool.List(ips); err != nil {
		return false, err
	}
	for _, testip := range ips.All() {
		if testip.UsedBy == "" {
			*ip = *testip
			return true, nil
		}
	}
	return false, fmt.Errorf("Not supported")
}

func (pool *LibvirtIPPool) Assign(ip *models.IP, vm *models.VirtualMachine) error {
	network, err := pool.conn.LookupNetworkByName(pool.network)
	if err != nil {
		return err
	}
	return network.Update(
		libvirt.NETWORK_UPDATE_COMMAND_ADD_LAST,
		libvirt.NETWORK_SECTION_IP_DHCP_HOST,
		0,
		fmt.Sprintf(
			`<host mac="%s" name="%s" ip="%s" />`,
			vm.HWAddr, vm.Name, ip.Address,
		),
		libvirt.NETWORK_UPDATE_AFFECT_LIVE|libvirt.NETWORK_UPDATE_AFFECT_CONFIG|libvirt.NETWORK_UPDATE_AFFECT_CURRENT,
	)
}
