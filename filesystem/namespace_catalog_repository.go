package filesystem

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"subuk/vmango/compute"
	"sync"

	"golang.org/x/crypto/ssh"
)

type namespaceCatalog struct {
	Keys      map[string]string                    `json:"keys"`
	Templates map[string]compute.CloudInitTemplate `json:"templates"`
}
type namespaceCatalogStore struct {
	Version    int                                      `json:"version"`
	Namespaces map[compute.NamespaceID]namespaceCatalog `json:"namespaces"`
}

// NamespaceCatalogRepository holds the process lock for all namespace catalogs.
// Each mutation durably replaces the complete versioned store; keys and template
// revision comparisons are serialized with migration and all other writers.
type NamespaceCatalogRepository struct {
	dir    string
	lock   *os.File
	mu     sync.Mutex
	poison error
	commit func(namespaceCatalogStore) error
}

func NewNamespaceCatalogRepository(dir string) (*NamespaceCatalogRepository, error) {
	if !filepath.IsAbs(dir) {
		return nil, errors.New("namespace catalog directory must be absolute")
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
		return nil, fmt.Errorf("namespace catalog storage is unsafe or already in use: %w", err)
	}
	r := &NamespaceCatalogRepository{dir: dir, lock: lock}
	r.commit = r.write
	if _, err := r.read(); err != nil {
		lock.Close()
		return nil, err
	}
	return r, nil
}
func (r *NamespaceCatalogRepository) Close() error { return r.lock.Close() }

func parseCatalogKey(value string) (*compute.Key, error) {
	input := bytes.TrimSpace([]byte(value))
	pub, comment, options, rest, err := ssh.ParseAuthorizedKey(input)
	if err != nil || len(bytes.TrimSpace(rest)) != 0 {
		return nil, errors.New("invalid SSH public key")
	}
	return &compute.Key{Type: pub.Type(), Value: append([]byte(nil), input...), Comment: comment, Options: options, Fingerprint: ssh.FingerprintLegacyMD5(pub)}, nil
}
func (r *NamespaceCatalogRepository) read() (namespaceCatalogStore, error) {
	empty := namespaceCatalogStore{Version: 1, Namespaces: map[compute.NamespaceID]namespaceCatalog{}}
	if r.poison != nil {
		return empty, r.poison
	}
	if err := checkAuthPath(r.dir, true); err != nil {
		return empty, err
	}
	path := filepath.Join(r.dir, "catalogs.json")
	if err := checkAuthPath(path, false); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return empty, nil
		}
		return empty, err
	}
	f, err := openAuthFile(path, false)
	if err != nil {
		return empty, err
	}
	defer f.Close()
	var store namespaceCatalogStore
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(&store); err != nil {
		return empty, errors.New("corrupt namespace catalog storage")
	}
	var extra any
	if d.Decode(&extra) != io.EOF || store.Version != 1 || store.Namespaces == nil {
		return empty, errors.New("unsupported or invalid namespace catalog storage")
	}
	for ns, catalog := range store.Namespaces {
		if !compute.ValidCatalogID(string(ns)) || catalog.Keys == nil || catalog.Templates == nil {
			return empty, errors.New("invalid namespace catalog record")
		}
		for fingerprint, value := range catalog.Keys {
			key, err := parseCatalogKey(value)
			if err != nil || key.Fingerprint != fingerprint {
				return empty, errors.New("invalid namespace SSH key record")
			}
		}
		for id, template := range catalog.Templates {
			if template.ID != id || template.NamespaceID != ns || !validStoredTemplate(template) {
				return empty, errors.New("invalid namespace template record")
			}
		}
	}
	return store, nil
}
func (r *NamespaceCatalogRepository) write(store namespaceCatalogStore) error {
	if err := checkAuthPath(r.dir, true); err != nil {
		return err
	}
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
	if err = replaceAuthFile(f.Name(), filepath.Join(r.dir, "catalogs.json")); err != nil {
		return err
	}
	return syncAuthDirectory(r.dir)
}
func (r *NamespaceCatalogRepository) update(ns compute.NamespaceID, fn func(*namespaceCatalog) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	store, err := r.read()
	if err != nil {
		return err
	}
	catalog, ok := store.Namespaces[ns]
	if !ok {
		catalog = namespaceCatalog{Keys: map[string]string{}, Templates: map[string]compute.CloudInitTemplate{}}
	}
	if err := fn(&catalog); err != nil {
		return err
	}
	store.Namespaces[ns] = catalog
	if err := r.commit(store); err != nil {
		r.poison = fmt.Errorf("namespace catalog persistence failed; reopen storage before recovery: %w", err)
		return r.poison
	}
	return nil
}
func (r *NamespaceCatalogRepository) Keys(ns compute.NamespaceID) (compute.KeyRepository, error) {
	if !compute.ValidCatalogID(string(ns)) {
		return nil, compute.ErrInvalid
	}
	return &namespaceKeys{r: r, ns: ns}, nil
}
func (r *NamespaceCatalogRepository) Templates(ns compute.NamespaceID) (compute.CloudInitTemplateRepository, error) {
	if !compute.ValidCatalogID(string(ns)) {
		return nil, compute.ErrInvalid
	}
	return &namespaceTemplates{r: r, ns: ns}, nil
}

type namespaceKeys struct {
	r  *NamespaceCatalogRepository
	ns compute.NamespaceID
}

func (v *namespaceKeys) List() ([]*compute.Key, error) {
	v.r.mu.Lock()
	defer v.r.mu.Unlock()
	store, err := v.r.read()
	if err != nil {
		return nil, err
	}
	keys := []*compute.Key{}
	for _, value := range store.Namespaces[v.ns].Keys {
		key, err := parseCatalogKey(value)
		if err != nil {
			return nil, err
		}
		key.NamespaceID = v.ns
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Fingerprint < keys[j].Fingerprint })
	return keys, nil
}
func (v *namespaceKeys) Get(fingerprint string) (*compute.Key, error) {
	keys, err := v.List()
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if key.Fingerprint == fingerprint {
			return key, nil
		}
	}
	return nil, compute.ErrKeyNotFound
}
func (v *namespaceKeys) Add(input string) error {
	key, err := parseCatalogKey(input)
	if err != nil {
		return err
	}
	return v.r.update(v.ns, func(c *namespaceCatalog) error {
		if _, ok := c.Keys[key.Fingerprint]; ok {
			return compute.ErrKeyAlreadyExists
		}
		c.Keys[key.Fingerprint] = strings.TrimSpace(key.ValueString())
		return nil
	})
}
func (v *namespaceKeys) Delete(fingerprint string) error {
	return v.r.update(v.ns, func(c *namespaceCatalog) error {
		if _, ok := c.Keys[fingerprint]; !ok {
			return compute.ErrKeyNotFound
		}
		delete(c.Keys, fingerprint)
		return nil
	})
}

type namespaceTemplates struct {
	r  *NamespaceCatalogRepository
	ns compute.NamespaceID
}

func (v *namespaceTemplates) List() ([]compute.CloudInitTemplate, error) {
	v.r.mu.Lock()
	defer v.r.mu.Unlock()
	store, err := v.r.read()
	if err != nil {
		return nil, err
	}
	result := []compute.CloudInitTemplate{}
	for _, template := range store.Namespaces[v.ns].Templates {
		result = append(result, template)
	}
	return result, nil
}
func (v *namespaceTemplates) Get(id string) (compute.CloudInitTemplate, error) {
	v.r.mu.Lock()
	defer v.r.mu.Unlock()
	store, err := v.r.read()
	if err != nil {
		return compute.CloudInitTemplate{}, err
	}
	t, ok := store.Namespaces[v.ns].Templates[id]
	if !ok {
		return compute.CloudInitTemplate{}, compute.ErrNotFound
	}
	return t, nil
}
func (v *namespaceTemplates) Create(t compute.CloudInitTemplate) error {
	if t.NamespaceID != "" && t.NamespaceID != v.ns {
		return compute.ErrInvalid
	}
	t.NamespaceID = v.ns
	if !validStoredTemplate(t) {
		return compute.ErrInvalid
	}
	return v.r.update(v.ns, func(c *namespaceCatalog) error {
		if _, ok := c.Templates[t.ID]; ok {
			return compute.ErrConflict
		}
		c.Templates[t.ID] = t
		return nil
	})
}
func (v *namespaceTemplates) Update(t compute.CloudInitTemplate, revision string) error {
	if t.NamespaceID != "" && t.NamespaceID != v.ns {
		return compute.ErrInvalid
	}
	t.NamespaceID = v.ns
	if !validStoredTemplate(t) {
		return compute.ErrInvalid
	}
	return v.r.update(v.ns, func(c *namespaceCatalog) error {
		prev, ok := c.Templates[t.ID]
		if !ok {
			return compute.ErrNotFound
		}
		if prev.Revision != revision {
			return compute.ErrConflict
		}
		c.Templates[t.ID] = t
		return nil
	})
}
func (v *namespaceTemplates) Delete(id, revision string) error {
	return v.r.update(v.ns, func(c *namespaceCatalog) error {
		prev, ok := c.Templates[id]
		if !ok {
			return compute.ErrNotFound
		}
		if prev.Revision != revision {
			return compute.ErrConflict
		}
		delete(c.Templates, id)
		return nil
	})
}

// Import explicitly copies selected legacy entries without deleting their
// source. Exact repeats are idempotent; conflicting content is never replaced.
func (r *NamespaceCatalogRepository) Import(ns compute.NamespaceID, keys []*compute.Key, templates []compute.CloudInitTemplate, apply bool) error {
	if !compute.ValidCatalogID(string(ns)) {
		return compute.ErrInvalid
	}
	validate := func(c *namespaceCatalog) error {
		for _, k := range keys {
			if k == nil || (k.NamespaceID != "" && k.NamespaceID != ns) {
				return compute.ErrInvalid
			}
			parsed, err := parseCatalogKey(k.ValueString())
			if err != nil || parsed.Fingerprint != k.Fingerprint {
				return compute.ErrInvalid
			}
			value := strings.TrimSpace(parsed.ValueString())
			if previous, ok := c.Keys[k.Fingerprint]; ok && previous != value {
				return compute.ErrConflict
			}
			c.Keys[k.Fingerprint] = value
		}
		for _, t := range templates {
			if t.NamespaceID != "" && t.NamespaceID != ns {
				return compute.ErrInvalid
			}
			t.NamespaceID = ns
			if !validStoredTemplate(t) {
				return compute.ErrInvalid
			}
			if previous, ok := c.Templates[t.ID]; ok && previous != t {
				return compute.ErrConflict
			}
			c.Templates[t.ID] = t
		}
		return nil
	}
	if apply {
		return r.update(ns, validate)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	store, err := r.read()
	if err != nil {
		return err
	}
	c, ok := store.Namespaces[ns]
	if !ok {
		c = namespaceCatalog{Keys: map[string]string{}, Templates: map[string]compute.CloudInitTemplate{}}
	}
	return validate(&c)
}
