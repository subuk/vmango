package compute

import (
	"errors"
	"sort"
)

// HostCatalog contains logical choices validated at an existing placement.
// It describes current choices without reserving capacity or accepting commands.
type HostCatalog struct {
	Networks       []CatalogEntry `json:"networks" nullable:"false"`
	StorageClasses []CatalogEntry `json:"storage_classes" nullable:"false"`
}

func (s *CloudService) HostCatalog(cloudID, node string, vm *VirtualMachine) (HostCatalog, error) {
	out := HostCatalog{Networks: []CatalogEntry{}, StorageClasses: []CatalogEntry{}}
	c, h, err := s.ForHost(cloudID, node)
	if err != nil {
		return out, err
	}
	if s.Pools == nil || s.VMs == nil {
		return out, ErrUnavailable
	}
	pools, err := s.Pools.List(VolumePoolListOptions{NodeIds: []string{node}})
	if err != nil {
		return out, err
	}
	available := map[string]bool{}
	for _, pool := range pools {
		available[pool.Name] = true
	}
	for _, class := range c.StorageClasses {
		if b, ok := h.Storage[class.ID]; ok && available[b.Pool] {
			out.StorageClasses = append(out.StorageClasses, class)
		}
	}
	for _, network := range c.Networks {
		if b, ok := h.Networks[network.ID]; ok {
			model := b.Model
			if model == "" && vm != nil && len(vm.Interfaces) > 0 {
				model = vm.Interfaces[0].Model
			}
			nic := &VirtualMachineAttachedInterface{NetworkName: b.Network, Bridge: b.Bridge, Model: model, AccessVlan: b.VLAN}
			if err := s.VMs.ValidateNetwork(node, nic); err == nil {
				out.Networks = append(out.Networks, network)
			} else if !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrConflict) && !errors.Is(err, ErrNotFound) {
				return out, err
			}
		}
	}
	sort.Slice(out.Networks, func(i, j int) bool { return out.Networks[i].ID < out.Networks[j].ID })
	sort.Slice(out.StorageClasses, func(i, j int) bool { return out.StorageClasses[i].ID < out.StorageClasses[j].ID })
	return out, nil
}

func (s *CloudService) MachineCatalog(cloudID string, vm *VirtualMachine) (HostCatalog, error) {
	return s.HostCatalog(cloudID, vm.NodeId, vm)
}

// VolumeCatalog uses the already-read volume and its host. Attached sources
// cannot be cloned; an empty selection records that successfully known fact.
func (s *CloudService) VolumeCatalog(cloudID string, v *Volume) ([]CatalogEntry, error) {
	c, h, err := s.ForHost(cloudID, v.NodeId)
	if err != nil {
		return nil, err
	}
	out := []CatalogEntry{}
	if v.AttachedTo != "" || v.Metadata.Hidden {
		return out, nil
	}
	if s.Pools == nil {
		return nil, ErrUnavailable
	}
	pools, err := s.Pools.List(VolumePoolListOptions{NodeIds: []string{v.NodeId}})
	if err != nil {
		return nil, err
	}
	available := map[string]bool{}
	for _, pool := range pools {
		available[pool.Name] = true
	}
	for _, class := range c.StorageClasses {
		if binding, ok := h.Storage[class.ID]; ok && available[binding.Pool] {
			out = append(out, class)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
