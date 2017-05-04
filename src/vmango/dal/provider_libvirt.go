package dal

import (
	"fmt"
	"github.com/libvirt/libvirt-go"
	text_template "text/template"
	"vmango/cfg"
)

type LibvirtProvider struct {
	name     string
	conn     *libvirt.Connect
	machines Machinerep
	images   Imagerep
}

func NewLibvirtProvider(conf cfg.HypervisorConfig) (*LibvirtProvider, error) {
	vmtpl, err := text_template.ParseFiles(conf.VmTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse machine template %s: %s", conf.VmTemplate, err)
	}
	voltpl, err := text_template.ParseFiles(conf.VolTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to parse volume template %s: %s", conf.VolTemplate, err)
	}
	virtConn, err := libvirt.NewConnect(conf.Url)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to libvirt: %s", err)
	}
	machinerep, err := NewLibvirtMachinerep(
		virtConn, vmtpl, voltpl, conf.Network,
		conf.RootStoragePool, conf.Name,
		conf.IgnoreVms,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize machinerep: %s", err)
	}
	imagerep := NewLibvirtImagerep(virtConn, conf.ImageStoragePool, conf.Name)
	provider := &LibvirtProvider{
		name:     conf.Name,
		conn:     virtConn,
		machines: machinerep,
		images:   imagerep,
	}
	return provider, nil
}

func (p *LibvirtProvider) Name() string {
	return p.name
}

func (p *LibvirtProvider) Images() Imagerep {
	return p.images
}

func (p *LibvirtProvider) Machines() Machinerep {
	return p.machines
}

func (p *LibvirtProvider) String() string {
	return p.name
}
