package compute_test

import (
	"errors"
	"path/filepath"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"testing"
)

func TestNamespaceCatalogServicesRequireConfiguredContext(t *testing.T) {
	repo, err := filesystem.NewNamespaceCatalogRepository(filepath.Join(t.TempDir(), "catalogs"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	registry, err := compute.NewNamespaceService([]compute.Namespace{{ID: "alpha", Clouds: []string{"cloud"}}, {ID: "beta", Clouds: []string{"cloud"}}})
	if err != nil {
		t.Fatal(err)
	}
	keys := compute.NewNamespaceKeyService(repo, registry)
	templates := compute.NewNamespaceCloudInitTemplateService(repo, registry)
	if _, err := keys.List(); err == nil {
		t.Fatal("unscoped key access succeeded")
	}
	if _, err := templates.List(); err == nil {
		t.Fatal("unscoped template access succeeded")
	}
	if _, err := keys.ForNamespace(""); err == nil {
		t.Fatal("empty namespace succeeded")
	}
	if _, err := templates.ForNamespace("removed"); err == nil {
		t.Fatal("unconfigured namespace succeeded")
	}
	a, _ := templates.ForNamespace("alpha")
	b, _ := templates.ForNamespace("beta")
	created, err := a.Create(compute.CloudInitTemplateInput{Name: "Bootstrap", UserData: "#cloud-config\n{}"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Resolve(compute.CloudInitTemplateRef{ID: created.ID, Revision: created.Revision}); !errors.Is(err, compute.ErrNotFound) {
		t.Fatal("cross namespace template resolved", err)
	}
	changed, err := a.Update(created.ID, created.Revision, compute.CloudInitTemplateInput{Name: "New", UserData: "#cloud-config\nusers: []"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Resolve(compute.CloudInitTemplateRef{ID: created.ID, Revision: created.Revision}); !errors.Is(err, compute.ErrConflict) {
		t.Fatal("stale template revision resolved", err)
	}
	if body, err := a.Resolve(compute.CloudInitTemplateRef{ID: changed.ID, Revision: changed.Revision}); err != nil || body != changed.UserData {
		t.Fatal(body, err)
	}
	removed, _ := compute.NewNamespaceService(nil)
	if _, err := compute.NewNamespaceCloudInitTemplateService(repo, removed).ForNamespace("alpha"); err == nil {
		t.Fatal("removed namespace selectable")
	}
	readded := compute.NewNamespaceCloudInitTemplateService(repo, registry)
	restored, _ := readded.ForNamespace("alpha")
	if got, err := restored.Get(created.ID); err != nil || got != changed {
		t.Fatal("removing namespace erased catalog", got, err)
	}
}
