package libvirt

import (
	x "libvirt.org/go/libvirtxml"
	"subuk/vmango/compute"
)

// Online CPU IDs are not necessarily dense. Omitted die/cluster IDs are safe
// only when the host explicitly reports at most one of that topology level.
func hostCPUTopology(caps *x.Caps, online map[int]bool, readOK bool) []compute.NodeCpu {
	maxID := -1
	for id := range online {
		maxID = max(maxID, id)
	}
	if caps.Host.NUMA != nil && caps.Host.NUMA.Cells != nil {
		for _, cell := range caps.Host.NUMA.Cells.Cells {
			if cell.CPUS != nil {
				for _, c := range cell.CPUS.CPUs {
					maxID = max(maxID, c.ID)
				}
			}
		}
	}
	if maxID < 0 || maxID > 1048575 {
		return nil
	}
	result := make([]compute.NodeCpu, maxID+1)
	for id := range result {
		isOnline, known := online[id]
		result[id] = compute.NodeCpu{SocketId: -1, CoreId: -1, NumaId: -1, Online: isOnline, TopologyKnown: readOK && known && !isOnline}
	}
	var topology *x.CapsHostCPUTopology
	if caps.Host.CPU != nil {
		topology = caps.Host.CPU.Topology
	}
	seen := map[int]bool{}
	if caps.Host.NUMA != nil && caps.Host.NUMA.Cells != nil {
		for _, cell := range caps.Host.NUMA.Cells.Cells {
			if cell.CPUS == nil {
				continue
			}
			for _, c := range cell.CPUS.CPUs {
				if c.ID < 0 || c.ID > maxID {
					continue
				}
				cpu := &result[c.ID]
				cpu.NumaId = cell.ID
				_, known := online[c.ID]
				cpu.TopologyKnown = readOK && known && !seen[c.ID] && (!cpu.Online || c.SocketID != nil && c.CoreID != nil && (c.DieID != nil || topology != nil && topology.Dies <= 1) && (c.ClusterID != nil || topology != nil && topology.Clusters <= 1))
				if c.SocketID != nil {
					cpu.SocketId = *c.SocketID
				}
				if c.CoreID != nil {
					cpu.CoreId = *c.CoreID
				}
				if c.DieID != nil {
					cpu.DieId = *c.DieID
				}
				if c.ClusterID != nil {
					cpu.ClusterId = *c.ClusterID
				}
				seen[c.ID] = true
			}
		}
	}
	return result
}

// Darwin's libvirt driver has no nodeGetCPUMap. Native hw.*cpu values describe
// currently available CPUs. Accept only a complete one-thread-per-core topology
// whose physical and logical counts both match; otherwise retain the diagnostic.
func localDarwinOnlineMap(caps *x.Caps, physical, logical int) (map[int]bool, bool) {
	if physical < 1 || logical != physical || caps.Host.NUMA == nil || caps.Host.NUMA.Cells == nil {
		return nil, false
	}
	online := map[int]bool{}
	for _, cell := range caps.Host.NUMA.Cells.Cells {
		if cell.CPUS == nil {
			return nil, false
		}
		for _, cpu := range cell.CPUS.CPUs {
			if _, exists := online[cpu.ID]; exists {
				return nil, false
			}
			online[cpu.ID] = true
		}
	}
	if len(online) != logical {
		return nil, false
	}
	n := compute.Node{Cpus: hostCPUTopology(caps, online, true)}
	cores, err := n.PhysicalCoreCount()
	return online, err == nil && cores == physical
}
