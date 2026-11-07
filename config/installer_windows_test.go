//go:build windows

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerRejectsUnsupportedWindowsController(t *testing.T) {
	root := t.TempDir()
	for _, existing := range []bool{false, true} {
		path := filepath.Join(root, "vmango.conf")
		if existing {
			if err := os.WriteFile(path, []byte("api {}\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		_, err := OpenInstallDocument(path)
		if err == nil || !strings.Contains(err.Error(), "supported on macOS and Linux only") {
			t.Fatalf("expected unsupported platform error (existing=%t), got %v", existing, err)
		}
		if !existing {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("unsupported installer created configuration: %v", err)
			}
		}
	}
}
