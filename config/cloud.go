package config

import (
	"fmt"
	"math"
	"subuk/vmango/compute"
)

type CatalogEntryConfig struct {
	ID   string `hcl:",key"`
	Name string `hcl:"name"`
}
type InstanceTypeConfig struct {
	ID               string `hcl:",key"`
	Name             string `hcl:"name"`
	CPU              int    `hcl:"cpu"`
	CPURequest       *int   `hcl:"cpu_request"`
	MemoryMiB        int64  `hcl:"memory_mib"`
	MemoryRequestMiB *int64 `hcl:"memory_request_mib"`
	BootDiskGiB      int64  `hcl:"boot_disk_gib"`
	Autostart        bool   `hcl:"autostart"`
	Hugepages        bool   `hcl:"hugepages"`
}
type CloudConfig struct {
	ID             string               `hcl:",key"`
	Name           string               `hcl:"name"`
	Images         []string             `hcl:"images"`
	Zones          []CatalogEntryConfig `hcl:"zone"`
	InstanceTypes  []InstanceTypeConfig `hcl:"instance_type"`
	Networks       []CatalogEntryConfig `hcl:"network"`
	StorageClasses []CatalogEntryConfig `hcl:"storage_class"`
	Hosts          []CloudHostConfig    `hcl:"host"`
}
type CloudHostConfig struct {
	Node                   string                 `hcl:",key"`
	Uri                    string                 `hcl:"uri"`
	ImagePool              string                 `hcl:"image_pool"`
	ConfigDriveSuffix      string                 `hcl:"config_drive_suffix"`
	ConfigDrivePool        string                 `hcl:"config_drive_pool"`
	ConfigDriveWriteFormat string                 `hcl:"config_drive_write_format"`
	Emulator               string                 `hcl:"emulator"`
	GraphicsListen         string                 `hcl:"graphics_listen"`
	QcowPreallocMetadata   bool                   `hcl:"qcow_prealloc_metadata"`
	Cache                  bool                   `hcl:"cache"`
	Zone                   string                 `hcl:"zone"`
	Labels                 map[string]string      `hcl:"labels"`
	CPUReserve             int                    `hcl:"cpu_reserve"`
	MemoryReserveMiB       int64                  `hcl:"memory_reserve_mib"`
	Networks               []NetworkBindingConfig `hcl:"network"`
	Storage                []StorageBindingConfig `hcl:"storage_class"`
}
type NetworkBindingConfig struct {
	Bridge  string `hcl:"bridge"`
	ID      string `hcl:",key"`
	Network string `hcl:"network"`
	Model   string `hcl:"model"`
	VLAN    uint   `hcl:"vlan"`
}
type StorageBindingConfig struct {
	ID     string `hcl:",key"`
	Pool   string `hcl:"pool"`
	Format string `hcl:"format"`
}

func displayName(id, name string) string {
	if name == "" {
		return id
	}
	return name
}
func byteSize(value int64, shift uint) (uint64, error) {
	if value < 0 || uint64(value) > math.MaxUint64>>shift {
		return 0, fmt.Errorf("invalid or overflowing size")
	}
	return uint64(value) << shift, nil
}
func (cfg *Config) CloudDefinitions() ([]compute.Cloud, error) {
	result := []compute.Cloud{}
	seen := map[string]bool{}
	nodes := map[string]string{}
	uris := map[string]string{}
	images, _, err := cfg.ImageDefinitions()
	if err != nil {
		return nil, err
	}
	knownImages := map[string]bool{}
	for _, i := range images {
		knownImages[i.ID] = true
	}
	for _, entry := range cfg.Clouds {
		if seen[entry.ID] {
			return nil, fmt.Errorf("duplicate cloud %s", entry.ID)
		}
		seen[entry.ID] = true
		c := compute.Cloud{ID: entry.ID, Name: displayName(entry.ID, entry.Name), Images: entry.Images}
		for _, pair := range []struct {
			from []CatalogEntryConfig
			to   *[]compute.CatalogEntry
		}{{entry.Zones, &c.Zones}, {entry.Networks, &c.Networks}, {entry.StorageClasses, &c.StorageClasses}} {
			for _, e := range pair.from {
				*pair.to = append(*pair.to, compute.CatalogEntry{ID: e.ID, Name: displayName(e.ID, e.Name)})
			}
		}
		for _, id := range c.Images {
			if !knownImages[id] {
				return nil, fmt.Errorf("cloud %s: unknown image %s", c.ID, id)
			}
		}
		for _, t := range entry.InstanceTypes {
			memory, err := byteSize(t.MemoryMiB, 20)
			if err != nil {
				return nil, err
			}
			request := memory
			if t.MemoryRequestMiB != nil {
				request, err = byteSize(*t.MemoryRequestMiB, 20)
				if err != nil {
					return nil, err
				}
			}
			disk, err := byteSize(t.BootDiskGiB, 30)
			if err != nil {
				return nil, err
			}
			cpuRequest := t.CPU
			if t.CPURequest != nil {
				cpuRequest = *t.CPURequest
			}
			c.InstanceTypes = append(c.InstanceTypes, compute.InstanceType{ID: t.ID, Name: displayName(t.ID, t.Name), VCPUs: t.CPU, CPURequest: cpuRequest, MemoryBytes: memory, MemoryRequestBytes: request, BootDiskBytes: disk, Hugepages: t.Hugepages, Autostart: t.Autostart})
		}
		for _, h := range entry.Hosts {
			if h.Uri == "" {
				return nil, fmt.Errorf("cloud %s host %s: uri is required", c.ID, h.Node)
			}
			if nodes[h.Node] != "" || uris[h.Uri] != "" {
				return nil, fmt.Errorf("host/connection %s must belong to exactly one cloud and zone", h.Node)
			}
			nodes[h.Node] = c.ID
			uris[h.Uri] = c.ID
			reserve, err := byteSize(h.MemoryReserveMiB, 20)
			if err != nil {
				return nil, err
			}
			host := compute.CloudHost{Node: h.Node, Zone: h.Zone, Labels: h.Labels, CPUReserve: h.CPUReserve, MemoryReserveBytes: reserve, Networks: map[string]compute.NetworkBinding{}, Storage: map[string]compute.StorageBinding{}}
			for _, b := range h.Networks {
				if _, ok := host.Networks[b.ID]; ok {
					return nil, fmt.Errorf("duplicate network binding %s", b.ID)
				}
				model := b.Model
				network := b.Network
				if (network == "") == (b.Bridge == "") {
					return nil, fmt.Errorf("network binding %s must select exactly one network or bridge", b.ID)
				}
				if b.Bridge != "" {
					network = b.Bridge
				}
				host.Networks[b.ID] = compute.NetworkBinding{Network: network, Bridge: b.Bridge != "", Model: model, VLAN: b.VLAN}
			}
			for _, b := range h.Storage {
				if _, ok := host.Storage[b.ID]; ok {
					return nil, fmt.Errorf("duplicate storage binding %s", b.ID)
				}
				format := b.Format
				if format == "" {
					format = "qcow2"
				}
				host.Storage[b.ID] = compute.StorageBinding{Pool: b.Pool, Format: compute.NewVolumeFormat(format)}
			}
			c.Hosts = append(c.Hosts, host)
		}
		if err := c.Validate(); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, nil
}

// Hosts returns backend configuration in cloud and host declaration order.
// Backend settings stay in config; CloudDefinitions exposes only domain policy.
func (cfg *Config) Hosts() []CloudHostConfig {
	var hosts []CloudHostConfig
	for _, cloud := range cfg.Clouds {
		hosts = append(hosts, cloud.Hosts...)
	}
	return hosts
}
