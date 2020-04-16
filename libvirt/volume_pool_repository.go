package libvirt

import (
	"subuk/vmango/compute"
	"subuk/vmango/util"
	"time"

	libvirtxml "github.com/libvirt/libvirt-go-xml"
	"github.com/rs/zerolog"
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
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	virPools, err := conn.ListAllStoragePools(0)
	if err != nil {
		return nil, util.NewError(err, "cannot list storage pools")
	}
	for _, virPool := range virPools {
		virPoolXml, err := virPool.GetXMLDesc(0)
		if err != nil {
			return nil, util.NewError(err, "cannot get pool name")
		}
		virPoolConfig := &libvirtxml.StoragePool{}
		if err := virPoolConfig.Unmarshal(virPoolXml); err != nil {
			return nil, util.NewError(err, "cannot unmarshal volume pool xml")
		}
		active, err := virPool.IsActive()
		if err != nil {
			return nil, util.NewError(err, "cannot check if pool %s is active", virPoolConfig.Name)
		}
		if !active {
			continue
		}
		volumePool := &compute.VolumePool{
			Name: virPoolConfig.Name,
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
			continue
		}
		volumePools = append(volumePools, nodeVolumePools...)
	}
	repo.logger.Debug().TimeDiff("took", time.Now(), start).Msg("volume pool list finished")
	return volumePools, nil
}
