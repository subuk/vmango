package filesystem

import (
	"path/filepath"
	"subuk/vmango/compute"
	"testing"
)

func TestVolumeOwnershipSurvivesRestartAndRevocation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ownership")
	repo, err := NewVolumeOwnershipRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if repo != nil {
			repo.Close()
		}
	})
	if second, err := NewVolumeOwnershipRepository(dir); err == nil {
		second.Close()
		t.Fatal("another writer acquired the ownership store")
	}
	key := "host\x00/pool/disk"
	owned := compute.VolumeOwnership{Cloud: "local", StorageClass: "standard", Node: "host", Path: "/pool/disk", Pool: "pool", Identity: "backend-volume"}
	if err = repo.Put(key, owned); err != nil {
		t.Fatal(err)
	}
	reopen := func() {
		t.Helper()
		if err := repo.Close(); err != nil {
			t.Fatal(err)
		}
		repo, err = NewVolumeOwnershipRepository(dir)
		if err != nil {
			t.Fatal(err)
		}
	}
	reopen()
	if got, err := repo.Get(key); err != nil || got != owned {
		t.Fatalf("ownership after restart: %+v, %v", got, err)
	}
	if err = repo.Put(key, compute.VolumeOwnership{}); err != nil {
		t.Fatal(err)
	}
	reopen()
	if got, err := repo.Get(key); err != nil || got != (compute.VolumeOwnership{}) {
		t.Fatalf("revoked ownership returned after restart: %+v, %v", got, err)
	}
}
