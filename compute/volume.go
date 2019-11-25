package compute

import (
	"path/filepath"
)

type VolumeMetadata struct {
	OsName    string
	OsVersion string
	OsArch    Arch
	Protected bool
}

type Volume struct {
	Type       VolumeType
	Path       string
	Size       uint64 // Bytes
	Pool       string
	Format     VolumeFormat
	AttachedTo string
	AttachedAs DeviceType
	Metadata   VolumeMetadata
}

func (v *Volume) SizeMb() uint64 {
	return v.Size / 1024 / 1024
}

func (volume *Volume) Base() string {
	return filepath.Base(volume.Path)
}

type VolumeRepository interface {
	Get(path string) (*Volume, error)
	GetByName(pool, name string) (*Volume, error)
	Create(pool, name string, format VolumeFormat, size uint64) (*Volume, error)
	Clone(originalPath, volumeName, poolName string, volumeFormat VolumeFormat, newSizeMb uint64) (*Volume, error)
	Resize(path string, newSize uint64) error
	Delete(path string) error
	List() ([]*Volume, error)
}
