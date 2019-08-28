package compute

import (
	"path/filepath"
)

type Volume struct {
	Type       string
	Path       string
	Size       uint64 // MiB
	Pool       string
	Format     VolumeFormat
	AttachedTo string
}

func (volume *Volume) Base() string {
	return filepath.Base(volume.Path)
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

type VolumeRepository interface {
	Get(path string) (*Volume, error)
	Create(pool, name string, format VolumeFormat, size uint64) (*Volume, error)
	Clone(originalPath, volumeName, poolName string, volumeFormat VolumeFormat, newSizeMb uint64) (*Volume, error)
	Resize(path string, newSize uint64) error
	Delete(path string) error
	List() ([]*Volume, error)
	Pools() ([]*VolumePool, error)
}
