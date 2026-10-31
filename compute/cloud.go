package compute

import (
	"fmt"
	"sort"
	"strings"
)

// Cloud is the public catalog plus private host bindings. API discovery must
// return a public discovery DTO, not Cloud: backend bindings are private.
type Cloud struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Zones          []CatalogEntry `json:"zones"`
	InstanceTypes  []InstanceType `json:"instance_types"`
	Networks       []CatalogEntry `json:"networks"`
	StorageClasses []CatalogEntry `json:"storage_classes"`
	Images         []string       `json:"images"`
	Hosts          []CloudHost    `json:"hosts"`
}

type CatalogEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type InstanceType struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	VCPUs              int    `json:"vcpus"`
	CPURequest         int    `json:"cpu_request"`
	MemoryBytes        uint64 `json:"memory_bytes"`
	MemoryRequestBytes uint64 `json:"memory_request_bytes"`
	BootDiskBytes      uint64 `json:"boot_disk_bytes"`
	Hugepages          bool   `json:"hugepages"`
	Autostart          bool   `json:"autostart"`
}
type NetworkBinding struct {
	Bridge  bool
	Network string
	Model   string
	VLAN    uint
}
type StorageBinding struct {
	Pool   string
	Format VolumeFormat
}
type CloudHost struct {
	Node               string
	Zone               string
	Labels             map[string]string
	CPUOvercommit      int
	CPUReserve         int
	MemoryReserveBytes uint64
	Networks           map[string]NetworkBinding
	Storage            map[string]StorageBinding
}

// MachineMetadata is an applied snapshot, never reconstructed from today's
// instance type catalog. Limits continue to come from actual VM configuration.
type MachineMetadata struct {
	Cloud              string `json:"cloud"`
	Zone               string `json:"zone"`
	InstanceType       string `json:"instance_type,omitempty"`
	Image              string `json:"image,omitempty"`
	CPURequest         int    `json:"cpu_request"`
	MemoryRequestBytes uint64 `json:"memory_request_bytes"`
	MinimumMemoryBytes uint64 `json:"minimum_memory_bytes,omitempty"`
}

type Placement struct {
	Required  map[string]string `json:"required,omitempty"`
	Preferred map[string]string `json:"preferred,omitempty"`
}
type LocalDiskRef struct {
	Host string `json:"host"`
	Path string `json:"volume_id"`
}
type DataDiskSpec struct {
	Name         string `json:"name"`
	StorageClass string `json:"storage_class"`
	SizeBytes    uint64 `json:"size_bytes"`
}
type MachineCreateSpec struct {
	Name          string         `json:"name"`
	Image         string         `json:"image"`
	InstanceType  string         `json:"instance_type"`
	Zone          string         `json:"zone"`
	Network       string         `json:"network"`
	StorageClass  string         `json:"storage_class"`
	Placement     Placement      `json:"placement,omitempty"`
	Start         bool           `json:"start"`
	KeyIDs        []string       `json:"keys,omitempty"`
	UserData      string         `json:"user_data,omitempty"`
	Disks         []DataDiskSpec `json:"new_disks,omitempty"`
	ExistingDisks []LocalDiskRef `json:"existing_disks,omitempty"`
}

func invalid(field, message string) error { return &ValidationError{Field: field, Message: message} }
func (c Cloud) InstanceType(id string) (InstanceType, error) {
	for _, t := range c.InstanceTypes {
		if t.ID == id {
			return t, nil
		}
	}
	return InstanceType{}, invalid("instance_type", "Unknown instance type.")
}
func hasEntry(entries []CatalogEntry, id string) bool {
	for _, e := range entries {
		if e.ID == id {
			return true
		}
	}
	return false
}
func (c Cloud) Host(node string) (CloudHost, bool) {
	for _, h := range c.Hosts {
		if h.Node == node {
			return h, true
		}
	}
	return CloudHost{}, false
}
func (c Cloud) PlacementAttributes() []string {
	keys := map[string]bool{}
	for _, h := range c.Hosts {
		for k := range h.Labels {
			keys[k] = true
		}
	}
	out := []string{}
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func (c Cloud) ValidatePlacement(p Placement) error {
	known := map[string]bool{}
	for _, k := range c.PlacementAttributes() {
		known[k] = true
	}
	for _, selectors := range []map[string]string{p.Required, p.Preferred} {
		for key, value := range selectors {
			if !known[key] || value == "" {
				return invalid("placement", "Unsupported or empty placement label: "+key)
			}
		}
	}
	return nil
}
func (c Cloud) Validate() error {
	if !resourceName.MatchString(c.ID) || c.Name == "" {
		return fmt.Errorf("invalid cloud identity %q", c.ID)
	}
	for _, entries := range [][]CatalogEntry{c.Zones, c.Networks, c.StorageClasses} {
		seen := map[string]bool{}
		if len(entries) == 0 {
			return fmt.Errorf("cloud %s: catalog must not be empty", c.ID)
		}
		for _, e := range entries {
			if !resourceName.MatchString(e.ID) || seen[e.ID] || e.Name == "" {
				return fmt.Errorf("cloud %s: invalid or duplicate catalog identity %q", c.ID, e.ID)
			}
			seen[e.ID] = true
		}
	}
	seen := map[string]bool{}
	for _, t := range c.InstanceTypes {
		if t.MemoryRequestBytes != t.MemoryBytes {
			return fmt.Errorf("instance type %s: memory overselling is not supported yet; memory requests must equal limits", t.ID)
		}
		if !resourceName.MatchString(t.ID) || seen[t.ID] || t.VCPUs < 1 || t.VCPUs > 65535 || t.CPURequest < 1 || t.CPURequest > t.VCPUs || t.MemoryBytes == 0 || t.BootDiskBytes == 0 {
			return fmt.Errorf("invalid instance type %q", t.ID)
		}
		seen[t.ID] = true
	}
	if len(seen) == 0 || len(c.Hosts) == 0 || len(c.Images) == 0 {
		return fmt.Errorf("cloud %s needs hosts, images and instance types", c.ID)
	}
	images := map[string]bool{}
	for _, id := range c.Images {
		if images[id] || id == "" {
			return fmt.Errorf("duplicate or empty image reference %q", id)
		}
		images[id] = true
	}
	seen = map[string]bool{}
	for _, h := range c.Hosts {
		if seen[h.Node] || h.Node == "" || !hasEntry(c.Zones, h.Zone) || h.CPUReserve < 0 || h.CPUOvercommit < 0 {
			return fmt.Errorf("cloud %s: invalid host %q", c.ID, h.Node)
		}
		seen[h.Node] = true
		for id, b := range h.Storage {
			if !hasEntry(c.StorageClasses, id) || b.Pool == "" || (b.Format != VolumeFormatRaw && b.Format != VolumeFormatQcow2) {
				return fmt.Errorf("host %s: invalid storage binding %s", h.Node, id)
			}
		}
		for id, b := range h.Networks {
			if !hasEntry(c.Networks, id) || b.Network == "" || b.VLAN > 4094 {
				return fmt.Errorf("host %s: invalid network binding %s", h.Node, id)
			}
		}
	}
	return nil
}

// HostInventory contains backend-normalized, fresh configuration including
// inactive and unmanaged domains. A failed read never produces an empty host.
type HostInventory struct {
	Host     CloudHost
	Node     *Node
	Machines []*VirtualMachine
	Pools    []*VolumePool
}
type PlacementCandidate struct {
	Inventory  HostInventory
	Preferred  int
	FreeCPU    int
	FreeMemory uint64
}

func accounted(vm *VirtualMachine) (int, uint64, error) {
	cpu, mem := vm.VCpus, vm.Memory.Bytes()
	if vm.Metadata != nil {
		cpu, mem = vm.Metadata.CPURequest, vm.Metadata.MemoryRequestBytes
	}
	if cpu < 1 || cpu > vm.VCpus || mem == 0 || mem != vm.Memory.Bytes() {
		return 0, 0, fmt.Errorf("machine %s has invalid scheduling metadata", vm.Id)
	}
	return cpu, mem, nil
}

// Fit accounts requests rather than instantaneous runtime usage. Replacing
// excludes precisely the existing machine when admitting a type change.
func Fit(i HostInventory, t InstanceType, replacing string) (PlacementCandidate, error) {
	r := PlacementCandidate{Inventory: i}
	if i.Node == nil {
		return r, fmt.Errorf("host inventory is unavailable")
	}
	overcommit := i.Host.CPUOvercommit
	if overcommit == 0 {
		overcommit = 4
	}
	physicalCPU := len(i.Node.Cpus) - i.Host.CPUReserve
	if physicalCPU < 0 || overcommit < 1 || physicalCPU > int(^uint(0)>>1)/overcommit {
		return r, fmt.Errorf("invalid host CPU capacity")
	}
	cpuBudget := physicalCPU * overcommit
	memory := i.Node.Memory().Bytes()
	if i.Host.MemoryReserveBytes > memory {
		return r, fmt.Errorf("host memory reserve exceeds capacity")
	}
	memory -= i.Host.MemoryReserveBytes
	usedCPU := 0
	usedMemory := uint64(0)
	for _, vm := range i.Machines {
		if vm.Id == replacing {
			continue
		}
		cpu, mem, err := accounted(vm)
		if err != nil {
			return r, err
		}
		if cpu > cpuBudget-usedCPU || mem > memory-usedMemory {
			return r, fmt.Errorf("existing requests exceed host capacity")
		}
		usedCPU += cpu
		usedMemory += mem
	}
	if t.CPURequest > cpuBudget-usedCPU || t.MemoryBytes > memory-usedMemory {
		return r, fmt.Errorf("insufficient resources on host %s", i.Host.Node)
	}
	r.FreeCPU = cpuBudget - usedCPU - t.CPURequest
	r.FreeMemory = memory - usedMemory - t.MemoryBytes
	return r, nil
}

func choose(candidates []PlacementCandidate) (CloudHost, error) {
	if len(candidates) == 0 {
		return CloudHost{}, fmt.Errorf("no compatible placement")
	}
	sort.Slice(candidates, func(a, b int) bool {
		x, y := candidates[a], candidates[b]
		if x.Preferred != y.Preferred {
			return x.Preferred > y.Preferred
		}
		if x.FreeCPU != y.FreeCPU {
			return x.FreeCPU > y.FreeCPU
		}
		if x.FreeMemory != y.FreeMemory {
			return x.FreeMemory > y.FreeMemory
		}
		return strings.Compare(x.Inventory.Host.Node, y.Inventory.Host.Node) < 0
	})
	return candidates[0].Inventory.Host, nil
}

// ValidCatalogID reports whether an identifier can name a configured catalog entry.
func ValidCatalogID(id string) bool { return resourceName.MatchString(id) }
