package libvirt

import (
	"fmt"
	"io"
	"subuk/vmango/compute"
	"subuk/vmango/util"
	"sync"
	"time"

	"github.com/rs/zerolog"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	libvirt "github.com/libvirt/libvirt-go"
)

type VolumeRepository struct {
	pool     *ConnectionPool
	metadata map[string]compute.VolumeMetadata
	logger   zerolog.Logger
}

func NewVolumeRepository(pool *ConnectionPool, metadata map[string]compute.VolumeMetadata, logger zerolog.Logger) *VolumeRepository {
	return &VolumeRepository{pool: pool, metadata: metadata, logger: logger}
}

func (repo *VolumeRepository) virVolumeToVolume(nodeId string, pool *libvirt.StoragePool, virVolume *libvirt.StorageVol) (*compute.Volume, error) {
	virVolumeXml, err := virVolume.GetXMLDesc(0)
	if err != nil {
		return nil, util.NewError(err, "cannot get volume info")
	}
	virVolumeConfig := &libvirtxml.StorageVolume{}
	if err := virVolumeConfig.Unmarshal(virVolumeXml); err != nil {
		return nil, util.NewError(err, "cannot unmarshal volume xml")
	}
	poolXml, err := pool.GetXMLDesc(0)
	if err != nil {
		return nil, util.NewError(err, "cannot get storage pool xml")
	}
	poolConfig := &libvirtxml.StoragePool{}
	if err := poolConfig.Unmarshal(poolXml); err != nil {
		return nil, util.NewError(err, "cannot unmarshal storage pool xml")
	}

	volume := &compute.Volume{}
	volume.NodeId = nodeId
	volume.Path = virVolumeConfig.Target.Path
	volume.Pool = poolConfig.Name
	volume.Metadata = repo.metadata[virVolumeConfig.Target.Path]
	volume.Size = ComputeSizeFromLibvirtSize(virVolumeConfig.Capacity.Unit, virVolumeConfig.Capacity.Value)

	switch getVolTargetFormatType(virVolumeConfig) {
	case "raw":
		volume.Format = compute.VolumeFormatRaw
	case "iso":
		volume.Format = compute.VolumeFormatIso
	case "qcow2":
		volume.Format = compute.VolumeFormatQcow2
	}

	return volume, nil
}

func (repo *VolumeRepository) fetchAttachedVm(conn *libvirt.Connect, volumes []*compute.Volume) error {
	domains, err := conn.ListAllDomains(0)
	if err != nil {
		return util.NewError(err, "cannot list domains")
	}
	for _, domain := range domains {
		domainXml, err := domain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
		if err != nil {
			return util.NewError(err, "cannot get domain xml description")
		}
		domainConfig := &libvirtxml.Domain{}
		if err := domainConfig.Unmarshal(domainXml); err != nil {
			return util.NewError(err, "cannot unmarshal domain xml")
		}
		for _, diskConfig := range domainConfig.Devices.Disks {
			attachedVolume := VirtualMachineAttachedVolumeFromDomainDiskConfig(diskConfig)
			if attachedVolume == nil {
				continue
			}
			for _, volume := range volumes {
				if volume.Path == attachedVolume.Path {
					volume.AttachedTo = domainConfig.Name
					volume.AttachedAs = attachedVolume.DeviceType
				}
			}
		}
	}
	return nil
}

func (repo *VolumeRepository) GetByName(pool, name, node string) (*compute.Volume, error) {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(node)

	virStoragePool, err := conn.LookupStoragePoolByName(pool)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup storage pool")
	}
	virVolume, err := virStoragePool.LookupStorageVolByName(name)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup volume")
	}
	volume, err := repo.virVolumeToVolume(node, virStoragePool, virVolume)
	if err != nil {
		return nil, util.NewError(err, "cannot parse volume")
	}
	if err := repo.fetchAttachedVm(conn, []*compute.Volume{volume}); err != nil {
		return nil, util.NewError(err, "cannot fetch attached vm")
	}
	return volume, nil
}

func (repo *VolumeRepository) Get(path, node string) (*compute.Volume, error) {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(node)

	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup volume by path %s", path)
	}
	pool, err := virVolume.LookupPoolByVolume()
	if err != nil {
		return nil, util.NewError(err, "cannot lookup pool for volume")
	}
	volume, err := repo.virVolumeToVolume(node, pool, virVolume)
	if err != nil {
		return nil, util.NewError(err, "cannot parse volume")
	}
	if err := repo.fetchAttachedVm(conn, []*compute.Volume{volume}); err != nil {
		return nil, util.NewError(err, "cannot fetch attached vm")
	}
	return volume, nil
}

func (repo *VolumeRepository) listNode(nodeId string) ([]*compute.Volume, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	volumes := []*compute.Volume{}

	pools, err := conn.ListAllStoragePools(0)
	if err != nil {
		return nil, util.NewError(err, "cannot list storage pools")
	}

	for _, pool := range pools {
		active, err := pool.IsActive()
		if err != nil {
			return nil, util.NewError(err, "cannot check if pool is active")
		}
		if !active {
			continue
		}
		virVolumes, err := pool.ListAllStorageVolumes(0)
		if err != nil {
			return nil, util.NewError(err, "cannot list storage volumes")
		}
		for _, virVolume := range virVolumes {
			volume, err := repo.virVolumeToVolume(nodeId, &pool, &virVolume)
			if err != nil {
				return nil, util.NewError(err, "cannot parse libvirt volume")
			}
			volumes = append(volumes, volume)
		}
	}
	if err := repo.fetchAttachedVm(conn, volumes); err != nil {
		return nil, util.NewError(err, "cannot fetch attached vm")
	}
	return volumes, nil
}

func (repo *VolumeRepository) List(options compute.VolumeListOptions) ([]*compute.Volume, error) {
	volumes := []*compute.Volume{}
	nodeVolumes := map[string][]*compute.Volume{}
	nodes := repo.pool.Nodes(options.NodeIds)
	wg := &sync.WaitGroup{}
	wg.Add(len(nodes))
	start := time.Now()
	for _, nodeId := range nodes {
		nodeVolumes[nodeId] = nil
		go func(nodeId string) {
			defer wg.Done()
			nodeStart := time.Now()
			vols, err := repo.listNode(nodeId)
			if err != nil {
				repo.logger.Warn().Str("node", nodeId).TimeDiff("took", time.Now(), nodeStart).Msg("cannot list volumes")
				return
			}
			repo.logger.Debug().Str("node", nodeId).TimeDiff("took", time.Now(), nodeStart).Msg("node volume list done")
			nodeVolumes[nodeId] = vols
		}(nodeId)
	}
	wg.Wait()
	for _, nodeId := range nodes {
		volumes = append(volumes, nodeVolumes[nodeId]...)
	}
	repo.logger.Debug().TimeDiff("took", time.Now(), start).Msg("full volume list done")
	return volumes, nil
}

func (repo *VolumeRepository) Create(params compute.VolumeCreateParams) (*compute.Volume, error) {
	conn, err := repo.pool.Acquire(params.NodeId)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(params.NodeId)

	virPool, err := conn.LookupStoragePoolByName(params.Pool)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup libvirt pool")
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
		return nil, util.NewError(err, "cannot marshal libvirt volume config")
	}
	virVolCreateFlags := libvirt.StorageVolCreateFlags(0)
	if params.Format == compute.VolumeFormatQcow2 {
		virVolCreateFlags |= libvirt.STORAGE_VOL_CREATE_PREALLOC_METADATA
	}

	virVolume, err := virPool.StorageVolCreateXML(virVolumeXml, virVolCreateFlags)
	if err != nil {
		return nil, util.NewError(err, "cannot create volume")
	}
	return repo.virVolumeToVolume(params.NodeId, virPool, virVolume)
}

func (repo *VolumeRepository) Clone(params compute.VolumeCloneParams) (*compute.Volume, error) {
	conn, err := repo.pool.Acquire(params.NodeId)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(params.NodeId)

	originalVirVolume, err := conn.LookupStorageVolByPath(params.OriginalPath)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup original volume")
	}

	originalVirVolumeInfo, err := originalVirVolume.GetInfo()
	if err != nil {
		return nil, util.NewError(err, "cannot get original volume info")
	}

	virPool, err := conn.LookupStoragePoolByName(params.NewPool)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup libvirt pool")
	}
	virPoolXml, err := virPool.GetXMLDesc(0)
	if err != nil {
		return nil, util.NewError(err, "cannot get pool xml")
	}
	virPoolConfig := libvirtxml.StoragePool{}
	if err := virPoolConfig.Unmarshal(virPoolXml); err != nil {
		return nil, util.NewError(err, "cannot parse pool xml")
	}

	virVolumeConfig := &libvirtxml.StorageVolume{}
	virVolumeConfig.Capacity = &libvirtxml.StorageVolumeSize{
		Unit:  ComputeSizeUnitToLibvirtUnit(params.NewSize.Unit),
		Value: params.NewSize.Value,
	}

	virVolumeConfig.Name = params.NewName
	switch params.Format {
	case compute.VolumeFormatRaw:
		virVolumeConfig.Target = &libvirtxml.StorageVolumeTarget{
			Format: &libvirtxml.StorageVolumeTargetFormat{
				Type: "raw",
			},
		}
	case compute.VolumeFormatQcow2:
		virVolumeConfig.Target = &libvirtxml.StorageVolumeTarget{
			Format: &libvirtxml.StorageVolumeTargetFormat{
				Type: "qcow2",
			},
		}
	}

	virVolumeXml, err := virVolumeConfig.Marshal()
	if err != nil {
		return nil, util.NewError(err, "cannot marshal libvirt volume config")
	}

	virVolCreateFlags := libvirt.StorageVolCreateFlags(0)
	if params.Format == compute.VolumeFormatQcow2 {
		virVolCreateFlags |= libvirt.STORAGE_VOL_CREATE_PREALLOC_METADATA
	}

	virVolume, err := virPool.StorageVolCreateXMLFrom(virVolumeXml, originalVirVolume, virVolCreateFlags)
	if err != nil {
		return nil, util.NewError(err, "cannot clone volume")
	}

	if params.Format == compute.VolumeFormatQcow2 && originalVirVolumeInfo.Capacity < params.NewSize.Bytes() {
		if err := virVolume.Resize(params.NewSize.Bytes(), 0); err != nil {
			return nil, util.NewError(err, "cannot resize qcow2 image after clone")
		}
	}
	return repo.virVolumeToVolume(params.NodeId, virPool, virVolume)
}

func (repo *VolumeRepository) Resize(path, node string, newSize compute.Size) error {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(node)

	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return util.NewError(err, "cannot lookup original volume")
	}

	if err := virVolume.Resize(newSize.Bytes(), 0); err != nil {
		return util.NewError(err, "resize failed")
	}
	return nil
}

func (repo *VolumeRepository) Delete(path, node string) error {
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(node)

	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return util.NewError(err, "cannot lookup storage volume")
	}
	if md, ok := repo.metadata[path]; ok && md.Protected {
		return fmt.Errorf("volume is protected")
	}
	if err := virVolume.Delete(libvirt.STORAGE_VOL_DELETE_NORMAL); err != nil {
		return util.NewError(err, "cannot delete volume")
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
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)
	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return util.NewError(err, "cannot lookup storage volume")
	}

	stream, err := conn.NewStream(0)
	if err != nil {
		return util.NewError(err, "cannot initialize upload stream")
	}
	if err := virVolume.Upload(stream, 0, size, 0); err != nil {
		return util.NewError(err, "cannot start upload")
	}
	if _, err := io.Copy(&virStreamWrapper{stream}, content); err != nil {
		return util.NewError(err, "upload failed")
	}
	if err := stream.Finish(); err != nil {
		return util.NewError(err, "cannot finalize upload")
	}
	return nil
}
