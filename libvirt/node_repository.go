package libvirt

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"subuk/vmango/compute"
	"time"

	"github.com/rs/zerolog"
	libvirt "libvirt.org/go/libvirt"
	libvirtxml "libvirt.org/go/libvirtxml"
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
			repo.logger.Warn().Err(administrationFailure(err)).Str("node", nodeId).Msg("cannot get node")
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (repo *NodeRepository) Get(nodeId string, options compute.NodeGetOptions) (*compute.Node, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, repositoryError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)

	capsXml, err := conn.GetCapabilities()
	if err != nil {
		return nil, repositoryError(err, "cannot fetch host capabilities")
	}
	capsConfig := &libvirtxml.Caps{}
	if err := capsConfig.Unmarshal(capsXml); err != nil {
		return nil, repositoryError(err, "cannot parse capabilities")
	}
	hostname, err := conn.GetHostname()
	if err != nil {
		return nil, repositoryError(err, "cannot get hostname")
	}
	sysinfoXml, err := conn.GetSysinfo(0)
	if err != nil {
		// return nil, repositoryError(err, "cannot get sysinfo")
	}
	sysinfoConfig := &libvirtxml.DomainSysInfo{}
	if sysinfoXml != "" {
		if err := xml.Unmarshal([]byte(sysinfoXml), sysinfoConfig); err != nil {
			return nil, repositoryError(err, "cannot parse sysinfo")
		}
	}
	node := &compute.Node{
		Id:       nodeId,
		Hostname: hostname,
	}
	if sysinfoConfig.SMBIOS != nil && len(sysinfoConfig.SMBIOS.Processor) > 0 {
		for _, entry := range sysinfoConfig.SMBIOS.Processor[0].Entry {
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
	case "aarch64":
		node.CpuArch = compute.ArchAarch64
	}

	reqFreePagesMap := map[uint64]struct{}{}
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
					reqFreePagesMap[4] = struct{}{}
				case 2048:
					node.Numas[numaInfo.ID].Pages2m = pageInfo.Count
					reqFreePagesMap[2048] = struct{}{}
				case 1048576:
					node.Numas[numaInfo.ID].Pages1g = pageInfo.Count
					reqFreePagesMap[1048576] = struct{}{}
				}
			}

		}
	}
	online, _, onlineErr := conn.GetCPUMap(0)
	onlineKnown := onlineErr == nil
	if !onlineKnown && runtime.GOOS == "darwin" {
		uri, _ := url.Parse(repo.pool.nodeUri[nodeId])
		if uri != nil && uri.Host == "" && uri.Scheme == "qemu" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			values, e := exec.CommandContext(ctx, "/usr/sbin/sysctl", "-n", "hw.physicalcpu", "hw.logicalcpu").Output()
			cancel()
			fields := strings.Fields(string(values))
			if e == nil && len(fields) == 2 {
				physical, e1 := strconv.Atoi(fields[0])
				logical, e2 := strconv.Atoi(fields[1])
				if e1 == nil && e2 == nil {
					online, onlineKnown = localDarwinOnlineMap(capsConfig, physical, logical)
				}
			}
		}
	}
	node.Cpus = hostCPUTopology(capsConfig, online, onlineKnown)

	if len(reqFreePagesMap) > 0 {
		reqFreePages := []uint64{}
		for pageSize := range reqFreePagesMap {
			reqFreePages = append(reqFreePages, pageSize)
		}
		freePages, err := conn.GetFreePages(reqFreePages, 0, uint(len(node.Numas)), 0)
		if err != nil {
			return nil, repositoryError(err, "cannot get free memory pages")
		}
		for idx, pageCount := range freePages {
			pageSize := reqFreePages[idx%len(reqFreePages)]
			numaId := idx / len(reqFreePages)
			switch pageSize {
			case 4:
				node.Numas[numaId].Pages4kFree = pageCount
			case 2048:
				node.Numas[numaId].Pages2mFree = pageCount
			case 1048576:
				node.Numas[numaId].Pages1gFree = pageCount
			}
		}
	}

	if options.NoPins {
		return node, nil
	}

	virDomains, err := conn.ListAllDomains(0)
	if err != nil {
		return nil, repositoryError(err, "cannot list node domains")
	}
	for _, virDomain := range virDomains {
		virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
		if err != nil {
			return nil, repositoryError(err, "cannot get domain xml")
		}
		virDomainConfig := &libvirtxml.Domain{}
		if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
			return nil, repositoryError(err, "cannot unmarshal domain xml")
		}
		if virDomainConfig.CPUTune == nil {
			for cpuId := range node.Cpus {
				node.Cpus[cpuId].Pins = append(node.Cpus[cpuId].Pins, compute.NodeCpuPin{
					Desc: "all",
					VmId: virDomainConfig.UUID,
				})
			}
			continue
		}
		if virDomainConfig.CPUTune.VCPUPin == nil {
			for cpuId := range node.Cpus {
				node.Cpus[cpuId].Pins = append(node.Cpus[cpuId].Pins, compute.NodeCpuPin{
					Desc: "vcpus",
					VmId: virDomainConfig.UUID,
				})
			}
			continue
		}

		if virDomainConfig.CPUTune.EmulatorPin == nil {
			for cpuId := range node.Cpus {
				node.Cpus[cpuId].Pins = append(node.Cpus[cpuId].Pins, compute.NodeCpuPin{
					Desc: "emulator",
					VmId: virDomainConfig.UUID,
				})
			}
			continue
		}

		affinity := ParseCpuAffinity(virDomainConfig.CPUTune.EmulatorPin.CPUSet)
		for _, cpuId := range affinity {
			if int(cpuId) >= len(node.Cpus) {
				continue
			}
			node.Cpus[cpuId].Pins = append(node.Cpus[cpuId].Pins, compute.NodeCpuPin{
				Desc: "emulator",
				VmId: virDomainConfig.UUID,
			})
		}

		for _, vcpupin := range virDomainConfig.CPUTune.VCPUPin {
			vcpuAffinity := ParseCpuAffinity(vcpupin.CPUSet)
			for _, cpuId := range vcpuAffinity {
				if int(cpuId) >= len(node.Cpus) {
					continue
				}
				node.Cpus[cpuId].Pins = append(node.Cpus[cpuId].Pins, compute.NodeCpuPin{
					Desc: fmt.Sprintf("vcpu-%d", vcpupin.VCPU),
					VmId: virDomainConfig.UUID,
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
