package compute

import (
	"errors"
	"io"
)

var ErrVolumeNotFound = errors.New("volume not found")

type VolumeCloneParams struct {
	NodeId       string
	Format       VolumeFormat
	OriginalPath string
	NewName      string
	NewPool      string
	NewSize      Size
}

type VolumeCreateParams struct {
	NodeId string
	Name   string
	Pool   string
	Format VolumeFormat
	Size   Size
}

type VolumeListOptions struct {
	NodeIds   []string
	PoolNames []string
}

type VolumeRepository interface {
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
}

func NewVolumeService(repo VolumeRepository) *VolumeService {
	return &VolumeService{repo}
}
