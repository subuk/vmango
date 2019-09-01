package compute

import (
	"path/filepath"
)

type VolumeMetadata struct {
	OsName    string
	OsVersion string
	OsArch    Arch
}

type Volume struct {
	Type       VolumeType
	Path       string
	Size       uint64 // MiB
	Pool       string
	Format     VolumeFormat
	AttachedTo string
	AttachedAs DeviceType
	Metadata   VolumeMetadata
}

func (volume *Volume) Base() string {
	return filepath.Base(volume.Path)
}

func (volume *Volume) SizeMb() uint64 {
	return volume.Size
}

func (volume *Volume) SizeGb() uint64 {
	return volume.Size / 1024
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
