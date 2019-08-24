package compute

type Volume struct {
	Type       string
	Path       string
	Size       uint64 // MiB
	Pool       string
	AttachedTo string
}

type VolumePool struct {
	Name string
}

type VolumeRepository interface {
	Get(path string) (*Volume, error)
	List() ([]*Volume, error)
	Pools() ([]*VolumePool, error)
}
