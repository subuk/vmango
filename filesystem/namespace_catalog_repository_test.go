package filesystem

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"subuk/vmango/compute"
	"sync"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

func catalogTestKey(t *testing.T) *compute.Key {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseCatalogKey(string(ssh.MarshalAuthorizedKey(key)))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
func TestNamespaceCatalogIsolationPersistenceAndMigration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "catalogs")
	r, err := NewNamespaceCatalogRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := catalogTestKey(t)
	template := compute.CloudInitTemplate{ID: uuid.NewString(), Revision: uuid.NewString(), Name: "Bootstrap", UserData: "#cloud-config\n{}"}
	if err := r.Import("alpha", []*compute.Key{key}, []compute.CloudInitTemplate{template}, false); err != nil {
		t.Fatal(err)
	}
	a, _ := r.Keys("alpha")
	if list, err := a.List(); err != nil || len(list) != 0 {
		t.Fatal("dry-run changed catalog", list, err)
	}
	if err := r.Import("alpha", []*compute.Key{key}, []compute.CloudInitTemplate{template}, true); err != nil {
		t.Fatal(err)
	}
	if err := r.Import("alpha", []*compute.Key{key}, []compute.CloudInitTemplate{template}, true); err != nil {
		t.Fatal("repeat import", err)
	}
	b, _ := r.Keys("beta")
	if _, err := b.Get(key.Fingerprint); !errors.Is(err, compute.ErrKeyNotFound) {
		t.Fatal("key leaked", err)
	}
	bt, _ := r.Templates("beta")
	if _, err := bt.Get(template.ID); !errors.Is(err, compute.ErrNotFound) {
		t.Fatal("guessed template ID leaked", err)
	}
	if err := b.Add(key.ValueString()); err != nil {
		t.Fatal("same fingerprint must be independent", err)
	}
	if err := a.Delete(key.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get(key.Fingerprint); err != nil {
		t.Fatal("deleting another namespace key removed ours", err)
	}
	if second, err := NewNamespaceCatalogRepository(dir); err == nil {
		second.Close()
		t.Fatal("competing writer acquired process lock")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r, err = NewNamespaceCatalogRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	at, _ := r.Templates("alpha")
	got, err := at.Get(template.ID)
	template.NamespaceID = "alpha"
	if err != nil || got != template {
		t.Fatal("template ID/revision/content changed on reopen", got, err)
	}
	changed := template
	changed.UserData = "#cloud-config\nusers: []"
	if err := r.Import("alpha", nil, []compute.CloudInitTemplate{changed}, true); !errors.Is(err, compute.ErrConflict) {
		t.Fatal("conflicting migration overwrote template", err)
	}
	if err := r.Import("", nil, nil, true); !errors.Is(err, compute.ErrInvalid) {
		t.Fatal("empty namespace accepted", err)
	}
}
func TestNamespaceCatalogRevisionConcurrencyAndCorruption(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "catalogs")
	r, err := NewNamespaceCatalogRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	v, _ := r.Templates("alpha")
	template := compute.CloudInitTemplate{ID: uuid.NewString(), Revision: uuid.NewString(), Name: "Bootstrap", UserData: "#cloud-config\n{}"}
	if err := v.Create(template); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			next := template
			next.Revision = uuid.NewString()
			results <- v.Update(next, template.Revision)
		})
	}
	wg.Wait()
	close(results)
	var success, conflict int
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, compute.ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("CAS failed", success, conflict)
	}
	if err := v.Delete(template.ID, template.Revision); !errors.Is(err, compute.ErrConflict) {
		t.Fatal("stale revision deletion", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "catalogs.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	keys, _ := r.Keys("missing")
	if _, err := keys.List(); err == nil {
		t.Fatal("corruption became an empty catalog")
	}
	if err := keys.Add(catalogTestKey(t).ValueString()); err == nil {
		t.Fatal("mutation overwrote corrupt metadata")
	}
}

func TestNamespaceCatalogFailedCommitRequiresRecovery(t *testing.T) {
	for _, afterReplacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-replacement", true: "after-replacement"}[afterReplacement], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "catalogs")
			r, err := NewNamespaceCatalogRepository(dir)
			if err != nil {
				t.Fatal(err)
			}
			keys, _ := r.Keys("alpha")
			first := catalogTestKey(t)
			if err := keys.Add(first.ValueString()); err != nil {
				t.Fatal(err)
			}
			r.commit = func(store namespaceCatalogStore) error {
				if afterReplacement {
					if err := r.write(store); err != nil {
						return err
					}
				}
				return errors.New("injected durable commit failure")
			}
			second := catalogTestKey(t)
			if err := keys.Add(second.ValueString()); err == nil {
				t.Fatal("failed commit reported success")
			}
			if _, err := keys.List(); err == nil {
				t.Fatal("uncertain store remained usable")
			}
			if err := keys.Delete(first.Fingerprint); err == nil {
				t.Fatal("write proceeded after uncertain commit")
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			r, err = NewNamespaceCatalogRepository(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			keys, _ = r.Keys("alpha")
			got, err := keys.List()
			want := 1
			if afterReplacement {
				want = 2
			}
			if err != nil || len(got) != want {
				t.Fatal("recovery did not read complete old/new store", got, err)
			}
			if _, err := keys.Get(first.Fingerprint); err != nil {
				t.Fatal("previous committed key lost", err)
			}
		})
	}
}
