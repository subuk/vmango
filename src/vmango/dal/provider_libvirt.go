package dal

import (
	"encoding/xml"
	"fmt"
	"github.com/libvirt/libvirt-go"
	text_template "text/template"
	"vmango/cfg"
	"vmango/models"
)

const LIBVIRT_PROVIDER_TYPE = "libvirt"

type LibvirtProvider struct {
	name        string
	storagePool string
	conn        *libvirt.Connect
	machines    Machinerep
	images      Imagerep
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
	imagerep := NewLibvirtImagerep(virtConn, conf.ImageStoragePool)
	provider := &LibvirtProvider{
		name:        conf.Name,
		conn:        virtConn,
		machines:    machinerep,
		storagePool: conf.RootStoragePool,
		images:      imagerep,
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

func (p *LibvirtProvider) Status(status *models.StatusInfo) error {
	// Basic info
	status.Name = p.Name()
	status.Type = LIBVIRT_PROVIDER_TYPE

	// Description
	hostname, err := p.conn.GetHostname()
	if err != nil {
		return err
	}

	status.Description = fmt.Sprintf("KVM hypervisor %s", hostname)

	// Connection
	libvirtURI, err := p.conn.GetURI()
	if err != nil {
		return err
	}
	status.Connection = libvirtURI

	// Storage info
	vmPool, err := p.conn.LookupStoragePoolByName(p.storagePool)
	if err != nil {
		return err
	}
	vmPoolXMLString, err := vmPool.GetXMLDesc(0)
	if err != nil {
		return err
	}
	vmPoolConfig := struct {
		Capacity   uint64 `xml:"capacity"`
		Availaible uint64 `xml:"available"`
		Allocation uint64 `xml:"allocation"`
	}{}
	if err := xml.Unmarshal([]byte(vmPoolXMLString), &vmPoolConfig); err != nil {
		return err
	}
	status.Storage.Total = vmPoolConfig.Capacity
	status.Storage.Usage = int((float64(vmPoolConfig.Allocation) / float64(vmPoolConfig.Capacity)) * 100)

	// Memory info
	memStat, err := p.conn.GetMemoryStats(libvirt.NODE_MEMORY_STATS_ALL_CELLS, 0)
	if err != nil {
		return err
	}
	memTotal := memStat.Total * 1024
	memFree := (memStat.Free + memStat.Buffers + memStat.Cached) * 1024
	memUsed := (memTotal - memFree)
	memUsedPercent := int((float32(memUsed) / float32(memTotal)) * 100)

	status.Memory.Total = memTotal
	status.Memory.Usage = memUsedPercent

	// Machine count
	machines := models.VirtualMachineList{}
	if err := p.Machines().List(&machines); err != nil {
		return fmt.Errorf("failed to count machines: %s", err)
	}
	status.MachineCount = machines.Count()
	return nil
}
