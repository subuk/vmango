package libvirt

import (
	"fmt"
	"subuk/vmango/compute"
	"subuk/vmango/util"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	libvirt "github.com/libvirt/libvirt-go"
)

type VolumeRepository struct {
	pool     *ConnectionPool
	metadata map[string]compute.VolumeMetadata
}

func NewVolumeRepository(pool *ConnectionPool, metadata map[string]compute.VolumeMetadata) *VolumeRepository {
	return &VolumeRepository{pool: pool, metadata: metadata}
}

func (repo *VolumeRepository) virVolumeToVolume(pool *libvirt.StoragePool, virVolume *libvirt.StorageVol) (*compute.Volume, error) {
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
	volume.Type = compute.NewVolumeType(virVolumeConfig.Type)
	volume.Path = virVolumeConfig.Target.Path
	volume.Pool = poolConfig.Name
	volume.Format = compute.FormatUnknown
	volume.Metadata = repo.metadata[virVolumeConfig.Target.Path]
	volume.Size = ComputeSizeFromLibvirtSize(virVolumeConfig.Capacity.Unit, virVolumeConfig.Capacity.Value)

	switch poolConfig.Type {
	case "logical":
		volume.Format = compute.FormatRaw
	}

	if virVolumeConfig.Target != nil && virVolumeConfig.Target.Format != nil {
		switch virVolumeConfig.Target.Format.Type {
		case "iso":
			volume.Format = compute.FormatIso
		case "qcow2":
			volume.Format = compute.FormatQcow2
		case "raw":
			volume.Format = compute.FormatRaw
		}
	}
	return volume, nil
}

func (repo *VolumeRepository) fetchAttachedVm(conn *libvirt.Connect, volumes []*compute.Volume) error {
	domains, err := conn.ListAllDomains(0)
	if err != nil {
		return util.NewError(err, "cannot list domains")
	}
	for _, domain := range domains {
		domainXml, err := domain.GetXMLDesc(libvirt.DOMAIN_XML_MIGRATABLE)
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

func (repo *VolumeRepository) GetByName(pool, name string) (*compute.Volume, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virStoragePool, err := conn.LookupStoragePoolByName(pool)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup storage pool")
	}
	virVolume, err := virStoragePool.LookupStorageVolByName(name)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup volume")
	}
	volume, err := repo.virVolumeToVolume(virStoragePool, virVolume)
	if err != nil {
		return nil, util.NewError(err, "cannot parse volume")
	}
	if err := repo.fetchAttachedVm(conn, []*compute.Volume{volume}); err != nil {
		return nil, util.NewError(err, "cannot fetch attached vm")
	}
	return volume, nil
}

func (repo *VolumeRepository) Get(path string) (*compute.Volume, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup volume by path %s", path)
	}
	pool, err := virVolume.LookupPoolByVolume()
	if err != nil {
		return nil, util.NewError(err, "cannot lookup pool for volume")
	}
	volume, err := repo.virVolumeToVolume(pool, virVolume)
	if err != nil {
		return nil, util.NewError(err, "cannot parse volume")
	}
	if err := repo.fetchAttachedVm(conn, []*compute.Volume{volume}); err != nil {
		return nil, util.NewError(err, "cannot fetch attached vm")
	}
	return volume, nil
}

func (repo *VolumeRepository) List() ([]*compute.Volume, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	pools, err := conn.ListAllStoragePools(0)
	if err != nil {
		return nil, util.NewError(err, "cannot list storage pools")
	}

	volumes := []*compute.Volume{}
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
			volume, err := repo.virVolumeToVolume(&pool, &virVolume)
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

func (repo *VolumeRepository) Create(params compute.VolumeCreateParams) (*compute.Volume, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

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
	if params.Format == compute.FormatQcow2 {
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
	if params.Format == compute.FormatQcow2 {
		virVolCreateFlags |= libvirt.STORAGE_VOL_CREATE_PREALLOC_METADATA
	}

	virVolume, err := virPool.StorageVolCreateXML(virVolumeXml, virVolCreateFlags)
	if err != nil {
		return nil, util.NewError(err, "cannot create volume")
	}
	return repo.virVolumeToVolume(virPool, virVolume)
}

func (repo *VolumeRepository) Clone(params compute.VolumeCloneParams) (*compute.Volume, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

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

	if virPoolConfig.Type == "logical" {
		params.Format = compute.FormatRaw
	}

	virVolumeConfig := &libvirtxml.StorageVolume{}
	virVolumeConfig.Capacity = &libvirtxml.StorageVolumeSize{
		Unit:  ComputeSizeUnitToLibvirtUnit(params.NewSize.Unit),
		Value: params.NewSize.Value,
	}

	virVolumeConfig.Name = params.NewName
	switch params.Format {
	case compute.FormatRaw:
		virVolumeConfig.Target = &libvirtxml.StorageVolumeTarget{
			Format: &libvirtxml.StorageVolumeTargetFormat{
				Type: "raw",
			},
		}
	case compute.FormatQcow2:
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
	if params.Format == compute.FormatQcow2 {
		virVolCreateFlags |= libvirt.STORAGE_VOL_CREATE_PREALLOC_METADATA
	}

	virVolume, err := virPool.StorageVolCreateXMLFrom(virVolumeXml, originalVirVolume, virVolCreateFlags)
	if err != nil {
		return nil, util.NewError(err, "cannot clone volume")
	}

	if params.Format == compute.FormatQcow2 && originalVirVolumeInfo.Capacity < params.NewSize.Bytes() {
		if err := virVolume.Resize(params.NewSize.Bytes(), 0); err != nil {
			return nil, util.NewError(err, "cannot resize qcow2 image after clone")
		}
	}
	return repo.virVolumeToVolume(virPool, virVolume)
}

func (repo *VolumeRepository) Resize(path string, newSize compute.Size) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virVolume, err := conn.LookupStorageVolByPath(path)
	if err != nil {
		return util.NewError(err, "cannot lookup original volume")
	}

	if err := virVolume.Resize(newSize.Bytes(), 0); err != nil {
		return util.NewError(err, "resize failed")
	}
	return nil
}

func (repo *VolumeRepository) Delete(path string) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

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
