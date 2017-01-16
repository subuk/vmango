package models

type IP struct {
	Address string
	Gateway string
	Netmask int
	UsedBy  string
}

type IPList struct {
	addresses []*IP
}

func (ips *IPList) All() []*IP {
	return ips.addresses
}

func (ips *IPList) Add(ip *IP) {
	ips.addresses = append(ips.addresses, ip)
}
