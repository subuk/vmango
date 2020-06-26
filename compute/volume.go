package compute

import (
	"path/filepath"
)

type Volume struct {
	NodeId     string
	Path       string
	Name       string
	Size       Size
	Pool       string
	Format     VolumeFormat
	AttachedTo string
	AttachedAs DeviceType
	Image      string
}

func (volume *Volume) Available() bool {
	return volume.AttachedTo == "" && volume.Image == ""
}

func (volume *Volume) Base() string {
	return filepath.Base(volume.Path)
}
