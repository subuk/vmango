package compute

type VolumePoolRepository interface {
	List() ([]*VolumePool, error)
}

type VolumePool struct {
	Name string
	Size uint64 // Bytes
	Used uint64 // Bytes
	Free uint64 // Bytes
}

func (pool *VolumePool) UsagePercent() int {
	return int(100 * pool.Used / pool.Size)
}
