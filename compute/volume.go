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
	Size       Size
	Pool       string
	Format     VolumeFormat
	AttachedTo string
	AttachedAs DeviceType
	Metadata   VolumeMetadata
}

func (volume *Volume) Base() string {
	return filepath.Base(volume.Path)
}

type VolumeRepository interface {
	Get(path string) (*Volume, error)
	GetByName(pool, name string) (*Volume, error)
	Create(params VolumeCreateParams) (*Volume, error)
	Clone(params VolumeCloneParams) (*Volume, error)
	Resize(path string, newSize Size) error
	Delete(path string) error
	List() ([]*Volume, error)
}
