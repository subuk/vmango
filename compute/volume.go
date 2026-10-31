package compute

import (
	"path/filepath"
)

type VolumeMetadata struct {
	Protected bool
	Hidden    bool
}

type Volume struct {
	Identity   string
	NodeId     string
	Path       string
	Name       string
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

// VolumePolicyKey scopes image-derived policy to its configured backend host.
func VolumePolicyKey(node, path string) string { return node + "\x00" + path }
