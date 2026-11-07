package libvirt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"
	"subuk/vmango/util"
	"sync"

	"github.com/rs/zerolog"

	libvirtxml "libvirt.org/go/libvirtxml"

	libvirt "libvirt.org/go/libvirt"
)

func (repo *VolumeRepository) ResolvePath(node, pool, name string) (string, error) {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return "", repositoryError(err, "resolve storage destination")
	}
	defer repo.pool.Release(node)
	p, err := conn.LookupStoragePoolByName(pool)
	if err != nil {
		return "", repositoryError(err, "lookup destination pool")
	}
	defer p.Free()
	xml, err := p.GetXMLDesc(0)
	if err != nil {
		return "", repositoryError(err, "read destination pool")
	}
	var config libvirtxml.StoragePool
	if err := config.Unmarshal(xml); err != nil {
		return "", err
	}
	if config.Target == nil || config.Target.Path == "" || path.Base(name) != name {
		return "", fmt.Errorf("%w: storage pool needs a file destination", compute.ErrInvalid)
	}
	return path.Join(config.Target.Path, name), nil
}

type VolumeRepository struct {
	pool     *ConnectionPool
	metadata map[string]compute.VolumeMetadata
	settings map[string]NodeSettings
	logger   zerolog.Logger
}

func NewVolumeRepository(pool *ConnectionPool, settings map[string]NodeSettings, metadata map[string]compute.VolumeMetadata, logger zerolog.Logger) *VolumeRepository {
	return &VolumeRepository{pool: pool, settings: settings, metadata: metadata, logger: logger}
}

func (repo *VolumeRepository) virVolumeToVolume(nodeId string, pool *libvirt.StoragePool, virVolume *libvirt.StorageVol) (*compute.Volume, error) {
	virVolumeXml, err := virVolume.GetXMLDesc(0)
	if err != nil {
		return nil, repositoryError(err, "cannot get volume info")
	}
	virVolumeConfig := &libvirtxml.StorageVolume{}
	if err := virVolumeConfig.Unmarshal(virVolumeXml); err != nil {
		return nil, repositoryError(err, "cannot unmarshal volume xml")
	}
	if virVolumeConfig.Target == nil || virVolumeConfig.Target.Path == "" || virVolumeConfig.Capacity == nil {
		return nil, fmt.Errorf("volume metadata is missing its path or capacity")
	}
	poolXml, err := pool.GetXMLDesc(0)
	if err != nil {
		return nil, repositoryError(err, "cannot get storage pool xml")
	}
	poolConfig := &libvirtxml.StoragePool{}
	if err := poolConfig.Unmarshal(poolXml); err != nil {
		return nil, repositoryError(err, "cannot unmarshal storage pool xml")
	}

	volume := &compute.Volume{}
	key, err := virVolume.GetKey()
	if err != nil {
		return nil, err
	}
	poolID, err := pool.GetUUIDString()
	if err != nil {
		return nil, err
	}
	scope := sha256.Sum256([]byte(repo.pool.nodeUri[nodeId]))
	volume.Identity = hex.EncodeToString(scope[:]) + "\x00" + poolID + "\x00" + key
	volume.PoolIdentity = poolID
	volume.BackendKey = key
	volume.NodeId = nodeId
	volume.Path = virVolumeConfig.Target.Path
	volume.Name = virVolumeConfig.Name
	volume.Label = volume.Name
	volume.Pool = poolConfig.Name
	volume.Metadata = repo.metadata[virVolumeConfig.Target.Path]
	if scoped, ok := repo.metadata[compute.VolumePolicyKey(nodeId, virVolumeConfig.Target.Path)]; ok {
		volume.Metadata.Protected = volume.Metadata.Protected || scoped.Protected
		volume.Metadata.Hidden = volume.Metadata.Hidden || scoped.Hidden
	}
	if strings.HasPrefix(volume.Name, compute.ManagedImagePrefix) {
		volume.Metadata = compute.VolumeMetadata{Protected: true, Hidden: true}
	}
	volume.Size = ComputeSizeFromLibvirtSize(virVolumeConfig.Capacity.Unit, virVolumeConfig.Capacity.Value)

	switch getVolTargetFormatType(virVolumeConfig) {
	case "raw":
		volume.Format = compute.VolumeFormatRaw
	case "iso":
		volume.Format = compute.VolumeFormatIso
	case "qcow2":
		volume.Format = compute.VolumeFormatQcow2
	}
	volume.BackingStore = backingStore(virVolumeConfig)

	return volume, nil
}

func (repo *VolumeRepository) fetchAttachedVm(conn *libvirt.Connect, volumes []*compute.Volume) error {
	domains, err := conn.ListAllDomains(0)
	if err != nil {
		return repositoryError(err, "cannot list domains")
	}
	for _, domain := range domains {
		domainXml, err := domain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
		if err != nil {
			return repositoryError(err, "cannot get domain xml description")
		}
		domainConfig := &libvirtxml.Domain{}
		if err := domainConfig.Unmarshal(domainXml); err != nil {
			return repositoryError(err, "cannot unmarshal domain xml")
		}
		for _, diskConfig := range domainConfig.Devices.Disks {
			attachedVolume := VirtualMachineAttachedVolumeFromDomainDiskConfig(diskConfig)
			if attachedVolume == nil {
				continue
			}
			for _, volume := range volumes {
				if volume.Path == attachedVolume.Path {
					volume.AttachedTo = domainConfig.UUID
					volume.AttachedAs = attachedVolume.DeviceType
				}
			}
		}
	}
	return nil
}

func (repo *VolumeRepository) Get(path, node string) (*compute.Volume, error) {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return nil, repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(node)

	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return nil, repositoryError(err, "cannot lookup volume by path %s", path)
	}
	pool, err := virVolume.LookupPoolByVolume()
	if err != nil {
		return nil, repositoryError(err, "cannot lookup pool for volume")
	}
	volume, err := repo.virVolumeToVolume(node, pool, virVolume)
	if err != nil {
		return nil, repositoryError(err, "cannot parse volume")
	}
	if err := repo.fetchAttachedVm(conn, []*compute.Volume{volume}); err != nil {
		return nil, repositoryError(err, "cannot fetch attached vm")
	}
	return volume, nil
}

func (repo *VolumeRepository) listNode(nodeId string, onlyPools []string) ([]*compute.Volume, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	volumes := []*compute.Volume{}

	pools, err := conn.ListAllStoragePools(0)
	if err != nil {
		return nil, repositoryError(err, "cannot list storage pools")
	}

	for _, pool := range pools {
		poolName, err := pool.GetName()
		if err != nil {
			return nil, repositoryError(err, "cannot get volume pool name")
		}
		if onlyPools != nil && !util.ArrayContainsString(onlyPools, poolName) {
			continue
		}
		active, err := pool.IsActive()
		if err != nil {
			return nil, repositoryError(err, "cannot check if pool is active")
		}
		if !active {
			continue
		}
		virVolumes, err := pool.ListAllStorageVolumes(0)
		if err != nil {
			return nil, repositoryError(err, "cannot list storage volumes")
		}
		for _, virVolume := range virVolumes {
			volume, err := repo.virVolumeToVolume(nodeId, &pool, &virVolume)
			if err != nil {
				return nil, repositoryError(err, "cannot parse libvirt volume")
			}
			if volume.Metadata.Hidden {
				continue
			}
			volumes = append(volumes, volume)
		}
	}
	if err := repo.fetchAttachedVm(conn, volumes); err != nil {
		return nil, repositoryError(err, "cannot fetch attached vm")
	}
	return volumes, nil
}

func (repo *VolumeRepository) List(options compute.VolumeListOptions) ([]*compute.Volume, error) {
	inventory, err := repo.ListInventory(options)
	return inventory.Volumes, err
}

// ListInventory records each failed host instead of treating it as empty.
func (repo *VolumeRepository) ListInventory(options compute.VolumeListOptions) (compute.VolumeInventory, error) {
	result := compute.VolumeInventory{Volumes: []*compute.Volume{}, UnavailableHosts: []string{}}
	nodes := repo.pool.Nodes(options.NodeIds)
	reads := make([][]*compute.Volume, len(nodes))
	failures := make([]error, len(nodes))
	var wg sync.WaitGroup
	workers := make(chan struct{}, 20)
	for n, node := range nodes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workers <- struct{}{}
			defer func() { <-workers }()
			reads[n], failures[n] = repo.listNode(node, options.PoolNames)
		}()
	}
	wg.Wait()
	for n, node := range nodes {
		if failures[n] != nil {
			repo.logger.Warn().Str("node", node).Msg("cannot list volumes")
			result.UnavailableHosts = append(result.UnavailableHosts, node)
			continue
		}
		result.Volumes = append(result.Volumes, reads[n]...)
	}
	return result, nil
}

func (repo *VolumeRepository) Create(params compute.VolumeCreateParams) (result *compute.Volume, err error) {
	var effects []tasks.Effect
	ref := compute.VolumeRef(params.NodeId, params.Name)
	defer func() { err = mutationError(err, "storage-volume.create", ref, effects) }()

	conn, err := repo.pool.Acquire(params.NodeId)
	if err != nil {
		return nil, repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(params.NodeId)

	settings := repo.settings[params.NodeId]

	virPool, err := conn.LookupStoragePoolByName(params.Pool)
	if err != nil {
		return nil, repositoryError(err, "cannot lookup libvirt pool")
	}

	virVolumeConfig := &libvirtxml.StorageVolume{}
	virVolumeConfig.Name = params.Name
	virVolumeConfig.Capacity = &libvirtxml.StorageVolumeSize{
		Unit:  ComputeSizeUnitToLibvirtUnit(params.Size.Unit),
		Value: params.Size.Value,
	}
	if params.Format == compute.VolumeFormatQcow2 {
		virVolumeConfig.Target = &libvirtxml.StorageVolumeTarget{
			Format: &libvirtxml.StorageVolumeTargetFormat{
				Type: "qcow2",
			},
		}
	}

	virVolumeXml, err := virVolumeConfig.Marshal()
	if err != nil {
		return nil, repositoryError(err, "cannot marshal libvirt volume config")
	}
	virVolCreateFlags := libvirt.StorageVolCreateFlags(0)
	if params.Format == compute.VolumeFormatQcow2 && settings.QcowPreallocMetadata {
		virVolCreateFlags |= libvirt.STORAGE_VOL_CREATE_PREALLOC_METADATA
	}

	virVolume, err := virPool.StorageVolCreateXML(virVolumeXml, virVolCreateFlags)
	if err != nil {
		return nil, repositoryError(err, "cannot create volume")
	}
	if path, e := virVolume.GetPath(); e == nil {
		ref = compute.VolumeRef(params.NodeId, path)
		effects = append(effects, confirmed(ref, "created"))
	}
	return repo.virVolumeToVolume(params.NodeId, virPool, virVolume)
}

func (repo *VolumeRepository) Clone(params compute.VolumeCloneParams) (result *compute.Volume, err error) {
	var effects []tasks.Effect
	ref := compute.VolumeRef(params.NodeId, params.NewName)
	defer func() {
		err = cloneMutationError(err, params, ref, effects)
	}()
	conn, err := repo.pool.Acquire(params.NodeId)
	if err != nil {
		return nil, repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(params.NodeId)
	original, err := conn.LookupStorageVolByPath(params.OriginalPath)
	if err != nil {
		return nil, repositoryError(err, "cannot lookup original volume")
	}
	defer original.Free()
	sourcePool, err := original.LookupPoolByVolume()
	if err != nil {
		return nil, repositoryError(err, "cannot lookup source pool")
	}
	defer sourcePool.Free()
	if err = sourcePool.Refresh(0); err != nil {
		return nil, repositoryError(err, "cannot refresh clone source metadata")
	}
	source, err := repo.virVolumeToVolume(params.NodeId, sourcePool, original)
	if err != nil {
		return nil, err
	}
	if params.SourceIdentity != "" && params.SourceIdentity != source.Identity {
		return nil, fmt.Errorf("%w: clone source identity changed", compute.ErrConflict)
	}
	if source.BackingStore != nil {
		base, e := conn.LookupStorageVolByPath(source.BackingStore.Path)
		if e != nil {
			return nil, repositoryError(e, "cannot inspect clone source backing chain")
		}
		basePool, e := base.LookupPoolByVolume()
		if e != nil {
			base.Free()
			return nil, repositoryError(e, "cannot locate backing source pool")
		}
		e = basePool.Refresh(0)
		basePool.Free()
		if e != nil {
			base.Free()
			return nil, repositoryError(e, "cannot refresh backing source metadata")
		}
		config, e := readStorageVolume(base)
		base.Free()
		if e != nil {
			return nil, e
		}
		if backingStore(config) != nil {
			return nil, fmt.Errorf("%w: unsupported backing chain: more than one level", compute.ErrInvalid)
		}
		if getVolTargetFormatType(config) != source.BackingStore.Format.String() {
			return nil, fmt.Errorf("%w: backing format does not match source", compute.ErrInvalid)
		}
	}
	config, err := cloneVolumeConfig(params, source)
	if err != nil {
		return nil, err
	}
	pool, err := conn.LookupStoragePoolByName(params.NewPool)
	if err != nil {
		return nil, repositoryError(err, "cannot lookup destination pool")
	}
	defer pool.Free()
	poolDescription, err := pool.GetXMLDesc(0)
	if err != nil {
		return nil, repositoryError(err, "cannot inspect destination pool")
	}
	var poolConfig libvirtxml.StoragePool
	if err = poolConfig.Unmarshal(poolDescription); err != nil {
		return nil, err
	}
	if poolConfig.Target != nil && poolConfig.Target.Path != "" {
		ref = compute.VolumeRef(params.NodeId, path.Join(poolConfig.Target.Path, params.NewName))
	}
	description, err := config.Marshal()
	if err != nil {
		return nil, repositoryError(err, "cannot marshal clone volume XML")
	}
	flags := libvirt.StorageVolCreateFlags(0)
	if params.Format == compute.VolumeFormatQcow2 && repo.settings[params.NodeId].QcowPreallocMetadata {
		flags |= libvirt.STORAGE_VOL_CREATE_PREALLOC_METADATA
	}
	var created *libvirt.StorageVol
	if params.CloneMode.Effective() == compute.CloneModeOverlay {
		created, err = pool.StorageVolCreateXML(description, flags)
	} else {
		created, err = pool.StorageVolCreateXMLFrom(description, original, flags)
	}
	if err != nil {
		return nil, repositoryError(err, "cannot create cloned volume")
	}
	defer created.Free()
	if p, e := created.GetPath(); e == nil {
		ref = compute.VolumeRef(params.NodeId, p)
	} else {
		effects = append(effects, confirmed(ref, "created"))
		return nil, repositoryError(e, "cannot read created clone path")
	}
	effects = append(effects, confirmed(ref, "created"))
	if params.CloneMode.Effective() == compute.CloneModeCopy && params.Format == compute.VolumeFormatQcow2 && source.Size.Bytes() < params.NewSize.Bytes() {
		if err = created.Resize(params.NewSize.Bytes(), 0); err != nil {
			return nil, repositoryError(err, "cannot resize qcow2 volume after clone")
		}
	}
	result, err = repo.virVolumeToVolume(params.NodeId, pool, created)
	if err != nil {
		return result, err
	}
	if err = verifyClonedVolume(params, source, result); err != nil {
		return result, err
	}
	return result, nil
}

func (repo *VolumeRepository) Resize(path, node string, newSize compute.Size) error {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(node)

	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return repositoryError(err, "cannot lookup original volume")
	}

	if err := virVolume.Resize(newSize.Bytes(), 0); err != nil {
		return repositoryError(err, "resize failed")
	}
	return nil
}

func (repo *VolumeRepository) Delete(path, node string) error {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(node)

	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return repositoryError(err, "cannot lookup storage volume")
	}
	if repo.metadata[path].Protected || repo.metadata[compute.VolumePolicyKey(node, path)].Protected {
		return fmt.Errorf("%w: volume is protected", compute.ErrConflict)
	}
	if err := virVolume.Delete(libvirt.STORAGE_VOL_DELETE_NORMAL); err != nil {
		return repositoryError(err, "cannot delete volume")
	}
	return nil
}

type virStreamWrapper struct {
	steam *libvirt.Stream
}

func (w *virStreamWrapper) Write(p []byte) (int, error) {
	return w.steam.Send(p)
}

func (repo *VolumeRepository) Upload(path, nodeId string, content io.Reader, size uint64) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)
	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return repositoryError(err, "cannot lookup storage volume")
	}

	defer virVolume.Free()
	return uploadStream(context.Background(), conn, virVolume, content, size)
}

// ValidateCreate checks storage destinations without creating or resizing them.
func (repo *VolumeRepository) ValidateCreate(p compute.VolumeCreateParams) error {
	if strings.HasPrefix(p.Name, compute.ManagedImagePrefix) {
		return fmt.Errorf("%w: reserved managed image volume name", compute.ErrInvalid)
	}
	conn, err := repo.pool.Acquire(p.NodeId)
	if err != nil {
		return repositoryError(err, "backend validation failed")
	}
	defer repo.pool.Release(p.NodeId)
	pool, err := conn.LookupStoragePoolByName(p.Pool)
	if err != nil {
		return repositoryError(err, "backend validation failed")
	}
	defer pool.Free()
	active, err := pool.IsActive()
	if err != nil {
		return repositoryError(err, "backend validation failed")
	}
	if !active {
		return fmt.Errorf("%w: destination storage pool is inactive", compute.ErrConflict)
	}
	volume, err := pool.LookupStorageVolByName(p.Name)
	if err == nil {
		volume.Free()
		return fmt.Errorf("%w: destination volume %q already exists", compute.ErrConflict, p.Name)
	}
	if !errors.Is(err, libvirt.ERR_NO_STORAGE_VOL) {
		return repositoryError(err, "backend validation failed")
	}
	return nil
}
