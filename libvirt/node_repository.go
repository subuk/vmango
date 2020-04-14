package libvirt

import (
	"encoding/xml"
	"subuk/vmango/compute"
	"subuk/vmango/util"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
)

type NodeRepository struct {
	pool *ConnectionPool
}

func NewNodeRepository(pool *ConnectionPool) *NodeRepository {
	return &NodeRepository{pool: pool}
}

func (repo *NodeRepository) List() ([]*compute.Node, error) {
	nodes := []*compute.Node{}
	for _, node := range repo.pool.Nodes() {
		node, err := repo.Get(node)
		if err != nil {
			return nil, util.NewError(err, "cannot get node")
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (repo *NodeRepository) Get(nodeId string) (*compute.Node, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)

	sysinfoXml, err := conn.GetSysinfo(0)
	if err != nil {
		return nil, util.NewError(err, "cannot get sysinfo")
	}
	sysinfoConfig := &libvirtxml.DomainSysInfo{}
	if err := xml.Unmarshal([]byte(sysinfoXml), sysinfoConfig); err != nil {
		return nil, util.NewError(err, "cannot parse sysinfo")
	}

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
	node := &compute.Node{
		Id:       nodeId,
		Hostname: hostname,
		Numas:    map[int]compute.NodeNuma{},
	}
	if len(sysinfoConfig.Processor) > 0 {
		for _, entry := range sysinfoConfig.Processor[0].Entry {
			if entry.Name == "version" {
				node.CpuInfo = entry.Value
			}
		}
	}
	if capsConfig.Host.IOMMU != nil {
		node.Iommu = capsConfig.Host.IOMMU.Support == "yes"
	}
	if capsConfig.Host.CPU != nil {
		node.CpuVendor = capsConfig.Host.CPU.Vendor
		node.CpuModel = capsConfig.Host.CPU.Model
	}
	switch capsConfig.Host.CPU.Arch {
	default:
		node.CpuArch = compute.ArchUnknown
	case "x86_64":
		node.CpuArch = compute.ArchAmd64
	}
	if capsConfig.Host.NUMA != nil && capsConfig.Host.NUMA.Cells != nil {
		for _, numaInfo := range capsConfig.Host.NUMA.Cells.Cells {
			var numa compute.NodeNuma
			if existingNuma, exists := node.Numas[numaInfo.ID]; exists {
				numa = existingNuma
			} else {
				numa = compute.NodeNuma{Cores: map[int]compute.NodeNumaCore{}}
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
			node.Numas[numaInfo.ID] = numa
		}
	}
	return node, nil
}
