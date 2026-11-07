//go:build linux || darwin

package bootstrap

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/installer"
	"subuk/vmango/libvirt"
)

func TestInstallResourcesReuseCanonicalConnectionsAndClose(t *testing.T) {
	doc, err := config.OpenInstallDocument(filepath.Join(t.TempDir(), "vmango.conf"))
	if err != nil {
		t.Fatal(err)
	}
	cache := newInstallResources(doc)
	a := installer.Host{ID: "a", Address: "host-a", User: "operator"}
	b := installer.Host{ID: "b", Address: "host-b", User: "operator"}
	first := cache.Get([]installer.Host{a, b})
	if second := cache.Get([]installer.Host{b, a}); first != second {
		t.Fatal("form reordering created a new libvirt pool")
	}
	a.PoolPath = "/different/resource"
	if second := cache.Get([]installer.Host{a, b}); first != second {
		t.Fatal("resource settings created a new connection pool")
	}
	a.Port = 2222
	if changed := cache.Get([]installer.Host{a, b}); first == changed {
		t.Fatal("connection change reused a stale pool")
	}
	if len(cache.entries) != 2 {
		t.Fatalf("unexpected pool count %d", len(cache.entries))
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = cache.Get([]installer.Host{a}).Prepare(context.Background(), a.ID, compute.HostPreparationSpec{})
	if !errors.Is(err, libvirt.ErrConnectionPoolClosed) {
		t.Fatalf("closed factory reopened: %v", err)
	}
}
