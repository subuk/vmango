package dal

import (
	"vmango/models"
)

type StubImagerep struct {
	Data    []*models.Image
	ListErr error
	GetErr  error
}

func (repo *StubImagerep) List(images *models.ImageList) error {
	if repo.ListErr != nil {
		return repo.ListErr
	}
	dataCopy := make([]*models.Image, len(repo.Data))
	copy(dataCopy, repo.Data)
	*images = *(&dataCopy)
	return nil
}

func (repo *StubImagerep) Get(needle *models.Image) (bool, error) {
	if repo.GetErr != nil {
		return false, repo.GetErr
	}

	for _, image := range repo.Data {
		if image.FullName == needle.FullName && image.Hypervisor == needle.Hypervisor {
			*needle = *image
			return true, nil
		}
	}
	return false, nil
}
