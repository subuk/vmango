package compute

import (
	"fmt"
	"slices"
)

// Image describes a reusable launch configuration. SourcePath is resolved on
// the selected node; storage properties are never cached in the catalog.
type Image struct {
	ID                 string
	Name               string
	OSName             string
	OSVersion          string
	Arch               Arch
	SourcePath         string
	Defaults           ImageDefaults
	MinimumMemoryBytes uint64
	MinimumDiskBytes   uint64
}

type ImageDefaults struct {
	Firmware       string // Empty inherits the current behavior; auto, bios and efi are explicit.
	DiskBus        DeviceBus
	DiskController string
	NICModel       string
	GuestAgent     *bool
	Initialization string // Empty inherits the node's config drive format.
}

func (i Image) Validate() error {
	if i.ID == "" || i.SourcePath == "" || !slices.Contains([]Arch{ArchAmd64, ArchAarch64}, i.Arch) {
		return fmt.Errorf("image %q needs an identifier, source path and supported architecture", i.ID)
	}
	d := i.Defaults
	if !slices.Contains([]DeviceBus{DeviceBusUnknown, DeviceBusVirtio, DeviceBusScsi, DeviceBusIde}, d.DiskBus) {
		return fmt.Errorf("image %q: invalid disk bus", i.ID)
	}
	if !slices.Contains([]string{"", "auto", "bios", "efi"}, d.Firmware) {
		return fmt.Errorf("image %q: invalid firmware %q", i.ID, d.Firmware)
	}
	if !slices.Contains([]string{"", "none", "nocloud", "openstack"}, d.Initialization) {
		return fmt.Errorf("image %q: invalid initialization mode %q", i.ID, d.Initialization)
	}
	if d.DiskController != "" && d.DiskController != "virtio-scsi" {
		return fmt.Errorf("image %q: unsupported disk controller %q", i.ID, d.DiskController)
	}
	if d.DiskController != "" && d.DiskBus != DeviceBusScsi {
		return fmt.Errorf("image %q: disk_controller requires disk_bus = scsi", i.ID)
	}
	if d.NICModel != "" && !slices.Contains([]string{"virtio", "e1000", "rtl8139"}, d.NICModel) {
		return fmt.Errorf("image %q: unsupported NIC model %q", i.ID, d.NICModel)
	}
	return nil
}

type ImageRepository interface {
	List() ([]Image, error)
	Get(id string) (Image, error)
}

type ImageService struct{ ImageRepository }

func NewImageService(repo ImageRepository) *ImageService { return &ImageService{repo} }

// Available filters definitions by actual, visible, unattached source volumes.
func (s *ImageService) Available(node string, arch Arch, volumes []*Volume) ([]Image, error) {
	images, err := s.List()
	if err != nil {
		return nil, err
	}
	available := map[string]bool{}
	for _, v := range volumes {
		if v.NodeId == node && !v.Metadata.Hidden && v.AttachedTo == "" {
			available[v.Path] = true
		}
	}
	result := []Image{}
	for _, i := range images {
		if i.Arch == arch && available[i.SourcePath] {
			result = append(result, i)
		}
	}
	return result, nil
}

func (s *ImageService) SourcePaths() (map[string]bool, error) {
	images, err := s.List()
	if err != nil {
		return nil, err
	}
	paths := map[string]bool{}
	for _, i := range images {
		paths[i.SourcePath] = true
	}
	return paths, nil
}
