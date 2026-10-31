package api

import (
	"subuk/vmango/compute"
)

type apiCPU struct {
	ID     int         `json:"id" doc:"Stable host CPU ID, independent of frontend sorting."`
	Socket int         `json:"socket"`
	Core   int         `json:"core"`
	NUMA   int         `json:"numa"`
	Pins   []apiCPUPin `json:"pins" nullable:"false"`
}
type apiCPUPin struct {
	VM          string `json:"vm"`
	Description string `json:"description"`
}
type apiNUMA struct {
	ID          int    `json:"id" doc:"Stable NUMA cell ID, independent of frontend sorting."`
	MemoryBytes uint64 `json:"memory_bytes"`
	Pages4K     uint64 `json:"pages_4k"`
	FreePages4K uint64 `json:"free_pages_4k"`
	Pages2M     uint64 `json:"pages_2m"`
	FreePages2M uint64 `json:"free_pages_2m"`
	Pages1G     uint64 `json:"pages_1g"`
	FreePages1G uint64 `json:"free_pages_1g"`
}
type apiNode struct {
	ID             string    `json:"id"`
	Hostname       string    `json:"hostname"`
	Arch           string    `json:"arch"`
	CPUModel       string    `json:"cpu_model"`
	CPUVendor      string    `json:"cpu_vendor"`
	CPUInfo        string    `json:"cpu_info"`
	ThreadsPerCore int       `json:"threads_per_core"`
	IOMMU          bool      `json:"iommu"`
	MemoryBytes    uint64    `json:"memory_bytes"`
	CPUs           []apiCPU  `json:"cpus" nullable:"false"`
	NUMA           []apiNUMA `json:"numa" nullable:"false"`
}

func nodeDTO(n *compute.Node) apiNode {
	d := apiNode{ID: n.Id, Hostname: n.Hostname, Arch: n.CpuArch.String(), CPUModel: n.CpuModel, CPUVendor: n.CpuVendor, CPUInfo: n.CpuInfo, ThreadsPerCore: n.ThreadsPerCore, IOMMU: n.Iommu, MemoryBytes: n.Memory().Bytes(), CPUs: []apiCPU{}, NUMA: []apiNUMA{}}
	for id, cpu := range n.Cpus {
		c := apiCPU{ID: id, Socket: cpu.SocketId, Core: cpu.CoreId, NUMA: cpu.NumaId, Pins: []apiCPUPin{}}
		for _, pin := range cpu.Pins {
			c.Pins = append(c.Pins, apiCPUPin{pin.VmId, pin.Desc})
		}
		d.CPUs = append(d.CPUs, c)
	}
	for id, n := range n.Numas {
		d.NUMA = append(d.NUMA, apiNUMA{id, n.Memory.Bytes(), n.Pages4k, n.Pages4kFree, n.Pages2m, n.Pages2mFree, n.Pages1g, n.Pages1gFree})
	}
	sortNodeTopology(&d)
	return d
}

type apiNetwork struct {
	Node string `json:"node"`
	Name string `json:"name"`
}
type apiPool struct {
	Node      string `json:"node"`
	Name      string `json:"name"`
	SizeBytes uint64 `json:"size_bytes"`
	UsedBytes uint64 `json:"used_bytes"`
	FreeBytes uint64 `json:"free_bytes"`
}
type apiCapabilities struct {
	Arch            string              `json:"arch"`
	MaxVCPUs        uint                `json:"max_vcpus"`
	Firmwares       []string            `json:"firmwares" nullable:"false"`
	DiskBuses       []string            `json:"disk_buses" nullable:"false"`
	NICModels       []string            `json:"nic_models" nullable:"false"`
	NetworkModels   map[string][]string `json:"network_models"`
	DiskControllers []string            `json:"disk_controllers" nullable:"false"`
	Graphics        []string            `json:"graphics" nullable:"false"`
	VideoModels     []string            `json:"video_models" nullable:"false"`
}
