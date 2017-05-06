package dal

import (
	"encoding/xml"
	"fmt"
	"strings"
)

type diskSourceXMLConfig struct {
	File string `xml:"file,attr"`
	Dev  string `xml:"dev,attr"`
}

func (source diskSourceXMLConfig) Path() string {
	if source.File != "" {
		return source.File
	}
	if source.Dev != "" {
		return source.Dev
	}
	return ""
}

type domainDiskXMLConfig struct {
	Device string `xml:"device,attr"`
	Driver struct {
		Name  string `xml:"name,attr"`
		Type  string `xml:"type,attr"`
		Cache string `xml:"cache,attr"`
	} `xml:"driver"`
	Target struct {
		Device string `xml:"dev,attr"`
		Bus    string `xml:"bus,attr"`
	} `xml:"target"`
	Source diskSourceXMLConfig `xml:"source"`
}

type domainXMLConfig struct {
	XMLName xml.Name              `xml:"domain"`
	Name    string                `xml:"name"`
	Disks   []domainDiskXMLConfig `xml:"devices>disk"`
	Os      struct {
		Type struct {
			Arch string `xml:"arch,attr"`
		} `xml:"type"`
	} `xml:"os"`
	Interfaces []struct {
		Type string `xml:"type,attr"`
		Mac  struct {
			Address string `xml:"address,attr"`
		} `xml:"mac"`
	} `xml:"devices>interface"`
	OSName   string `xml:"metadata>md>os"`
	ImageId  string `xml:"metadata>md>imageId"`
	Userdata string `xml:"metadata>md>userdata"`
	SSHKeys  []struct {
		Name   string `xml:"name,attr"`
		Public string `xml:",chardata"`
	} `xml:"metadata>md>sshkeys>key"`
	Graphics []struct {
		Type   string `xml:"type,attr"`
		Port   string `xml:"port,attr"`
		Listen string `xml:"listen,attr"`
	} `xml:"devices>graphics"`
}

func (domain *domainXMLConfig) RootDisk() *domainDiskXMLConfig {
	for _, disk := range domain.Disks {
		if disk.Device == "cdrom" {
			continue
		}
		if strings.HasSuffix(disk.Source.Path(), "_disk") {
			return &disk
		}
	}
	return nil
}

func (domcfg domainXMLConfig) VNCAddr() string {
	for _, g := range domcfg.Graphics {
		if g.Type == "vnc" {
			return fmt.Sprintf("%s:%s", g.Listen, g.Port)
		}
	}
	return ""
}

type netXMLConfig struct {
	XMLName xml.Name `xml:"network"`
	Name    string   `xml:"name"`
	IP      struct {
		Address string `xml:"address,attr"`
		Netmask string `xml:"netmask,attr"`
		Hosts   []struct {
			Name   string `xml:"name,attr"`
			HWAddr string `xml:"mac,attr"`
			IPAddr string `xml:"ip,attr"`
		} `xml:"dhcp>host"`
		DHCPRange struct {
			Start string `xml:"start,attr"`
			End   string `xml:"end,attr"`
		} `xml:"dhcp>range"`
	} `xml:"ip"`
}

func (n netXMLConfig) HasHost(ip string) bool {
	for _, host := range n.IP.Hosts {
		if host.IPAddr == ip {
			return true
		}
	}
	return false
}
