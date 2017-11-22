package dal

import (
	"vmango/domain"
)

type StubImagerep struct {
	Data    []*domain.Image
	ListErr error
	GetErr  error
}

func (repo *StubImagerep) List(images *domain.ImageList) error {
	if repo.ListErr != nil {
		return repo.ListErr
	}
	for _, image := range repo.Data {
		*images = append(*images, image)
	}
	return nil
}

func (repo *StubImagerep) Get(needle *domain.Image) (bool, error) {
	if repo.GetErr != nil {
		return false, repo.GetErr
	}

	for _, image := range repo.Data {
		if image.Id == needle.Id {
			*needle = *image
			return true, nil
		}
	}
	return false, nil
}
