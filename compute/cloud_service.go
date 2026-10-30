package compute

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type CloudService struct {
	Definitions []Cloud
	Nodes       *NodeService
	VMs         *VirtualMachineService
	Pools       *VolumePoolService
	Volumes     *VolumeService
	Manager     *VirtualMachineManager
}

type CloudOffer struct {
	Zone           string   `json:"zone"`
	Image          string   `json:"image"`
	InstanceType   string   `json:"instance_type"`
	Networks       []string `json:"networks" nullable:"false"`
	StorageClasses []string `json:"storage_classes" nullable:"false"`
}

type StorageAvailability struct {
	Zone           string   `json:"zone"`
	StorageClasses []string `json:"storage_classes" nullable:"false"`
}

// Storage availability is independent of boot image compatibility: standalone
// volumes remain usable even when no image can boot in a particular zone.
func (s *CloudService) StorageAvailability(c Cloud) ([]StorageAvailability, error) {
	byZone := map[string]map[string]bool{}
	for _, h := range c.Hosts {
		pools, err := s.Pools.List(VolumePoolListOptions{NodeIds: []string{h.Node}})
		if err != nil {
			return nil, err
		}
		if byZone[h.Zone] == nil {
			byZone[h.Zone] = map[string]bool{}
		}
		for _, pool := range pools {
			for id, b := range h.Storage {
				if b.Pool == pool.Name {
					byZone[h.Zone][id] = true
				}
			}
		}
	}
	result := []StorageAvailability{}
	for _, zone := range c.Zones {
		a := StorageAvailability{Zone: zone.ID, StorageClasses: []string{}}
		for _, class := range c.StorageClasses {
			if byZone[zone.ID][class.ID] {
				a.StorageClasses = append(a.StorageClasses, class.ID)
			}
		}
		result = append(result, a)
	}
	return result, nil
}

// Offers filters catalog choices using observed architecture and launch
// capabilities. It does not download images or reserve transient free capacity.
func (s *CloudService) Offers(c Cloud) ([]CloudOffer, error) {
	result := []CloudOffer{}
	for _, h := range c.Hosts {
		node, err := s.Nodes.Get(h.Node, NodeGetOptions{NoPins: true})
		if err != nil {
			return nil, err
		}
		caps, err := s.VMs.Capabilities(h.Node, node.CpuArch)
		if err != nil {
			return nil, err
		}
		pools, err := s.Pools.List(VolumePoolListOptions{NodeIds: []string{h.Node}})
		if err != nil {
			return nil, err
		}
		availablePools := map[string]bool{}
		for _, pool := range pools {
			availablePools[pool.Name] = true
		}
		for _, id := range c.Images {
			image, err := s.Manager.images.Get(id)
			if err != nil {
				return nil, err
			}
			if !image.Compatible(caps) {
				continue
			}
			minimumDisk := image.MinimumDiskBytes
			if !image.Managed() {
				source, err := s.Volumes.Get(image.SourcePath, h.Node)
				if err != nil || source.Metadata.Hidden || source.AttachedTo != "" {
					continue
				}
				minimumDisk = max(minimumDisk, source.Size.Bytes())
			} else {
				if s.Manager.images.ManagedSources == nil {
					continue
				}
				binding, err := s.Manager.images.ManagedSources.Bind(h.Node, image)
				if err != nil {
					continue
				}
				state, _ := s.Manager.images.ManagedSources.Status(binding, image)
				if state == "failed" {
					continue
				}
				if state == "ready" {
					source, err := s.Manager.images.ManagedSources.ready(binding, image)
					if err != nil {
						continue
					}
					minimumDisk = max(minimumDisk, source.Size.Bytes())
				}
			}
			for _, t := range c.InstanceTypes {
				if t.MemoryBytes < image.MinimumMemoryBytes || t.BootDiskBytes < minimumDisk || (caps.MaxVCPUs > 0 && uint(t.VCPUs) > caps.MaxVCPUs) {
					continue
				}
				offer := CloudOffer{Zone: h.Zone, Image: id, InstanceType: t.ID, Networks: []string{}, StorageClasses: []string{}}
				for _, n := range c.Networks {
					if b, ok := h.Networks[n.ID]; ok {
						if b.Model != "" && image.Defaults.NICModel != "" && b.Model != image.Defaults.NICModel {
							continue
						}
						if err := s.VMs.ValidateNetwork(h.Node, &VirtualMachineAttachedInterface{NetworkName: b.Network, Bridge: b.Bridge, Model: b.Model, AccessVlan: b.VLAN}); err != nil {
							continue
						}
						offer.Networks = append(offer.Networks, n.ID)
					}
				}
				for _, sc := range c.StorageClasses {
					if b, ok := h.Storage[sc.ID]; ok && availablePools[b.Pool] {
						offer.StorageClasses = append(offer.StorageClasses, sc.ID)
					}
				}
				if len(offer.Networks) > 0 && len(offer.StorageClasses) > 0 {
					result = append(result, offer)
				}
			}
		}
	}
	return result, nil
}

func (s *CloudService) Get(id string) (Cloud, error) {
	for _, c := range s.Definitions {
		if c.ID == id {
			return c, nil
		}
	}
	return Cloud{}, ErrNotFound
}

func (s *CloudService) ListVolumes(cloudID string, attachable bool) ([]*Volume, error) {
	c, err := s.Get(cloudID)
	if err != nil {
		return nil, err
	}
	nodes := []string{}
	for _, h := range c.Hosts {
		nodes = append(nodes, h.Node)
	}
	options := VolumeListOptions{NodeIds: nodes}
	if attachable {
		return s.Volumes.Attachable(options, s.Manager.images)
	}
	return s.Volumes.List(options)
}
func (s *CloudService) ForNode(node string) (Cloud, CloudHost, error) {
	for _, c := range s.Definitions {
		if h, ok := c.Host(node); ok {
			return c, h, nil
		}
	}
	return Cloud{}, CloudHost{}, fmt.Errorf("%w: host is not assigned to a cloud", ErrInvalid)
}

// ForHost resolves an observed host only within the requested cloud.
func (s *CloudService) ForHost(cloudID, node string) (Cloud, CloudHost, error) {
	c, err := s.Get(cloudID)
	if err != nil {
		return Cloud{}, CloudHost{}, err
	}
	h, ok := c.Host(node)
	if !ok {
		return Cloud{}, CloudHost{}, ErrNotFound
	}
	return c, h, nil
}
func (s *CloudService) Inventory(c Cloud) ([]HostInventory, error) {
	result := []HostInventory{}
	for _, host := range c.Hosts {
		node, err := s.Nodes.Get(host.Node, NodeGetOptions{NoPins: true})
		if err != nil {
			return nil, fmt.Errorf("inventory host %s: %w", host.Node, err)
		}
		machines, err := s.VMs.List(VirtualMachineListOptions{NodeIds: []string{host.Node}, Strict: true})
		if err != nil {
			return nil, fmt.Errorf("inventory host %s: %w", host.Node, err)
		}
		pools, err := s.Pools.List(VolumePoolListOptions{NodeIds: []string{host.Node}})
		if err != nil {
			return nil, fmt.Errorf("storage inventory host %s: %w", host.Node, err)
		}
		result = append(result, HostInventory{Host: host, Node: node, Machines: machines, Pools: pools})
	}
	return result, nil
}
func (s *CloudService) ValidateCreate(c Cloud, spec MachineCreateSpec) error {
	if !resourceName.MatchString(spec.ID) {
		return invalid("id", "Invalid machine name.")
	}
	if !hasEntry(c.Zones, spec.Zone) || !hasEntry(c.Networks, spec.Network) || !hasEntry(c.StorageClasses, spec.StorageClass) {
		return invalid("zone", "Select a configured zone, network and storage class.")
	}
	if !slices.Contains(c.Images, spec.Image) {
		return invalid("image", "Image is not in this cloud catalog.")
	}
	t, err := c.InstanceType(spec.InstanceType)
	if err != nil {
		return err
	}
	image, err := s.Manager.images.Get(spec.Image)
	if err != nil {
		return err
	}
	if t.MemoryBytes < image.MinimumMemoryBytes || t.BootDiskBytes < image.MinimumDiskBytes {
		return invalid("instance_type", "Instance resources do not meet the image minimum.")
	}
	if err = c.ValidatePlacement(spec.Placement); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, d := range spec.Disks {
		if !resourceName.MatchString(d.Name) || seen[d.Name] || !hasEntry(c.StorageClasses, d.StorageClass) || d.SizeBytes == 0 {
			return invalid("new_disks", "Invalid or duplicate additional disk.")
		}
		seen[d.Name] = true
	}
	owner := ""
	paths := map[string]bool{}
	for _, d := range spec.ExistingDisks {
		h, ok := c.Host(d.Host)
		if !ok || h.Zone != spec.Zone || d.Path == "" || paths[d.Path] || (owner != "" && owner != d.Host) {
			return invalid("existing_disks", "Local disks must belong to one host in the selected zone.")
		}
		owner = d.Host
		paths[d.Path] = true
	}
	return nil
}
func storageFits(i HostInventory, required map[string]uint64) error {
	for pool, bytes := range required {
		found := false
		for _, p := range i.Pools {
			if p.Name == pool {
				found = true
				if bytes > p.Free.Bytes() {
					return fmt.Errorf("pool %s needs %d bytes; available %d", pool, bytes, p.Free.Bytes())
				}
			}
		}
		if !found {
			return fmt.Errorf("pool %s is unavailable", pool)
		}
	}
	return nil
}
func addStorage(required map[string]uint64, pool string, bytes uint64) error {
	if ^uint64(0)-required[pool] < bytes {
		return fmt.Errorf("storage size overflow")
	}
	required[pool] += bytes
	return nil
}

func (s *CloudService) ResolveCreate(c Cloud, spec MachineCreateSpec, images *ImageService, inventory []HostInventory, bindings ...map[string]json.RawMessage) (InfrastructureCommand, error) {
	t, err := c.InstanceType(spec.InstanceType)
	if err != nil {
		return InfrastructureCommand{}, err
	}
	image, err := images.Get(spec.Image)
	if err != nil {
		return InfrastructureCommand{}, err
	}
	candidates := []PlacementCandidate{}
	commands := map[string]InfrastructureCommand{}
	reasons := []string{}
	for _, i := range inventory {
		h := i.Host
		if h.Zone != spec.Zone {
			continue
		}
		command, err := func() (InfrastructureCommand, error) {
			for key, value := range spec.Placement.Required {
				if h.Labels[key] != value {
					return InfrastructureCommand{}, fmt.Errorf("required placement label %s does not match", key)
				}
			}
			for _, d := range spec.ExistingDisks {
				if d.Host != h.Node {
					return InfrastructureCommand{}, fmt.Errorf("local disks belong to another host")
				}
			}
			fit, err := Fit(i, t, "")
			if err != nil {
				return InfrastructureCommand{}, err
			}
			if i.Node.CpuArch != image.Arch {
				return InfrastructureCommand{}, fmt.Errorf("image architecture is incompatible")
			}
			if t.Hugepages && !i.Node.Has2mPages() && !i.Node.Has1gPages() {
				return InfrastructureCommand{}, fmt.Errorf("hugepages unavailable")
			}
			storage, ok := h.Storage[spec.StorageClass]
			if !ok {
				return InfrastructureCommand{}, fmt.Errorf("storage class is not bound on host")
			}
			network, ok := h.Networks[spec.Network]
			if !ok {
				return InfrastructureCommand{}, fmt.Errorf("logical network is not bound on host")
			}
			if network.Model != "" && image.Defaults.NICModel != "" && network.Model != image.Defaults.NICModel {
				return InfrastructureCommand{}, fmt.Errorf("network model conflicts with image requirements")
			}
			vm := &VirtualMachine{Id: spec.ID, NodeId: h.Node, Arch: image.Arch, VCpus: t.VCPUs, Memory: NewSize(t.MemoryBytes, SizeUnitB), Hugepages: t.Hugepages, Autostart: t.Autostart, Config: &VirtualMachineConfig{Hostname: spec.ID, Userdata: []byte(spec.UserData)}, Interfaces: []*VirtualMachineAttachedInterface{{NetworkName: network.Network, LogicalNetwork: spec.Network, Bridge: network.Bridge, Model: network.Model, AccessVlan: network.VLAN}}, Metadata: &MachineMetadata{Cloud: c.ID, Zone: h.Zone, InstanceType: t.ID, Image: image.ID, CPURequest: t.CPURequest, MemoryRequestBytes: t.MemoryBytes, MinimumMemoryBytes: image.MinimumMemoryBytes}}
			clones := []VirtualMachineManagerClonedVolumeParams{{Cloud: c.ID, StorageClass: spec.StorageClass, ImageID: image.ID, NewName: spec.ID + "_root", NewPool: storage.Pool, NewSize: NewSize(t.BootDiskBytes, SizeUnitB), NewFormat: storage.Format, Alias: "root", DeviceType: DeviceTypeDisk}}
			newDisks := []VirtualMachineManagerCreatedVolumeParams{}
			required := map[string]uint64{storage.Pool: t.BootDiskBytes}
			for _, d := range spec.Disks {
				b, ok := h.Storage[d.StorageClass]
				if !ok {
					return InfrastructureCommand{}, fmt.Errorf("additional disk storage class %s is unavailable", d.StorageClass)
				}
				if err = addStorage(required, b.Pool, d.SizeBytes); err != nil {
					return InfrastructureCommand{}, err
				}
				newDisks = append(newDisks, VirtualMachineManagerCreatedVolumeParams{Cloud: c.ID, StorageClass: d.StorageClass, Name: spec.ID + "_" + d.Name, Pool: b.Pool, Format: b.Format, Size: NewSize(d.SizeBytes, SizeUnitB), DeviceType: DeviceTypeDisk, DeviceBus: DeviceBusVirtio, Alias: d.Name})
			}
			for _, d := range spec.ExistingDisks {
				if err = s.Volumes.ValidateAttachable(d.Path, h.Node, images); err != nil {
					return InfrastructureCommand{}, err
				}
				vm.Volumes = append(vm.Volumes, &VirtualMachineAttachedVolume{Path: d.Path, DeviceType: DeviceTypeDisk, DeviceBus: DeviceBusVirtio})
			}
			if image.Managed() {
				b, err := images.ManagedSources.Bind(h.Node, image)
				if err != nil {
					return InfrastructureCommand{}, err
				}
				state, cause := images.ManagedSources.Status(b, image)
				if state == "failed" {
					return InfrastructureCommand{}, fmt.Errorf("image preparation: %s", cause)
				}
				if state != "ready" {
					needed := image.Download.SizeBytes
					if needed == 0 {
						needed = images.ManagedSources.limits.MaxBytes
					}
					if err = addStorage(required, b.Pool, needed); err != nil {
						return InfrastructureCommand{}, err
					}
				}
			}
			if image.Defaults.Initialization != "none" {
				if err = addStorage(required, s.Manager.settings[h.Node].CdPool, 8<<20); err != nil {
					return InfrastructureCommand{}, err
				}
			}
			if err = storageFits(i, required); err != nil {
				return InfrastructureCommand{}, err
			}
			manager := s.Manager.withImages(images)
			if len(bindings) != 0 {
				manager.creationBindings = bindings[0]
			}
			prepared, clones, err := manager.PrepareCreation(vm, clones, newDisks, ImageDefaults{})
			if err != nil {
				return InfrastructureCommand{}, err
			}
			for key, value := range spec.Placement.Preferred {
				if h.Labels[key] == value {
					fit.Preferred++
				}
			}
			candidates = append(candidates, fit)
			return InfrastructureCommand{Node: h.Node, VM: vm.Id, Machine: prepared, Clones: clones, NewVolumes: newDisks, Start: spec.Start, KeyIDs: spec.KeyIDs}, nil
		}()
		if err != nil {
			reasons = append(reasons, h.Node+": "+err.Error())
			continue
		}
		commands[h.Node] = command
	}
	host, err := choose(candidates)
	if err != nil {
		return InfrastructureCommand{}, invalid("placement", "No suitable host: "+strings.Join(reasons, "; "))
	}
	return commands[host.Node], nil
}
