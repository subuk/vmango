package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDefaultsAndOverrides(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "vmango.conf")
	content := []byte(`
log_level = "debug"
libvirt "local" { uri = "qemu:///system" }
web {
    listen = "127.0.0.1:9000"
    session_secure = true
    oidc { scopes = ["openid", "email"] }
}
`)
	if err := os.WriteFile(filename, content, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(filename)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != "debug" || cfg.Web.Listen != "127.0.0.1:9000" || !cfg.Web.SessionSecure {
		t.Fatalf("explicit settings were lost: %+v", cfg)
	}
	if cfg.KeyFile != Default().KeyFile || cfg.Web.SessionMaxAge != Default().Web.SessionMaxAge {
		t.Fatalf("defaults were not merged: %+v", cfg)
	}
	if len(cfg.Web.Oidc.Scopes) != 2 || cfg.Web.Oidc.Scopes[1] != "email" {
		t.Fatalf("explicit OIDC scopes were changed: %v", cfg.Web.Oidc.Scopes)
	}
	if len(cfg.Libvirts) != 1 || cfg.Libvirts[0].ConfigDriveWriteFormat != "nocloud" || cfg.Libvirts[0].ConfigDrivePool != "default" {
		t.Fatalf("libvirt defaults were not applied: %+v", cfg.Libvirts)
	}
}
