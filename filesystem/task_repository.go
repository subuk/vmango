package filesystem

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"subuk/vmango/tasks"
	"sync"
)

// TaskRepository uses one atomic snapshot to commit task state, command,
// deduplication identity and all resource locks together. A process-wide file
// lock excludes other executors; private file helpers also reject symlinks.
type TaskRepository struct {
	dir  string
	lock *os.File
	mu   sync.Mutex
}

func NewTaskRepository(dir string) (*TaskRepository, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("task storage requires an absolute dedicated directory")
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
	r := &TaskRepository{dir: dir, lock: lock}
	if err = checkAuthPath(lock.Name(), false); err == nil {
		err = lockAuthFile(lock)
	}
	if err != nil {
		lock.Close()
		return nil, fmt.Errorf("task storage is unsafe or already in use: %w", err)
	}
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if name == ".lock" || name == "tasks.json" {
				continue
			}
			if !strings.HasPrefix(name, ".pending-") {
				err = errors.New("unexpected file in dedicated task directory")
				break
			}
			if err = checkAuthPath(filepath.Join(dir, name), false); err != nil {
				break
			}
			if err = os.Remove(filepath.Join(dir, name)); err != nil {
				break
			}
		}
	}
	if err == nil {
		_, err = r.Read()
	}
	if err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}
func (r *TaskRepository) Close() error { return r.lock.Close() }
func (r *TaskRepository) read() (tasks.Store, error) {
	state := tasks.Store{Version: 1, Records: map[string]tasks.Record{}}
	path := filepath.Join(r.dir, "tasks.json")
	if err := checkAuthPath(path, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return state, nil
		}
		return state, err
	}
	f, err := openAuthFile(path, false)
	if err != nil {
		return state, err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err = d.Decode(&state); err != nil {
		return state, fmt.Errorf("corrupt task storage: %w", err)
	}
	var extra any
	if d.Decode(&extra) != io.EOF || state.Version != 1 || state.Records == nil {
		return state, errors.New("invalid task storage version or contents")
	}
	seenKeys := map[string]bool{}
	for id, rec := range state.Records {
		if seenKeys[rec.KeyHash] || len(rec.Locks) == 0 || (rec.Task.State.Terminal() && rec.Task.FinishedAt == nil) || (!rec.Task.State.Terminal() && !rec.Task.BlocksResources) {
			return state, errors.New("invalid task identities, locks or lifecycle")
		}
		seenKeys[rec.KeyHash] = true
		if rec.Task.ID != id || rec.Task.CreatedAt.IsZero() || rec.KeyHash == "" || rec.Fingerprint == "" || (!rec.Task.State.Terminal() && rec.Task.State != tasks.Queued && rec.Task.State != tasks.Running) || (!rec.Task.State.Terminal() && len(rec.Payload) == 0) {
			return state, fmt.Errorf("invalid task record %s", id)
		}
	}
	return state, nil
}
func (r *TaskRepository) Read() (tasks.Store, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.read()
}
func (r *TaskRepository) Update(update func(*tasks.Store) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, err := r.read()
	if err != nil {
		return err
	}
	if err = update(&state); err != nil {
		return err
	}
	if err = checkAuthPath(r.dir, true); err != nil {
		return err
	}
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	f, err := createAuthTemp(r.dir)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = replaceAuthFile(f.Name(), filepath.Join(r.dir, "tasks.json")); err != nil {
		return err
	}
	return syncAuthDirectory(r.dir)
}
