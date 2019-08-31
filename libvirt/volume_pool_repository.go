package libvirt

import (
	"subuk/vmango/compute"
	"subuk/vmango/util"

	"github.com/libvirt/libvirt-go-xml"
)

type VolumePoolRepository struct {
	pool *ConnectionPool
}

func NewVolumePoolRepository(pool *ConnectionPool) *VolumePoolRepository {
	return &VolumePoolRepository{pool: pool}
}

func (repo *VolumePoolRepository) List() ([]*compute.VolumePool, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virPools, err := conn.ListAllStoragePools(0)
	if err != nil {
		return nil, util.NewError(err, "cannot list storage pools")
	}
	volumePools := []*compute.VolumePool{}
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
		// Size: virPoolConfig.Capacity
		if virPoolConfig.Capacity != nil {
			volumePool.Size = ParseLibvirtSizeToMegabytes(virPoolConfig.Capacity.Unit, virPoolConfig.Capacity.Value)
		}
		if virPoolConfig.Allocation != nil {
			volumePool.Used = ParseLibvirtSizeToMegabytes(virPoolConfig.Allocation.Unit, virPoolConfig.Allocation.Value)
		}
		if virPoolConfig.Available != nil {
			volumePool.Free = ParseLibvirtSizeToMegabytes(virPoolConfig.Available.Unit, virPoolConfig.Available.Value)
		}
		volumePools = append(volumePools, volumePool)
	}
	return volumePools, nil
}
