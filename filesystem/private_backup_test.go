package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateBackupCreateReplaceAndValidate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backup")
	if err := CreatePrivateBackupDirectory(dir); err != nil {
		t.Fatal(err)
	}
	if err := CheckPrivateBackupPath(dir, true); err != nil {
		t.Fatal("new backup directory is unsafe", err)
	}
	if err := CreatePrivateBackupDirectory(dir); !errors.Is(err, os.ErrExist) {
		t.Fatal("existing directory must not be reused", err)
	}
	path := filepath.Join(dir, "manifest.json")
	for _, contents := range []string{"initial", "completed"} {
		if err := WritePrivateBackupFile(path, []byte(contents)); err != nil {
			t.Fatal(err)
		}
		if err := CheckPrivateBackupPath(path, false); err != nil {
			t.Fatal("committed backup file is unsafe", err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != contents {
			t.Fatalf("backup contents: %q, %v", data, err)
		}
	}
	if err := CheckPrivateBackupPath(path, true); err == nil {
		t.Fatal("file accepted as backup directory")
	}
	if err := WritePrivateBackupFile(dir, []byte("replacement")); err == nil {
		t.Fatal("directory accepted as backup file")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "manifest.json" {
		t.Fatal("temporary backup files remain", entries, err)
	}
}
