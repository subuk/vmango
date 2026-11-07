package libvirt

import (
	"fmt"
	"path"
	"subuk/vmango/compute"

	vir "libvirt.org/go/libvirt"
	xml "libvirt.org/go/libvirtxml"
)

func backingStore(config *xml.StorageVolume) *compute.VolumeBackingStore {
	if config.BackingStore == nil || config.BackingStore.Path == "" {
		return nil
	}
	p := config.BackingStore.Path
	if !path.IsAbs(p) && config.Target != nil {
		p = path.Join(path.Dir(config.Target.Path), p)
	}
	format := compute.VolumeFormatUnknown
	if config.BackingStore.Format != nil {
		format = compute.NewVolumeFormat(config.BackingStore.Format.Type)
	}
	return &compute.VolumeBackingStore{Path: path.Clean(p), Format: format}
}

// libvirtxml allocates an empty Source even for the <backingStore/> terminator.
func emptyBackingStore(backing *xml.DomainDiskBackingStore) bool {
	return backing == nil || (backing.Index == 0 && backing.Format == nil && backing.BackingStore == nil &&
		(backing.Source == nil || *backing.Source == (xml.DomainDiskSource{})))
}

// dependencyScan combines pool metadata with both live and persistent domain
// definitions. Volume metadata is necessary when stopped domain XML omits chains.
type dependencyScan struct {
	state         compute.StorageDependencyState
	volumes       map[string]*xml.StorageVolume
	lookup        func(string) (*xml.StorageVolume, error)
	resolveSource func(*xml.DomainDiskSource) (string, error)
}

func newDependencyScan(lookup func(string) (*xml.StorageVolume, error), source func(*xml.DomainDiskSource) (string, error)) *dependencyScan {
	return &dependencyScan{state: compute.StorageDependencyState{WritableDisks: map[string][]string{}, CanonicalPaths: map[string]string{}}, volumes: map[string]*xml.StorageVolume{}, lookup: lookup, resolveSource: source}
}

func (s *dependencyScan) volume(p string) (*xml.StorageVolume, error) {
	if v := s.volumes[p]; v != nil {
		return v, nil
	}
	v, err := s.lookup(p)
	if err != nil {
		return nil, err
	}
	if v.Target == nil || v.Target.Path == "" {
		return nil, fmt.Errorf("missing volume path")
	}
	s.volumes[p], s.volumes[v.Target.Path] = v, v
	s.state.CanonicalPaths[p] = v.Target.Path
	return v, nil
}

func (s *dependencyScan) addVolume(v *xml.StorageVolume) error {
	if v.Target == nil || v.Target.Path == "" {
		return fmt.Errorf("missing volume path")
	}
	p := v.Target.Path
	s.volumes[p] = v
	s.state.CanonicalPaths[p] = p
	base := backingStore(v)
	if base == nil {
		return nil
	}
	if getVolTargetFormatType(v) != "qcow2" || (base.Format != compute.VolumeFormatRaw && base.Format != compute.VolumeFormatQcow2) {
		return fmt.Errorf("unsupported backing chain format")
	}
	parent, err := s.volume(base.Path)
	if err != nil {
		return err
	}
	if backingStore(parent) != nil || parent.Target.Path == p {
		return fmt.Errorf("unsupported backing chain: more than one level")
	}
	if getVolTargetFormatType(parent) != base.Format.String() {
		return fmt.Errorf("backing format does not match source")
	}
	s.state.Dependencies = append(s.state.Dependencies, compute.VolumeDependency{BasePath: parent.Target.Path, DependentPath: p})
	return nil
}

func (s *dependencyScan) addDomain(d *xml.Domain) error {
	if _, exists := s.state.WritableDisks[d.UUID]; !exists {
		s.state.WritableDisks[d.UUID] = []string{}
	}
	if d.Devices == nil {
		return nil
	}
	for _, disk := range d.Devices.Disks {
		if disk.Source == nil {
			continue
		}
		p, err := s.resolveSource(disk.Source)
		if err != nil {
			return err
		}
		// Empty removable drives have no dependency.
		if p == "" && (disk.Device == "cdrom" || disk.Device == "floppy") {
			continue
		}
		if p == "" {
			return fmt.Errorf("unsupported disk source")
		}
		v, err := s.volume(p)
		if err != nil {
			return err
		}
		if err = s.addVolume(v); err != nil {
			return err
		}
		if disk.ReadOnly == nil && disk.Device != "cdrom" {
			s.state.WritableDisks[d.UUID] = append(s.state.WritableDisks[d.UUID], v.Target.Path)
		}
		// Live XML can reveal a chain that cached volume metadata omitted.
		backing := disk.BackingStore
		if emptyBackingStore(backing) {
			continue
		}
		if !emptyBackingStore(backing.BackingStore) {
			return fmt.Errorf("unsupported backing chain: more than one level")
		}
		if backing.Source == nil {
			return fmt.Errorf("unresolved backing source")
		}
		basePath, err := s.resolveSource(backing.Source)
		if err != nil || basePath == "" {
			return fmt.Errorf("unresolved backing source")
		}
		base, err := s.volume(basePath)
		if err != nil {
			return err
		}
		if backing.Format == nil || backing.Format.Type != getVolTargetFormatType(base) {
			return fmt.Errorf("inconsistent live backing format")
		}
		if backingStore(base) != nil || base.Target.Path == v.Target.Path {
			return fmt.Errorf("unsupported backing chain: more than one level")
		}
		if actual := backingStore(v); actual != nil {
			resolved, err := s.volume(actual.Path)
			if err != nil || resolved.Target.Path != base.Target.Path {
				return fmt.Errorf("inconsistent backing metadata")
			}
		}
		s.state.Dependencies = append(s.state.Dependencies, compute.VolumeDependency{BasePath: base.Target.Path, DependentPath: v.Target.Path})
	}
	return nil
}

func storageDiskSource(conn *vir.Connect, source *xml.DomainDiskSource) (string, error) {
	if source.File != nil {
		return source.File.File, nil
	}
	if source.Block != nil {
		return source.Block.Dev, nil
	}
	if source.Volume != nil {
		pool, err := conn.LookupStoragePoolByName(source.Volume.Pool)
		if err != nil {
			return "", err
		}
		defer pool.Free()
		v, err := pool.LookupStorageVolByName(source.Volume.Volume)
		if err != nil {
			return "", err
		}
		defer v.Free()
		return v.GetPath()
	}
	return "", fmt.Errorf("unsupported disk source")
}

func readStorageVolume(v *vir.StorageVol) (*xml.StorageVolume, error) {
	description, err := v.GetXMLDesc(0)
	if err != nil {
		return nil, err
	}
	var config xml.StorageVolume
	if err = config.Unmarshal(description); err != nil {
		return nil, err
	}
	return &config, nil
}

// InspectDependencies never applies caller visibility filters. Errors deliberately
// omit foreign VM and disk identities; detailed failures remain in server logs.
func (repo *VolumeRepository) InspectDependencies(node string) (result compute.StorageDependencyState, err error) {
	defer func() {
		if err != nil {
			repo.logger.Warn().Err(err).Str("node", node).Msg("storage dependency scan failed")
			result = compute.StorageDependencyState{}
			err = fmt.Errorf("%w: storage dependency scan on host %s is incomplete or contains an unsupported chain", compute.ErrUnavailable, node)
		}
	}()
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return result, err
	}
	defer repo.pool.Release(node)
	scan := newDependencyScan(func(p string) (*xml.StorageVolume, error) {
		v, err := conn.LookupStorageVolByPath(p)
		if err != nil {
			return nil, err
		}
		defer v.Free()
		return readStorageVolume(v)
	}, func(source *xml.DomainDiskSource) (string, error) { return storageDiskSource(conn, source) })
	pools, err := conn.ListAllStoragePools(0)
	if err != nil {
		return result, err
	}
	defer func() {
		for i := range pools {
			pools[i].Free()
		}
	}()
	var configs []*xml.StorageVolume
	for i := range pools {
		pool := &pools[i]
		active, e := pool.IsActive()
		if e != nil {
			return result, e
		}
		if !active {
			return result, fmt.Errorf("inactive pool cannot be inspected")
		}
		if e = pool.Refresh(0); e != nil {
			return result, e
		}
		volumes, e := pool.ListAllStorageVolumes(0)
		if e != nil {
			return result, e
		}
		for j := range volumes {
			config, e := readStorageVolume(&volumes[j])
			volumes[j].Free()
			if e != nil {
				for k := j + 1; k < len(volumes); k++ {
					volumes[k].Free()
				}
				return result, e
			}
			configs = append(configs, config)
			if config.Target != nil {
				scan.volumes[config.Target.Path] = config
			}
		}
	}
	for _, config := range configs {
		if err = scan.addVolume(config); err != nil {
			return result, err
		}
	}
	domains, err := conn.ListAllDomains(0)
	if err != nil {
		return result, err
	}
	defer func() {
		for i := range domains {
			domains[i].Free()
		}
	}()
	for i := range domains {
		domain := &domains[i]
		persistent, e := domain.IsPersistent()
		if e != nil {
			return result, e
		}
		flags := []vir.DomainXMLFlags{0}
		if persistent {
			flags = append(flags, vir.DOMAIN_XML_INACTIVE)
		}
		for _, flag := range flags {
			description, e := domain.GetXMLDesc(flag)
			if e != nil {
				return result, e
			}
			var config xml.Domain
			if e = config.Unmarshal(description); e != nil {
				return result, e
			}
			if e = scan.addDomain(&config); e != nil {
				return result, e
			}
		}
	}
	return scan.state, nil
}
