package dal

import (
	"encoding/xml"
	"fmt"
	"vmango/domain"

	libvirt "github.com/libvirt/libvirt-go"
)

const LIBVIRT_PROVIDER_TYPE = "libvirt"

type LibvirtStatusrep struct {
	conn            *libvirt.Connect
	storagePoolName string
}

func NewLibvirtStatusrep(conn *libvirt.Connect, storagePoolName string) *LibvirtStatusrep {
	return &LibvirtStatusrep{
		conn:            conn,
		storagePoolName: storagePoolName,
	}
}

func (repo *LibvirtStatusrep) Fetch(status *domain.StatusInfo) error {
	// Basic info
	status.Type = LIBVIRT_PROVIDER_TYPE

	// Description
	hostname, err := repo.conn.GetHostname()
	if err != nil {
		return err
	}

	status.Description = fmt.Sprintf("KVM hypervisor %s", hostname)

	// Connection
	libvirtURI, err := repo.conn.GetURI()
	if err != nil {
		return err
	}
	status.Connection = libvirtURI

	// Storage info
	vmPool, err := repo.conn.LookupStoragePoolByName(repo.storagePoolName)
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
	memStat, err := repo.conn.GetMemoryStats(libvirt.NODE_MEMORY_STATS_ALL_CELLS, 0)
	if err != nil {
		return err
	}
	memTotal := memStat.Total * 1024
	memFree := (memStat.Free + memStat.Buffers + memStat.Cached) * 1024
	memUsed := (memTotal - memFree)
	memUsedPercent := int((float32(memUsed) / float32(memTotal)) * 100)

	status.Memory.Total = memTotal
	status.Memory.Usage = memUsedPercent

	// MachinesInfo
	domains, err := repo.conn.ListAllDomains(0)
	if err != nil {
		return fmt.Errorf("failed to list domains: %s", err)
	}
	status.MachineCount = len(domains)
	return nil
}
