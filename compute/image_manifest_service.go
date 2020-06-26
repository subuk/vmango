package compute

import (
	"errors"
)

var ErrImageManifestNotFound = errors.New("image manifest not found")

type ImageManifestListOptions struct{}

type ImageManifestRepository interface {
	List(options ImageManifestListOptions) ([]*ImageManifest, error)
	Get(volumePath string) (*ImageManifest, error)
	Save(imageManifest *ImageManifest) error
}

type ImageManifestService struct {
	ImageManifestRepository
}

func NewImageManifestService(repo ImageManifestRepository) *ImageManifestService {
	return &ImageManifestService{repo}
}
