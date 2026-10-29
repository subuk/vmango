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

func TestQuickStartConfig(t *testing.T) {
	cfg, err := Parse("../vmango.quickstart.conf")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Web.Listen != "127.0.0.1:8080" || cfg.Web.MediaUploadTmp != "." {
		t.Fatalf("quick start must use loopback and a portable upload directory: %+v", cfg.Web)
	}
	if len(cfg.Libvirts) != 1 || cfg.Libvirts[0].Uri != "qemu:///system" {
		t.Fatalf("quick start needs an explicit libvirt node: %+v", cfg.Libvirts)
	}
	if len(cfg.Web.Users) != 1 || cfg.Web.Users[0].Id != "admin" {
		t.Fatalf("quick start needs an explicit admin user: %+v", cfg.Web.Users)
	}
}

func TestAPIConfiguration(t *testing.T) {
	for _, tc := range []struct {
		body     string
		valid    bool
		disabled bool
		ttl      int
	}{
		{`web {}`, true, false, 3600},
		{`web { api_disabled = true console_link_lifetime_seconds = 90 }`, true, true, 90},
		{`web { console_link_lifetime_seconds = -1 }`, false, false, 0},
		{`web { console_link_lifetime_seconds = 2592001 }`, false, false, 0},
		{`web { console_signing_key = "short" }`, false, false, 0},
	} {
		file := filepath.Join(t.TempDir(), "vmango.conf")
		if err := os.WriteFile(file, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Parse(file)
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.body, err)
		}
		if err == nil && (cfg.Web.APIDisabled != tc.disabled || cfg.Web.ConsoleLinkLifetimeSeconds != tc.ttl) {
			t.Fatalf("API defaults/overrides lost")
		}
	}
}
