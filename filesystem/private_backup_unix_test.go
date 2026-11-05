//go:build !windows

package filesystem

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrivateBackupRejectsUnsafePermissions(t *testing.T) {
	for _, directory := range []bool{true, false} {
		t.Run(map[bool]string{true: "directory", false: "file"}[directory], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "backup")
			if err := CreatePrivateBackupDirectory(dir); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "manifest.json")
			if err := WritePrivateBackupFile(path, []byte("original")); err != nil {
				t.Fatal(err)
			}
			unsafePath, mode := path, os.FileMode(0644)
			if directory {
				unsafePath, mode = dir, 0755
			}
			if err := os.Chmod(unsafePath, mode); err != nil {
				t.Fatal(err)
			}
			if err := CheckPrivateBackupPath(unsafePath, directory); err == nil {
				t.Fatal("unsafe backup accepted")
			}
			if err := WritePrivateBackupFile(path, []byte("replacement")); err == nil {
				t.Fatal("unsafe backup overwritten")
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "original" {
				t.Fatal("rejected write changed contents", err)
			}
		})
	}
}
