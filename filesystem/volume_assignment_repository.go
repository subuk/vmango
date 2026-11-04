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

type volumeAssignmentRecord struct {
	Scope        string `json:"scope"`
	Node         string `json:"node"`
	Path         string `json:"path"`
	Identity     string `json:"identity"`
	Pool         string `json:"pool"`
	PoolIdentity string `json:"pool_identity,omitempty"`
	BackendKey   string `json:"backend_key,omitempty"`
	compute.VolumeAssignment
}

type volumeAssignmentStore struct {
	Version int                               `json:"version"`
	Records map[string]volumeAssignmentRecord `json:"records"`
}

// VolumeAssignmentRepository keeps namespace records on the application host.
// Its process lock is independent of the deletion-safety ownership repository.
type VolumeAssignmentRepository struct {
	dir    string
	lock   *os.File
	mu     sync.Mutex
	poison error
	// Injectable for focused persistence failure tests; production uses commit.
	write func(volumeAssignmentStore) error
}

func NewVolumeAssignmentRepository(dir string) (*VolumeAssignmentRepository, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("volume assignment directory must be absolute")
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
	if err := lockAuthFile(lock); err != nil {
		lock.Close()
		return nil, fmt.Errorf("volume assignment storage is already in use: %w", err)
	}
	r := &VolumeAssignmentRepository{dir: dir, lock: lock}
	r.write = r.commit
	if _, err := r.read(); err != nil {
		lock.Close()
		return nil, err
	}
	return r, nil
}

func (r *VolumeAssignmentRepository) Close() error { return r.lock.Close() }

func assignmentKey(scope, path string) string { return scope + "\x00" + path }

func (r *VolumeAssignmentRepository) read() (volumeAssignmentStore, error) {
	store := volumeAssignmentStore{Version: 1, Records: map[string]volumeAssignmentRecord{}}
	if r.poison != nil {
		return store, r.poison
	}
	if err := checkAuthPath(r.dir, true); err != nil {
		return store, fmt.Errorf("volume assignment storage unavailable: %w", err)
	}
	path := filepath.Join(r.dir, "volumes.json")
	if err := checkAuthPath(path, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return store, err
	}
	f, err := openAuthFile(path, false)
	if err != nil {
		return store, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(&store); err != nil {
		return store, errors.New("corrupt volume assignment storage")
	}
	var extra any
	if d.Decode(&extra) != io.EOF || store.Version != 1 || store.Records == nil {
		return store, errors.New("invalid or unsupported volume assignment storage")
	}
	for key, record := range store.Records {
		if key != assignmentKey(record.Scope, record.Path) || record.Scope == "" || record.Node == "" || record.Path == "" || record.Identity == "" || record.Pool == "" || !compute.ValidCatalogID(string(record.NamespaceID)) || !compute.ValidResourceLabel(record.Label) || (record.OwnerVM != "" && !compute.ValidCatalogID(record.OwnerVM)) {
			return store, errors.New("invalid volume assignment record")
		}
	}
	return store, nil
}

func volumeRecord(volume *compute.Volume, scope string, assignment compute.VolumeAssignment) (volumeAssignmentRecord, error) {
	if volume == nil || scope == "" || volume.NodeId == "" || volume.Path == "" {
		return volumeAssignmentRecord{}, fmt.Errorf("%w: volume placement is required", compute.ErrInvalid)
	}
	return volumeAssignmentRecord{Scope: scope, Node: volume.NodeId, Path: volume.Path, Identity: volume.Identity, Pool: volume.Pool, PoolIdentity: volume.PoolIdentity, BackendKey: volume.BackendKey, VolumeAssignment: assignment}, nil
}

func sameVolume(a, b volumeAssignmentRecord) bool {
	return a.Scope == b.Scope && a.Node == b.Node && a.Path == b.Path && a.Identity == b.Identity && a.Pool == b.Pool && a.PoolIdentity == b.PoolIdentity && a.BackendKey == b.BackendKey
}

func (r *VolumeAssignmentRepository) Get(volume *compute.Volume, scope string) (*compute.VolumeAssignment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := volumeRecord(volume, scope, compute.VolumeAssignment{})
	if err != nil {
		return nil, err
	}
	store, err := r.read()
	if err != nil {
		return nil, err
	}
	previous, ok := store.Records[assignmentKey(scope, volume.Path)]
	if !ok {
		return nil, nil
	}
	if !sameVolume(previous, record) {
		return nil, fmt.Errorf("%w: volume assignment backend identity mismatch", compute.ErrConflict)
	}
	return &previous.VolumeAssignment, nil
}

func (r *VolumeAssignmentRepository) CheckCreate(node, path, scope string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if node == "" || path == "" || scope == "" {
		return fmt.Errorf("%w: volume placement is required", compute.ErrInvalid)
	}
	store, err := r.read()
	if err != nil {
		return err
	}
	if _, exists := store.Records[assignmentKey(scope, path)]; exists {
		return fmt.Errorf("%w: destination has a stale volume assignment; recover its registry record before creating a replacement", compute.ErrConflict)
	}
	return nil
}

func (r *VolumeAssignmentRepository) Put(volume *compute.Volume, scope string, assignment compute.VolumeAssignment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := volumeRecord(volume, scope, assignment)
	if err != nil {
		return err
	}
	if record.Identity == "" || record.Pool == "" || !compute.ValidCatalogID(string(assignment.NamespaceID)) || !compute.ValidResourceLabel(assignment.Label) || (assignment.OwnerVM != "" && !compute.ValidCatalogID(assignment.OwnerVM)) {
		return fmt.Errorf("%w: namespace, label and volume identity are required", compute.ErrInvalid)
	}
	store, err := r.read()
	if err != nil {
		return err
	}
	key := assignmentKey(scope, volume.Path)
	if old, ok := store.Records[key]; ok {
		if !sameVolume(old, record) {
			return fmt.Errorf("%w: volume assignment backend identity mismatch", compute.ErrConflict)
		}
		if old.OwnerVM != assignment.OwnerVM && old.OwnerVM != "" {
			return fmt.Errorf("%w: VM artifact ownership cannot change", compute.ErrConflict)
		}
		if old.NamespaceID != assignment.NamespaceID {
			return fmt.Errorf("%w: volume is already assigned to another namespace", compute.ErrConflict)
		}
		if old == record {
			return nil
		}
	}
	store.Records[key] = record
	return r.persist(store)
}

// Delete is called with the captured identity only after backend deletion has
// been confirmed. It must never remove a replacement volume's assignment.
func (r *VolumeAssignmentRepository) Delete(volume *compute.Volume, scope string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, err := volumeRecord(volume, scope, compute.VolumeAssignment{})
	if err != nil {
		return err
	}
	store, err := r.read()
	if err != nil {
		return err
	}
	key := assignmentKey(scope, volume.Path)
	if old, ok := store.Records[key]; ok {
		if !sameVolume(old, record) {
			return fmt.Errorf("%w: volume assignment backend identity mismatch", compute.ErrConflict)
		}
		delete(store.Records, key)
		return r.persist(store)
	}
	return nil
}

func (r *VolumeAssignmentRepository) persist(store volumeAssignmentStore) error {
	if err := r.write(store); err != nil {
		r.poison = fmt.Errorf("volume assignment persistence failed; reopen storage before recovery: %w", err)
		return r.poison
	}
	return nil
}

func (r *VolumeAssignmentRepository) commit(store volumeAssignmentStore) error {
	data, err := json.Marshal(store)
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
	if err := replaceAuthFile(f.Name(), filepath.Join(r.dir, "volumes.json")); err != nil {
		return err
	}
	return syncAuthDirectory(r.dir)
}
