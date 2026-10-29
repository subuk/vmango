package web

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"subuk/vmango/compute"
)

type apiCPU struct {
	Socket int         `json:"socket"`
	Core   int         `json:"core"`
	NUMA   int         `json:"numa"`
	Pins   []apiCPUPin `json:"pins"`
}
type apiCPUPin struct {
	VM          string `json:"vm"`
	Description string `json:"description"`
}
type apiNUMA struct {
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
	CPUs           []apiCPU  `json:"cpus"`
	NUMA           []apiNUMA `json:"numa"`
}

func nodeDTO(n *compute.Node) apiNode {
	d := apiNode{ID: n.Id, Hostname: n.Hostname, Arch: n.CpuArch.String(), CPUModel: n.CpuModel, CPUVendor: n.CpuVendor, CPUInfo: n.CpuInfo, ThreadsPerCore: n.ThreadsPerCore, IOMMU: n.Iommu, MemoryBytes: n.Memory().Bytes(), CPUs: []apiCPU{}, NUMA: []apiNUMA{}}
	for _, cpu := range n.Cpus {
		c := apiCPU{Socket: cpu.SocketId, Core: cpu.CoreId, NUMA: cpu.NumaId, Pins: []apiCPUPin{}}
		for _, pin := range cpu.Pins {
			c.Pins = append(c.Pins, apiCPUPin{pin.VmId, pin.Desc})
		}
		d.CPUs = append(d.CPUs, c)
	}
	for _, n := range n.Numas {
		d.NUMA = append(d.NUMA, apiNUMA{n.Memory.Bytes(), n.Pages4k, n.Pages4kFree, n.Pages2m, n.Pages2mFree, n.Pages1g, n.Pages1gFree})
	}
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
type apiImage struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	OSName             string           `json:"os_name"`
	OSVersion          string           `json:"os_version"`
	Arch               string           `json:"arch"`
	VolumeID           string           `json:"volume_id"`
	MinimumMemoryBytes uint64           `json:"minimum_memory_bytes"`
	MinimumDiskBytes   uint64           `json:"minimum_disk_bytes"`
	Defaults           apiImageDefaults `json:"defaults"`
}
type apiArchInput struct {
	Node string `path:"node"`
	Arch string `query:"arch" enum:"x86_64,aarch64" doc:"Omit to use the node architecture."`
}
type apiCapabilities struct {
	Arch            string              `json:"arch"`
	MaxVCPUs        uint                `json:"max_vcpus"`
	Firmwares       []string            `json:"firmwares"`
	DiskBuses       []string            `json:"disk_buses"`
	NICModels       []string            `json:"nic_models"`
	NetworkModels   map[string][]string `json:"network_models"`
	DiskControllers []string            `json:"disk_controllers"`
	Graphics        []string            `json:"graphics"`
	VideoModels     []string            `json:"video_models"`
}

func (e *Environ) discoveryArch(in *apiArchInput) (compute.Arch, error) {
	if in.Arch != "" {
		return compute.NewArch(in.Arch), nil
	}
	n, err := e.nodes.Get(in.Node, compute.NodeGetOptions{NoPins: true})
	if err != nil {
		return 0, err
	}
	return n.CpuArch, nil
}
func (e *Environ) registerDiscovery(api huma.API) {
	registerAPI(api, "GET", "/nodes", "list-nodes", "List nodes", func(ctx context.Context, in *apiEmpty) (*apiResult[[]apiNode], error) {
		nodes, err := e.nodes.List(compute.NodeListOptions{})
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiNode{}
		for _, n := range nodes {
			out = append(out, nodeDTO(n))
		}
		return &apiResult[[]apiNode]{Body: out}, nil
	})
	registerAPI(api, "GET", "/nodes/{node}", "get-node", "Get node and CPU topology", func(ctx context.Context, in *apiNodeInput) (*apiResult[apiNode], error) {
		n, err := e.nodes.Get(in.Node, compute.NodeGetOptions{})
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[apiNode]{Body: nodeDTO(n)}, nil
	})
	registerAPI(api, "GET", "/nodes/{node}/networks", "list-networks", "List networks", func(ctx context.Context, in *apiNodeInput) (*apiResult[[]apiNetwork], error) {
		if _, err := e.nodes.Get(in.Node, compute.NodeGetOptions{NoPins: true}); err != nil {
			return nil, apiError(err)
		}
		items, err := e.networks.List(compute.NetworkListOptions{NodeIds: []string{in.Node}})
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiNetwork{}
		for _, n := range items {
			out = append(out, apiNetwork{n.NodeId, n.Name})
		}
		return &apiResult[[]apiNetwork]{Body: out}, nil
	})
	registerAPI(api, "GET", "/nodes/{node}/pools", "list-pools", "List storage pools", func(ctx context.Context, in *apiNodeInput) (*apiResult[[]apiPool], error) {
		if _, err := e.nodes.Get(in.Node, compute.NodeGetOptions{NoPins: true}); err != nil {
			return nil, apiError(err)
		}
		items, err := e.volpools.List(compute.VolumePoolListOptions{NodeIds: []string{in.Node}})
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiPool{}
		for _, p := range items {
			out = append(out, apiPool{p.NodeId, p.Name, p.Size.Bytes(), p.Used.Bytes(), p.Free.Bytes()})
		}
		return &apiResult[[]apiPool]{Body: out}, nil
	})
	registerAPI(api, "GET", "/nodes/{node}/capabilities", "get-capabilities", "Discover supported launch options", func(ctx context.Context, in *apiArchInput) (*apiResult[apiCapabilities], error) {
		arch, err := e.discoveryArch(in)
		if err != nil {
			return nil, apiError(err)
		}
		c, err := e.vms.Capabilities(in.Node, arch)
		if err != nil {
			return nil, apiError(err)
		}
		out := apiCapabilities{Arch: c.Arch.String(), MaxVCPUs: c.MaxVCPUs, Firmwares: c.Firmwares, DiskBuses: []string{}, NICModels: c.NICModels, NetworkModels: c.NetworkModels, DiskControllers: c.DiskControllers, Graphics: []string{}, VideoModels: []string{}}
		for _, v := range c.DiskBuses {
			out.DiskBuses = append(out.DiskBuses, v.String())
		}
		for _, v := range c.Graphics {
			out.Graphics = append(out.Graphics, v.String())
		}
		for _, v := range c.VideoModels {
			out.VideoModels = append(out.VideoModels, v.String())
		}
		return &apiResult[apiCapabilities]{Body: out}, nil
	})
	registerAPI(api, "GET", "/nodes/{node}/images", "list-images", "List available images for this node", func(ctx context.Context, in *apiArchInput) (*apiResult[[]apiImage], error) {
		arch, err := e.discoveryArch(in)
		if err != nil {
			return nil, apiError(err)
		}
		vs, err := e.volumes.List(compute.VolumeListOptions{NodeIds: []string{in.Node}})
		if err != nil {
			return nil, apiError(err)
		}
		items, err := e.images.Available(in.Node, arch, vs)
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiImage{}
		for _, i := range items {
			bus := ""
			if i.Defaults.DiskBus != compute.DeviceBusUnknown {
				bus = i.Defaults.DiskBus.String()
			}
			out = append(out, apiImage{i.ID, i.Name, i.OSName, i.OSVersion, i.Arch.String(), volumeID(i.SourcePath), i.MinimumMemoryBytes, i.MinimumDiskBytes, apiImageDefaults{i.Defaults.Firmware, bus, i.Defaults.DiskController, i.Defaults.NICModel, i.Defaults.GuestAgent, i.Defaults.Initialization}})
		}
		return &apiResult[[]apiImage]{Body: out}, nil
	})
}
