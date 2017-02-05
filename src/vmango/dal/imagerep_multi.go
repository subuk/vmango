package dal

import (
	"fmt"
	"vmango/models"
)

type MultiImagerep struct {
	repos map[string]Imagerep
}

func NewMultiImagerep(repos map[string]Imagerep) *MultiImagerep {
	return &MultiImagerep{
		repos: repos,
	}
}

func (multirep *MultiImagerep) List(images *models.ImageList) error {
	for repoName, repo := range multirep.repos {
		if err := repo.List(images); err != nil {
			return fmt.Errorf("failed to query repo %s: %s", repoName, err)
		}
	}
	return nil
}

func (multirep *MultiImagerep) Get(image *models.Image) (bool, error) {
	repo, exist := multirep.repos[image.Hypervisor]
	if !exist {
		return false, fmt.Errorf("imagerepo for hypervisor '%s' not configured", image.Hypervisor)
	}
	return repo.Get(image)
}
