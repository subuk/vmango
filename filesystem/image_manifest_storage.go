package filesystem

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"subuk/vmango/compute"
	"subuk/vmango/util"
	"sync"
)

type ImageManifestStorage struct {
	filename string
	mu       *sync.RWMutex
}

func NewImageManifestStorage(filename string) (*ImageManifestStorage, error) {
	dirname := filepath.Dir(filename)
	if err := os.MkdirAll(dirname, 0755); err != nil {
		return nil, util.NewError(err, "cannot create base directory")
	}
	storage := &ImageManifestStorage{filename: filename, mu: &sync.RWMutex{}}
	return storage, nil
}

func (repo *ImageManifestStorage) load() ([]*compute.ImageManifest, error) {
	repo.mu.RLock()
	defer repo.mu.RUnlock()
	content, err := ioutil.ReadFile(repo.filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []*compute.ImageManifest{}, nil
		}
		return nil, util.NewError(err, "cannot open storage file")
	}
	images := []*compute.ImageManifest{}
	if err := json.Unmarshal(content, &images); err != nil {
		return nil, util.NewError(err, "cannot parse images file")
	}
	return images, nil
}

func (repo *ImageManifestStorage) save(images []*compute.ImageManifest) error {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	content, err := json.MarshalIndent(&images, "", "  ")
	if err != nil {
		return util.NewError(err, "cannot marshal images")
	}
	if err := ioutil.WriteFile(repo.filename, content, 0644); err != nil {
		return util.NewError(err, "cannot write images file")
	}
	return nil
}

func (repo *ImageManifestStorage) List(options compute.ImageManifestListOptions) ([]*compute.ImageManifest, error) {
	return repo.load()
}

func (repo *ImageManifestStorage) Get(volumePath string) (*compute.ImageManifest, error) {
	manifests, err := repo.load()
	if err != nil {
		return nil, util.NewError(err, "cannot load manifests")
	}
	for _, manifest := range manifests {
		if manifest.VolumePath == volumePath {
			return manifest, nil
		}
	}
	return nil, compute.ErrImageManifestNotFound
}

func (repo *ImageManifestStorage) Save(image *compute.ImageManifest) error {
	images, err := repo.load()
	if err != nil {
		return err
	}
	found := false
	for idx, i := range images {
		if i.VolumePath == image.VolumePath {
			images[idx] = image
			found = true
		}
	}
	if !found {
		images = append(images, image)
	}

	return repo.save(images)
}

func (repo *ImageManifestStorage) FindByVolumePaths(paths []string) (map[string]*compute.ImageManifest, error) {
	manifests, err := repo.load()
	if err != nil {
		return nil, util.NewError(err, "failed to load manifests")
	}
	result := map[string]*compute.ImageManifest{}
	for _, manifest := range manifests {
		if util.ArrayContainsString(paths, manifest.VolumePath) {
			result[manifest.VolumePath] = manifest
		}
	}
	return result, nil
}
