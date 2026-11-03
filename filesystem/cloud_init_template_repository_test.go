package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"subuk/vmango/compute"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestCloudInitTemplateRepositoryPersistenceAndCAS(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "templates")
	repo, err := NewCloudInitTemplateRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := compute.NewCloudInitTemplateService(repo)
	created, err := service.Create(compute.CloudInitTemplateInput{Name: "Bootstrap", UserData: "#cloud-config\nwrite_files: []\n"})
	if err != nil {
		t.Fatal(err)
	}
	if second, err := NewCloudInitTemplateRepository(dir); err == nil {
		second.Close()
		t.Fatal("second owner acquired the store")
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	repo, err = NewCloudInitTemplateRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	loaded, err := repo.Get(created.ID)
	if err != nil || loaded != created {
		t.Fatal("template did not survive reopening", err)
	}
	service = compute.NewCloudInitTemplateService(repo)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			_, err := service.Update(created.ID, created.Revision, compute.CloudInitTemplateInput{Name: "Changed", UserData: "#!/bin/sh\necho ready\n"})
			results <- err
		})
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
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
		t.Fatalf("concurrent updates: success=%d conflict=%d", success, conflict)
	}
	if err := repo.Delete(created.ID, created.Revision); !errors.Is(err, compute.ErrConflict) {
		t.Fatal("stale deletion succeeded", err)
	}
	latest, err := repo.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(latest.ID, latest.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(created.ID); !errors.Is(err, compute.ErrNotFound) {
		t.Fatal("deleted record remains", err)
	}
}

func TestCloudInitTemplateRepositoryPrivateStorage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix mode and symlink checks")
	}
	dir := filepath.Join(t.TempDir(), "templates")
	repo, err := NewCloudInitTemplateRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := compute.NewCloudInitTemplateService(repo)
	created, err := service.Create(compute.CloudInitTemplateInput{Name: "Bootstrap", UserData: "#cloud-config\npassword: private-value\n"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, created.ID+".json")
	for _, tc := range []struct {
		path string
		mode os.FileMode
	}{{dir, 0700}, {path, 0600}} {
		info, err := os.Stat(tc.path)
		if err != nil || info.Mode().Perm() != tc.mode {
			t.Fatal("unsafe permissions", tc.path, err)
		}
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(created.ID); err == nil {
		t.Fatal("publicly readable template accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("private-outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(created.ID); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := repo.Update(created, created.Revision); err == nil {
		t.Fatal("symlink mutation accepted")
	}
	if _, err := repo.Get("../outside"); !errors.Is(err, compute.ErrInvalid) {
		t.Fatal("path traversal accepted", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "private-outside" {
		t.Fatal("outside file changed", err)
	}
}

func TestCloudInitTemplateRepositoryRejectsCorruptionWithoutSecrets(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "templates")
	repo, err := NewCloudInitTemplateRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	path := filepath.Join(dir, id+".json")
	file, err := openAuthFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"private-bootstrap-secret":"not a record"}`); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := checkAuthPath(path, false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(id); err == nil || strings.Contains(err.Error(), "private-bootstrap-secret") {
		t.Fatal("corruption leaked or was accepted", err)
	}
	repo.Close()
	if reopened, err := NewCloudInitTemplateRepository(dir); err == nil {
		reopened.Close()
		t.Fatal("corrupt catalog opened")
	}
}

func TestCloudInitTemplateRepositoryDiscardsInterruptedWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "templates")
	if err := makeAuthDirectory(dir); err != nil {
		t.Fatal(err)
	}
	pending, err := createAuthTemp(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := pending.Name()
	if _, err := pending.WriteString("private unfinished content"); err != nil {
		pending.Close()
		t.Fatal(err)
	}
	if err := pending.Close(); err != nil {
		t.Fatal(err)
	}
	repo, err := NewCloudInitTemplateRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	entries, err := repo.List()
	if err != nil || len(entries) != 0 {
		t.Fatal("uncommitted data promoted", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interrupted write retained", err)
	}
}
