package libvirt

import (
	"fmt"
	"subuk/vmango/compute"
	"time"

	"github.com/rs/zerolog"
	libvirtxml "libvirt.org/go/libvirtxml"
)

type VolumePoolRepository struct {
	pool   *ConnectionPool
	logger zerolog.Logger
}

func NewVolumePoolRepository(pool *ConnectionPool, logger zerolog.Logger) *VolumePoolRepository {
	return &VolumePoolRepository{pool: pool, logger: logger}
}

func (repo *VolumePoolRepository) listNode(nodeId string) ([]*compute.VolumePool, error) {
	volumePools := []*compute.VolumePool{}
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	virPools, err := conn.ListAllStoragePools(0)
	if err != nil {
		return nil, repositoryError(err, "cannot list storage pools")
	}
	for _, virPool := range virPools {
		activeForRefresh, refreshErr := virPool.IsActive()
		if refreshErr != nil {
			return nil, refreshErr
		}
		if activeForRefresh {
			if refreshErr = virPool.Refresh(0); refreshErr != nil {
				return nil, refreshErr
			}
		}
		virPoolXml, err := virPool.GetXMLDesc(0)
		if err != nil {
			return nil, repositoryError(err, "cannot get pool name")
		}
		virPoolConfig := &libvirtxml.StoragePool{}
		if err := virPoolConfig.Unmarshal(virPoolXml); err != nil {
			return nil, repositoryError(err, "cannot unmarshal volume pool xml")
		}
		active, err := virPool.IsActive()
		if err != nil {
			return nil, repositoryError(err, "cannot check if pool %s is active", virPoolConfig.Name)
		}
		if !active {
			continue
		}
		volumePool := &compute.VolumePool{
			Name:   virPoolConfig.Name,
			NodeId: nodeId,
		}
		if virPoolConfig.Capacity != nil {
			volumePool.Size = ComputeSizeFromLibvirtSize(virPoolConfig.Capacity.Unit, virPoolConfig.Capacity.Value)
		}
		if virPoolConfig.Allocation != nil {
			volumePool.Used = ComputeSizeFromLibvirtSize(virPoolConfig.Allocation.Unit, virPoolConfig.Allocation.Value)
		}
		if virPoolConfig.Available != nil {
			volumePool.Free = ComputeSizeFromLibvirtSize(virPoolConfig.Available.Unit, virPoolConfig.Available.Value)
		}
		// Bound logical capacity as well as physical free space. Sparse QCOW2
		// creation must not allow sequential admissions to overbook the pool.
		stored, err := virPool.ListAllStorageVolumes(0)
		if err != nil {
			return nil, err
		}
		committed := uint64(0)
		for _, v := range stored {
			info, err := v.GetInfo()
			v.Free()
			if err != nil {
				return nil, err
			}
			size := max(info.Capacity, info.Allocation)
			if size > ^uint64(0)-committed {
				return nil, fmt.Errorf("storage commitment overflow")
			}
			committed += size
		}
		logicalFree := uint64(0)
		if committed < volumePool.Size.Bytes() {
			logicalFree = volumePool.Size.Bytes() - committed
		}
		if logicalFree < volumePool.Free.Bytes() {
			volumePool.Free = compute.NewSize(logicalFree, compute.SizeUnitB)
		}
		volumePools = append(volumePools, volumePool)
	}
	return volumePools, nil
}

func (repo *VolumePoolRepository) List(options compute.VolumePoolListOptions) ([]*compute.VolumePool, error) {
	volumePools := []*compute.VolumePool{}
	start := time.Now()
	nodes := repo.pool.Nodes(options.NodeIds)
	for _, nodeId := range nodes {
		nodeStart := time.Now()
		nodeVolumePools, err := repo.listNode(nodeId)
		if err != nil {
			repo.logger.Warn().Err(err).Str("node", nodeId).TimeDiff("took", time.Now(), nodeStart).Msg("cannot list volume pools")
			return nil, err
		}
		volumePools = append(volumePools, nodeVolumePools...)
	}
	repo.logger.Debug().TimeDiff("took", time.Now(), start).Msg("volume pool list finished")
	return volumePools, nil
}
