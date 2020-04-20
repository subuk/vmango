package libvirt

import (
	"encoding/xml"
	"fmt"
	"subuk/vmango/compute"
	"subuk/vmango/util"

	libvirt "github.com/libvirt/libvirt-go"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
	"github.com/rs/zerolog"
)

type NodeRepository struct {
	pool   *ConnectionPool
	logger zerolog.Logger
}

func NewNodeRepository(pool *ConnectionPool, logger zerolog.Logger) *NodeRepository {
	return &NodeRepository{pool: pool, logger: logger}
}

func (repo *NodeRepository) List(options compute.NodeListOptions) ([]*compute.Node, error) {
	nodes := []*compute.Node{}
	for _, nodeId := range repo.pool.Nodes(nil) {
		node, err := repo.Get(nodeId, compute.NodeGetOptions{NoPins: options.NoPins})
		if err != nil {
			repo.logger.Warn().Err(err).Str("node", nodeId).Msg("cannot get node")
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (repo *NodeRepository) Get(nodeId string, options compute.NodeGetOptions) (*compute.Node, error) {
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
		if capsConfig.Host.CPU.Topology != nil {
			node.ThreadsPerCore = capsConfig.Host.CPU.Topology.Threads
		}
	}
	switch capsConfig.Host.CPU.Arch {
	default:
		node.CpuArch = compute.ArchUnknown
	case "x86_64":
		node.CpuArch = compute.ArchAmd64
	}

	nodeCpuMap := map[int]compute.NodeCpu{}
	if capsConfig.Host.NUMA != nil && capsConfig.Host.NUMA.Cells != nil {
		node.Numas = make([]compute.NodeNuma, capsConfig.Host.NUMA.Cells.Num)
		for _, numaInfo := range capsConfig.Host.NUMA.Cells.Cells {
			if numaInfo.Memory != nil {
				node.Numas[numaInfo.ID].Memory = ComputeSizeFromLibvirtSize(numaInfo.Memory.Unit, numaInfo.Memory.Size)
			}
			for _, pageInfo := range numaInfo.PageInfo {
				switch pageInfo.Size {
				case 4:
					node.Numas[numaInfo.ID].Pages4k = pageInfo.Count
				case 2048:
					node.Numas[numaInfo.ID].Pages2m = pageInfo.Count
				case 1048576:
					node.Numas[numaInfo.ID].Pages1g = pageInfo.Count
				}
			}

			if numaInfo.CPUS != nil {
				for _, numaCpuInfo := range numaInfo.CPUS.CPUs {
					cpu := compute.NodeCpu{
						NumaId: numaInfo.ID,
					}
					if numaCpuInfo.SocketID != nil {
						cpu.SocketId = *numaCpuInfo.SocketID
					}
					if numaCpuInfo.CoreID != nil {
						cpu.CoreId = *numaCpuInfo.CoreID
					}
					nodeCpuMap[numaCpuInfo.ID] = cpu
				}
			}
		}
	}
	node.Cpus = make([]compute.NodeCpu, len(nodeCpuMap))
	for idx := 0; idx < len(node.Cpus); idx++ {
		node.Cpus[idx] = nodeCpuMap[idx]
	}

	reqFreePages := []uint64{4, 2048, 1048576}
	freePages, err := conn.GetFreePages(reqFreePages, 0, uint(len(node.Numas)), 0)
	if err != nil {
		return nil, util.NewError(err, "cannot get free memory pages")
	}
	for numaId := 0; numaId < len(node.Numas); numaId++ {
		node.Numas[numaId].Pages4kFree = freePages[numaId*3]
		node.Numas[numaId].Pages2mFree = freePages[numaId*3+1]
		node.Numas[numaId].Pages1gFree = freePages[numaId*3+2]
	}

	if options.NoPins {
		return node, nil
	}

	virDomains, err := conn.ListAllDomains(0)
	if err != nil {
		return nil, util.NewError(err, "cannot list node domains")
	}
	for _, virDomain := range virDomains {
		virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
		if err != nil {
			return nil, util.NewError(err, "cannot get domain xml")
		}
		virDomainConfig := &libvirtxml.Domain{}
		if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
			return nil, util.NewError(err, "cannot unmarshal domain xml")
		}
		if virDomainConfig.CPUTune == nil {
			for cpuId := range node.Cpus {
				node.Cpus[cpuId].Pins = append(node.Cpus[cpuId].Pins, compute.NodeCpuPin{
					Desc: "all",
					VmId: virDomainConfig.Name,
				})
			}
			continue
		}
		if virDomainConfig.CPUTune.VCPUPin == nil {
			for cpuId := range node.Cpus {
				node.Cpus[cpuId].Pins = append(node.Cpus[cpuId].Pins, compute.NodeCpuPin{
					Desc: "vcpus",
					VmId: virDomainConfig.Name,
				})
			}
			continue
		}

		if virDomainConfig.CPUTune.EmulatorPin == nil {
			for cpuId := range node.Cpus {
				node.Cpus[cpuId].Pins = append(node.Cpus[cpuId].Pins, compute.NodeCpuPin{
					Desc: "emulator",
					VmId: virDomainConfig.Name,
				})
			}
			continue
		}

		affinity := ParseCpuAffinity(virDomainConfig.CPUTune.EmulatorPin.CPUSet)
		for _, cpuId := range affinity {
			node.Cpus[cpuId].Pins = append(node.Cpus[cpuId].Pins, compute.NodeCpuPin{
				Desc: "emulator",
				VmId: virDomainConfig.Name,
			})
		}

		for _, vcpupin := range virDomainConfig.CPUTune.VCPUPin {
			vcpuAffinity := ParseCpuAffinity(vcpupin.CPUSet)
			for _, cpuId := range vcpuAffinity {
				node.Cpus[cpuId].Pins = append(node.Cpus[cpuId].Pins, compute.NodeCpuPin{
					Desc: fmt.Sprintf("vcpu-%d", vcpupin.VCPU),
					VmId: virDomainConfig.Name,
				})
			}
		}
	}
	if options.CpuNumaIdFilter {
		newCpus := []compute.NodeCpu{}
		for _, cpu := range node.Cpus {
			if cpu.NumaId == options.CpuNumaId {
				newCpus = append(newCpus, cpu)
			} else {
				newCpus = append(newCpus, compute.NodeCpu{SocketId: -1, CoreId: -1, NumaId: -1})
			}
		}
		node.Cpus = newCpus
	}
	return node, nil
}
