package richt

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageObservationDoesNotCreateDirectories(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing", "pool")
	out, err := execute(context.Background(), "python3", []string{"-", missing}, []byte(storageProbe), nil, nil)
	if err != nil {
		t.Fatalf("probe: %s %v", out, err)
	}
	var result struct {
		FreeBytes uint64 `json:"free_bytes"`
	}
	if err = json.Unmarshal(out, &result); err != nil || result.FreeBytes == 0 {
		t.Fatalf("storage observation: %s %v", out, err)
	}
	if _, err = os.Stat(filepath.Join(dir, "missing")); !os.IsNotExist(err) {
		t.Fatal("read-only inspection created a directory")
	}
}

func TestStorageRejectsSymlinkAndFileAncestors(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(dir, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "file"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, item := range []string{"link", "link/child", "file", "file/child"} {
		out, err := execute(context.Background(), "python3", []string{"-", filepath.Join(dir, item)}, []byte(storageProbe), nil, nil)
		if err == nil || !strings.Contains(string(out), `"error"`) {
			t.Fatalf("unsafe path %s accepted: %s %v", item, out, err)
		}
	}
}
