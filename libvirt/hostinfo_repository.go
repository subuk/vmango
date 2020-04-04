package libvirt

import (
	"subuk/vmango/compute"
	"subuk/vmango/util"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

type HostInfoRepository struct {
	pool *ConnectionPool
}

func NewHostInfoRepository(pool *ConnectionPool) *HostInfoRepository {
	return &HostInfoRepository{pool: pool}
}

func (repo *HostInfoRepository) Get() (*compute.HostInfo, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(conn)

	capsXml, err := conn.GetCapabilities()
	if err != nil {
		return nil, util.NewError(err, "cannot fetch host capabilities")
	}
	capsConfig := &libvirtxml.Caps{}
	if err := capsConfig.Unmarshal(capsXml); err != nil {
		return nil, util.NewError(err, "cannot parse capabilities")
	}
	hostname, err := conn.GetHostname()
	if err != nil {
		return nil, util.NewError(err, "cannot get hostname")
	}
	hostInfo := &compute.HostInfo{
		Hostname: hostname,
		Numas:    map[int]compute.HostInfoNuma{},
	}
	if capsConfig.Host.IOMMU != nil {
		hostInfo.Iommu = capsConfig.Host.IOMMU.Support == "yes"
	}
	if capsConfig.Host.CPU != nil {
		hostInfo.CpuVendor = capsConfig.Host.CPU.Vendor
		hostInfo.CpuModel = capsConfig.Host.CPU.Model
	}
	switch capsConfig.Host.CPU.Arch {
	default:
		hostInfo.CpuArch = compute.ArchUnknown
	case "x86_64":
		hostInfo.CpuArch = compute.ArchAmd64
	}
	if capsConfig.Host.NUMA != nil && capsConfig.Host.NUMA.Cells != nil {
		for _, numaInfo := range capsConfig.Host.NUMA.Cells.Cells {
			var numa compute.HostInfoNuma
			if existingNuma, exists := hostInfo.Numas[numaInfo.ID]; exists {
				numa = existingNuma
			} else {
				numa = compute.HostInfoNuma{Cores: map[int]compute.HostInfoNumaCore{}}
			}
			if numaInfo.Memory != nil {
				numa.Memory = ComputeSizeFromLibvirtSize(numaInfo.Memory.Unit, numaInfo.Memory.Size)
			}
			for _, pageInfo := range numaInfo.PageInfo {
				switch pageInfo.Size {
				case 4:
					numa.Pages4k = pageInfo.Count
				case 2048:
					numa.Pages2m = pageInfo.Count
				case 1048576:
					numa.Pages1g = pageInfo.Count
				}
			}
			if numaInfo.CPUS != nil {
				for _, cpu := range numaInfo.CPUS.CPUs {
					if cpu.CoreID != nil {
						core := numa.Cores[*cpu.CoreID]
						core.CpuIds = append(core.CpuIds, cpu.ID)
						if cpu.SocketID != nil {
							core.SocketId = *cpu.SocketID
						}
						numa.Cores[*cpu.CoreID] = core
					}
				}
			}
			hostInfo.Numas[numaInfo.ID] = numa
		}
	}
	return hostInfo, nil
}
