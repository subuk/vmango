package compute

type VolumePoolRepository interface {
	List() ([]*VolumePool, error)
}

type VolumePool struct {
	Name string
	Size uint64 // MiB
	Used uint64 // MiB
	Free uint64 // MiB
}

func (pool *VolumePool) UsagePercent() int {
	return int(100 * pool.Used / pool.Free)
}

func (pool *VolumePool) FreeGB() uint64 {
	return pool.Free / 1024
}

func (pool *VolumePool) SizeGb() uint64 {
	return pool.Size / 1024
}
