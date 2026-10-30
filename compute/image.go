package compute

import (
	"encoding/hex"
	"fmt"
	"net/url"
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
	Download           *ImageDownload
	Defaults           ImageDefaults
	MinimumMemoryBytes uint64
	MinimumDiskBytes   uint64
}

// ImageDownload describes exact artifact bytes, independently of launch defaults.
// SizeBytes is optional HTTP size information, never guest disk capacity.
type ImageDownload struct {
	URL       string
	SHA256    string
	Format    VolumeFormat
	SizeBytes uint64
}

func (i Image) Managed() bool { return i.Download != nil }

type ImageDefaults struct {
	Firmware       string // Empty inherits the current behavior; auto, bios and efi are explicit.
	DiskBus        DeviceBus
	DiskController string
	NICModel       string
	GuestAgent     *bool
	Initialization string // Empty inherits the node's config drive format.
}

func (i Image) Validate() error {
	if i.ID == "" || (!i.Managed() && i.SourcePath == "") || !slices.Contains([]Arch{ArchAmd64, ArchAarch64}, i.Arch) {
		return fmt.Errorf("image %q needs an identifier, source path and supported architecture", i.ID)
	}
	if i.Managed() {
		d := i.Download
		u, err := url.Parse(d.URL)
		h, hashErr := hex.DecodeString(d.SHA256)
		if i.SourcePath != "" || err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" || hashErr != nil || len(h) != 32 || (d.Format != VolumeFormatRaw && d.Format != VolumeFormatQcow2) {
			return fmt.Errorf("image %q: downloadable sources require an HTTP(S) URL, SHA-256 and raw/qcow2 source_format, without path or URL credentials", i.ID)
		}
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

type ImageService struct {
	ImageRepository
	ManagedSources *ManagedImageService
	PinnedBindings map[string]ImageBinding
	Preflight      bool
}

// Snapshot uses the accepted definitions and bindings without consulting a
// subsequently edited catalog. Launch defaults and artifact identity stay pinned.
type pinnedImages []Image

func (p pinnedImages) List() ([]Image, error) { return p, nil }
func (p pinnedImages) Get(id string) (Image, error) {
	for _, i := range p {
		if i.ID == id {
			return i, nil
		}
	}
	return Image{}, fmt.Errorf("image %q is not pinned", id)
}
func (s *ImageService) Snapshot(images []Image, bindings map[string]ImageBinding, preflight bool) *ImageService {
	return &ImageService{ImageRepository: pinnedImages(images), ManagedSources: s.ManagedSources, PinnedBindings: bindings, Preflight: preflight}
}
func (s *ImageService) Source(i Image, node string, volumes *VolumeService) (*Volume, error) {
	if !i.Managed() {
		return volumes.Get(i.SourcePath, node)
	}
	if s.ManagedSources == nil {
		return nil, fmt.Errorf("managed image storage is unavailable")
	}
	b, ok := s.PinnedBindings[i.ID]
	if !ok {
		var err error
		b, err = s.ManagedSources.Bind(node, i)
		if err != nil {
			return nil, err
		}
	}
	if s.Preflight {
		return &Volume{NodeId: node, Path: b.Path, Pool: b.Pool, Name: b.Name, Size: NewSize(1, SizeUnitB), Format: i.Download.Format}, nil
	}
	return s.ManagedSources.ready(b, i)
}

func NewImageService(repo ImageRepository) *ImageService { return &ImageService{ImageRepository: repo} }

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
		if i.Arch == arch && (available[i.SourcePath] || (i.Managed() && s.ManagedSources != nil)) {
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
		if !i.Managed() {
			paths[i.SourcePath] = true
		}
	}
	return paths, nil
}

// Compatible checks catalog launch requirements against actual backend capabilities.
func (i Image) Compatible(c VirtualMachineCapabilities) bool {
	d := i.Defaults
	return c.Arch == i.Arch && (d.Firmware == "" || d.Firmware == "auto" || slices.Contains(c.Firmwares, d.Firmware)) && (d.DiskBus == DeviceBusUnknown || slices.Contains(c.DiskBuses, d.DiskBus)) && (d.NICModel == "" || slices.Contains(c.NICModels, d.NICModel)) && (d.DiskController == "" || slices.Contains(c.DiskControllers, d.DiskController))
}
