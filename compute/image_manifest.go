package compute

import (
	"fmt"
)

type ImageManifestOs struct {
	Name    string
	Version string
	Arch    Arch
}

type ImageManifest struct {
	Id         string
	VolumePath string
	Os         ImageManifestOs
}

func (m *ImageManifest) Description() string {
	return fmt.Sprintf("%s %s (%s)", m.Os.Name, m.Os.Version, m.Os.Arch)
}
