package filesystem

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"subuk/vmango/compute"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"
)

// CloudInitTemplateRepository owns its private directory until Close. The
// process lock and mutex make revision comparisons and mutations atomic.
type CloudInitTemplateRepository struct {
	dir  string
	lock *os.File
	mu   sync.Mutex
}

func NewCloudInitTemplateRepository(dir string) (*CloudInitTemplateRepository, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("cloud-init template storage requires an absolute dedicated directory")
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
	if err = checkAuthPath(lock.Name(), false); err == nil {
		err = lockAuthFile(lock)
	}
	if err != nil {
		lock.Close()
		return nil, fmt.Errorf("cloud-init template storage is unsafe or already in use: %w", err)
	}
	repo := &CloudInitTemplateRepository{dir: dir, lock: lock}
	if _, err = repo.List(); err != nil {
		repo.Close()
		return nil, err
	}
	// A temporary file left by an interrupted write is never promoted.
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".pending-") {
				if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
					break
				}
			}
		}
	}
	if err != nil {
		repo.Close()
		return nil, err
	}
	return repo, nil
}

func (r *CloudInitTemplateRepository) Close() error { return r.lock.Close() }

func templateUUID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed != uuid.Nil && parsed.String() == id
}

func validStoredTemplate(template compute.CloudInitTemplate) bool {
	return templateUUID(template.ID) && templateUUID(template.Revision) &&
		strings.TrimSpace(template.Name) != "" && len(template.Name) <= 128 && utf8.ValidString(template.Name) && !strings.ContainsAny(template.Name, "\x00\r\n") &&
		len(template.Description) <= 1024 && utf8.ValidString(template.Description) && !strings.ContainsRune(template.Description, 0) &&
		strings.TrimSpace(template.UserData) != "" && compute.ValidateCloudInitUserData(template.UserData) == nil
}

func (r *CloudInitTemplateRepository) read(id string) (compute.CloudInitTemplate, error) {
	var template compute.CloudInitTemplate
	if !templateUUID(id) {
		return template, compute.ErrInvalid
	}
	if err := checkAuthPath(r.dir, true); err != nil {
		return template, err
	}
	path := filepath.Join(r.dir, id+".json")
	if err := checkAuthPath(path, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return template, compute.ErrNotFound
		}
		return template, err
	}
	f, err := openAuthFile(path, false)
	if err != nil {
		return template, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return template, err
	}
	if info.Size() > 2<<20 {
		return template, errors.New("cloud-init template record is too large")
	}
	decoder := json.NewDecoder(io.LimitReader(f, 2<<20))
	decoder.DisallowUnknownFields()
	// Decoder errors can contain private input, so never expose their text.
	if err = decoder.Decode(&template); err != nil {
		return template, errors.New("corrupt cloud-init template record")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || template.ID != id || !validStoredTemplate(template) {
		return compute.CloudInitTemplate{}, errors.New("invalid cloud-init template record")
	}
	return template, nil
}

func (r *CloudInitTemplateRepository) List() ([]compute.CloudInitTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := checkAuthPath(r.dir, true); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, err
	}
	result := []compute.CloudInitTemplate{}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".lock" {
			continue
		}
		if strings.HasPrefix(name, ".pending-") {
			if err := checkAuthPath(filepath.Join(r.dir, name), false); err != nil {
				return nil, err
			}
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			return nil, errors.New("unexpected file in cloud-init template directory")
		}
		template, err := r.read(strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, err
		}
		result = append(result, template)
	}
	return result, nil
}

func (r *CloudInitTemplateRepository) Get(id string) (compute.CloudInitTemplate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.read(id)
}

func (r *CloudInitTemplateRepository) write(template compute.CloudInitTemplate) error {
	if !validStoredTemplate(template) {
		return compute.ErrInvalid
	}
	if err := checkAuthPath(r.dir, true); err != nil {
		return err
	}
	data, err := json.Marshal(template)
	if err != nil {
		return err
	}
	f, err := createAuthTemp(r.dir)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
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
	if err = replaceAuthFile(f.Name(), filepath.Join(r.dir, template.ID+".json")); err != nil {
		return err
	}
	return syncAuthDirectory(r.dir)
}

func (r *CloudInitTemplateRepository) Create(template compute.CloudInitTemplate) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.read(template.ID); !errors.Is(err, compute.ErrNotFound) {
		if err != nil {
			return err
		}
		return compute.ErrConflict
	}
	return r.write(template)
}

func (r *CloudInitTemplateRepository) Update(template compute.CloudInitTemplate, expectedRevision string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	previous, err := r.read(template.ID)
	if err != nil {
		return err
	}
	if previous.Revision != expectedRevision {
		return compute.ErrConflict
	}
	return r.write(template)
}

func (r *CloudInitTemplateRepository) Delete(id, revision string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	previous, err := r.read(id)
	if err != nil {
		return err
	}
	if previous.Revision != revision {
		return compute.ErrConflict
	}
	if err = os.Remove(filepath.Join(r.dir, id+".json")); err != nil {
		return err
	}
	return syncAuthDirectory(r.dir)
}
