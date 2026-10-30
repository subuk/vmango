package filesystem

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"subuk/vmango/auth"
	"sync"
	"time"
)

// AuthRepository owns a private local directory for the life of one process.
// Every operation reads fresh files; the mutex serializes durable mutations.
type AuthRepository struct {
	dir  string
	kind string
	mu   sync.Mutex
	lock *os.File
}

func NewAuthRepository(dir, kind string) (*AuthRepository, error) {
	if !filepath.IsAbs(dir) || (kind != "refresh" && kind != "personal") {
		return nil, errors.New("authentication store requires an absolute dedicated path and a valid kind")
	}
	if err := makeAuthDirectory(dir); err != nil {
		return nil, err
	}
	r := &AuthRepository{dir: dir, kind: kind}
	if err := r.check(dir, true); err != nil {
		return nil, err
	}
	lock, err := openAuthFile(filepath.Join(dir, ".lock"), true)
	if err != nil {
		return nil, err
	}
	r.lock = lock
	if err = r.check(r.lock.Name(), false); err == nil {
		err = lockAuthFile(r.lock)
	}
	if err != nil {
		r.lock.Close()
		return nil, fmt.Errorf("authentication store is unsafe or already owned by another process: %w", err)
	}
	if _, err = r.List(); err != nil {
		r.Close()
		return nil, err
	}
	if err = r.Cleanup(time.Now()); err != nil {
		r.Close()
		return nil, err
	}
	return r, nil
}

func (r *AuthRepository) Close() error { return r.lock.Close() }

func (r *AuthRepository) check(path string, dir bool) error { return checkAuthPath(path, dir) }

func (r *AuthRepository) read(id string) (auth.Record, error) {
	var rec auth.Record
	if !auth.ValidID(id) {
		return rec, auth.ErrInvalid
	}
	path := filepath.Join(r.dir, id+".json")
	if err := r.check(path, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rec, auth.ErrNotFound
		}
		return rec, err
	}
	f, err := openAuthFile(path, false)
	if err != nil {
		return rec, err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err = d.Decode(&rec); err != nil {
		return rec, fmt.Errorf("corrupt authentication record %s: %w", id, err)
	}
	var extra any
	if d.Decode(&extra) != io.EOF || rec.Version != 1 || rec.ID != id || rec.Kind != r.kind || rec.Owner.ID() == "" || !auth.ValidID(rec.Digest) || rec.CreatedAt.IsZero() || !rec.ExpiresAt.After(rec.CreatedAt) || len(rec.Previous) > auth.MaxRotations {
		return rec, fmt.Errorf("invalid authentication record %s", id)
	}
	for _, previous := range rec.Previous {
		if !auth.ValidID(previous) {
			return rec, fmt.Errorf("invalid rotation history in %s", id)
		}
	}
	return rec, nil
}

func (r *AuthRepository) syncDir() error { return syncAuthDirectory(r.dir) }
func (r *AuthRepository) write(rec auth.Record) error {
	if err := r.check(r.dir, true); err != nil {
		return err
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(r.dir, ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = replaceAuthFile(f.Name(), filepath.Join(r.dir, rec.ID+".json")); err != nil {
		return err
	}
	return r.syncDir()
}

func (r *AuthRepository) list() ([]auth.Record, error) {
	if err := r.check(r.dir, true); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, err
	}
	out := []auth.Record{}
	for _, entry := range entries {
		if entry.Name() == ".lock" {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".pending-") {
			// An interrupted write is never promoted; the committed record is authoritative.
			if err = r.check(filepath.Join(r.dir, entry.Name()), false); err != nil {
				return nil, err
			}
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unexpected file in dedicated authentication store: %s", entry.Name())
		}
		rec, err := r.read(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func (r *AuthRepository) List() ([]auth.Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.list()
}
func (r *AuthRepository) Update(id string, update func(*auth.Record) (bool, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, err := r.read(id)
	if err != nil {
		return err
	}
	changed, err := update(&rec)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return r.write(rec)
}
func (r *AuthRepository) Create(rec auth.Record, limit int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec.Version != 1 || !auth.ValidID(rec.ID) || rec.Kind != r.kind {
		return auth.ErrInvalid
	}
	if _, err := r.read(rec.ID); !errors.Is(err, auth.ErrNotFound) {
		if err != nil {
			return err
		}
		return errors.New("duplicate authentication record")
	}
	entries, err := r.list()
	if err != nil {
		return err
	}
	count, total := 0, 0
	for _, entry := range entries {
		if time.Now().Before(entry.ExpiresAt) {
			total++
			if !entry.Revoked && entry.Owner.ID() == rec.Owner.ID() {
				count++
			}
		}
	}
	// Retained revoked records count toward the global cap until expiry.
	if count >= limit || total >= 4096 {
		return auth.ErrLimit
	}
	return r.write(rec)
}
func (r *AuthRepository) Cleanup(now time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries, err := r.list()
	if err != nil {
		return err
	}
	changed := false
	for _, rec := range entries {
		if !now.Before(rec.ExpiresAt) {
			if err = os.Remove(filepath.Join(r.dir, rec.ID+".json")); err != nil {
				return err
			}
			changed = true
		}
	}
	files, err := os.ReadDir(r.dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".pending-") {
			if err = os.Remove(filepath.Join(r.dir, f.Name())); err != nil {
				return err
			}
			changed = true
		}
	}
	if changed {
		return r.syncDir()
	}
	return nil
}
