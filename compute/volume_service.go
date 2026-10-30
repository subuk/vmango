package compute

import (
	"errors"
	"fmt"
	"io"
)

var ErrVolumeNotFound = errors.New("volume not found")

type VolumeCloneParams struct {
	Cloud, StorageClass string
	NodeId              string
	Format              VolumeFormat
	OriginalPath        string
	NewName             string
	NewPool             string
	NewSize             Size
}

type VolumeCreateParams struct {
	Cloud, StorageClass string
	NodeId              string
	Name                string
	Pool                string
	Format              VolumeFormat
	Size                Size
}

type VolumeListOptions struct {
	NodeIds   []string
	PoolNames []string
}

type VolumeRepository interface {
	// ResolvePath maps a prospective pool/name to its backend path without writes.
	// It ensures create, attach, resize and delete commands share one lock identity.
	ResolvePath(node, pool, name string) (string, error)
	ValidateCreate(params VolumeCreateParams) error
	Get(path, node string) (*Volume, error)
	Create(params VolumeCreateParams) (*Volume, error)
	Clone(params VolumeCloneParams) (*Volume, error)
	Resize(path, node string, newSize Size) error
	Delete(path, node string) error
	Upload(path, nodeId string, content io.Reader, size uint64) error
	List(options VolumeListOptions) ([]*Volume, error)
}

type VolumeService struct {
	VolumeRepository
	Ownership VolumeOwnershipRepository
}

func NewVolumeService(repo VolumeRepository) *VolumeService {
	return &VolumeService{VolumeRepository: repo}
}

// Attachable excludes catalog sources independently of image architecture.
// Protection alone does not make an otherwise eligible volume unattachable.
func (s *VolumeService) Attachable(options VolumeListOptions, images *ImageService) ([]*Volume, error) {
	volumes, err := s.List(options)
	if err != nil {
		return nil, err
	}
	sources := map[string]bool{}
	if images != nil {
		sources, err = images.SourcePaths()
		if err != nil {
			return nil, err
		}
	}
	out := []*Volume{}
	for _, v := range volumes {
		if !v.Metadata.Hidden && v.AttachedTo == "" && !sources[v.Path] {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *VolumeService) ValidateAttachable(path, node string, images *ImageService) error {
	v, err := s.Get(path, node)
	if err != nil {
		return err
	}
	sources := map[string]bool{}
	if images != nil {
		sources, err = images.SourcePaths()
		if err != nil {
			return err
		}
	}
	if v.Metadata.Hidden || v.AttachedTo != "" || sources[v.Path] {
		return fmt.Errorf("%w: volume is not attachable", ErrConflict)
	}
	return nil
}
