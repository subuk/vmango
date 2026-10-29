// Package catalog holds the read-only image definitions loaded at startup.
package catalog

import (
	"fmt"
	"subuk/vmango/compute"
)

type ImageRepository struct{ images []compute.Image }

func copyImage(i compute.Image) compute.Image {
	if i.Defaults.GuestAgent != nil {
		v := *i.Defaults.GuestAgent
		i.Defaults.GuestAgent = &v
	}
	return i
}

func NewImageRepository(images []compute.Image) (*ImageRepository, error) {
	r := &ImageRepository{}
	seen := map[string]bool{}
	for _, i := range images {
		if err := i.Validate(); err != nil {
			return nil, err
		}
		if seen[i.ID] {
			return nil, fmt.Errorf("duplicate image identifier %q", i.ID)
		}
		seen[i.ID] = true
		r.images = append(r.images, copyImage(i))
	}
	return r, nil
}

func (r *ImageRepository) List() ([]compute.Image, error) {
	result := make([]compute.Image, len(r.images))
	for n, i := range r.images {
		result[n] = copyImage(i)
	}
	return result, nil
}

func (r *ImageRepository) Get(id string) (compute.Image, error) {
	for _, i := range r.images {
		if i.ID == id {
			return copyImage(i), nil
		}
	}
	return compute.Image{}, fmt.Errorf("image %q not found", id)
}
