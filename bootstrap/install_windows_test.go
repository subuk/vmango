//go:build windows

package bootstrap

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRejectsWindowsBeforeOpeningConfigurationOrRicht(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	err := Install(filepath.Join(root, "missing-parent", "vmango.conf"), filepath.Join(root, "missing-richt.exe"), filepath.Join(root, "state"), "invalid-listener", 0, &output)
	if err == nil || !strings.Contains(err.Error(), "supports Linux and macOS controllers") {
		t.Fatalf("expected unsupported platform error before other validation, got %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("unsupported installer printed a session")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("unsupported installer created local state")
	}
}
