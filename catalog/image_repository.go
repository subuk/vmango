// Package catalog holds the read-only image definitions loaded at startup.
package catalog

import (
	"fmt"
	"subuk/vmango/compute"
)

type ImageRepository struct{ images []compute.Image }

func copyImage(i compute.Image) compute.Image {
	if i.Download != nil {
		d := *i.Download
		i.Download = &d
	}
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
		if i.CloudID != "" && !compute.ValidCatalogID(i.ID) {
			return nil, fmt.Errorf("cloud %s image %q: invalid catalog identifier", i.CloudID, i.ID)
		}
		if err := i.Validate(); err != nil {
			return nil, err
		}
		if seen[i.CloudID+"\x00"+i.ID] {
			return nil, fmt.Errorf("duplicate image identifier %q", i.ID)
		}
		seen[i.CloudID+"\x00"+i.ID] = true
		r.images = append(r.images, copyImage(i))
	}
	return r, nil
}

func (r *ImageRepository) List(cloud string) ([]compute.Image, error) {
	result := []compute.Image{}
	for _, i := range r.images {
		if i.CloudID == cloud {
			result = append(result, copyImage(i))
		}
	}
	return result, nil
}

func (r *ImageRepository) Get(cloud, id string) (compute.Image, error) {
	for _, i := range r.images {
		if i.CloudID == cloud && i.ID == id {
			return copyImage(i), nil
		}
	}
	return compute.Image{}, fmt.Errorf("%w: image %q in cloud %q", compute.ErrNotFound, id, cloud)
}
