package filesystem

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"subuk/vmango/compute"
	"sync"
)

// VolumeOwnershipRepository records exact backend resources created by Vmango.
// It uses a separate private directory and the same durable replacement helpers
// as task history. A process lock excludes competing Vmango writers.
type VolumeOwnershipRepository struct {
	dir  string
	lock *os.File
	mu   sync.Mutex
}

func NewVolumeOwnershipRepository(dir string) (*VolumeOwnershipRepository, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("volume ownership metadata directory must be absolute")
	}
	if err := makeAuthDirectory(dir); err != nil {
		return nil, err
	}
	if err := checkAuthPath(dir, true); err != nil {
		return nil, err
	}
	lock, err := openAuthFile(filepath.Join(dir, ".lock"), true)
	if err != nil {
		return nil, err
	}
	if err = lockAuthFile(lock); err != nil {
		lock.Close()
		return nil, fmt.Errorf("volume ownership metadata storage is already in use: %w", err)
	}
	r := &VolumeOwnershipRepository{dir: dir, lock: lock}
	if _, err = r.read(); err != nil {
		lock.Close()
		return nil, err
	}
	return r, nil
}
func (r *VolumeOwnershipRepository) Close() error { return r.lock.Close() }
func (r *VolumeOwnershipRepository) read() (map[string]compute.VolumeOwnership, error) {
	result := map[string]compute.VolumeOwnership{}
	path := filepath.Join(r.dir, "volumes.json")
	if err := checkAuthPath(path, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return result, nil
		}
		return nil, err
	}
	f, err := openAuthFile(path, false)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&result); err != nil {
		return nil, fmt.Errorf("invalid volume ownership metadata: %w", err)
	}
	var extra any
	if d.Decode(&extra) != io.EOF || result == nil {
		return nil, errors.New("invalid volume ownership metadata contents")
	}
	return result, nil
}
func (r *VolumeOwnershipRepository) Get(key string) (compute.VolumeOwnership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	all, err := r.read()
	if err != nil {
		return compute.VolumeOwnership{}, err
	}
	a, ok := all[key]
	if !ok {
		return a, compute.ErrNotFound
	}
	return a, nil
}
func (r *VolumeOwnershipRepository) Put(key string, a compute.VolumeOwnership) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	all, err := r.read()
	if err != nil {
		return err
	}
	all[key] = a
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	f, err := createAuthTemp(r.dir)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = replaceAuthFile(f.Name(), filepath.Join(r.dir, "volumes.json")); err != nil {
		return err
	}
	return syncAuthDirectory(r.dir)
}
