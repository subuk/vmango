package compute

type VolumePool struct {
	Name string
	Size Size
	Used Size
	Free Size
}

func (pool *VolumePool) UsagePercent() int {
	return int(100 * pool.Used.Bytes() / pool.Size.Bytes())
}
