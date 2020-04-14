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
	NodeId     string
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
	Get(path, node string) (*Volume, error)
	GetByName(pool, name, node string) (*Volume, error)
	Create(params VolumeCreateParams) (*Volume, error)
	Clone(params VolumeCloneParams) (*Volume, error)
	Resize(path, node string, newSize Size) error
	Delete(path, node string) error
	List(options VolumeListOptions) ([]*Volume, error)
}
