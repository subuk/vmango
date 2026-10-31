package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDefaultsAndOverrides(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "vmango.conf")
	content := []byte(`
log_level = "debug"
` + cloudConfig + `
api {
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
	if cfg.LogLevel != "debug" || cfg.API.Listen != "127.0.0.1:9000" || !cfg.API.SessionSecure {
		t.Fatalf("explicit settings were lost: %+v", cfg)
	}
	if cfg.KeyFile != Default().KeyFile || cfg.API.SessionDir != Default().API.SessionDir {
		t.Fatalf("defaults were not merged: %+v", cfg)
	}
	if len(cfg.API.Oidc.Scopes) != 2 || cfg.API.Oidc.Scopes[1] != "email" {
		t.Fatalf("explicit OIDC scopes were changed: %v", cfg.API.Oidc.Scopes)
	}
	if len(cfg.Hosts()) != 2 || cfg.Hosts()[0].ConfigDriveWriteFormat != "nocloud" || cfg.Hosts()[0].ConfigDrivePool != "default" {
		t.Fatalf("host backend defaults were not applied: %+v", cfg.Hosts())
	}
}

func TestQuickStartConfig(t *testing.T) {
	content, err := os.ReadFile("../vmango.quickstart.conf")
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "quickstart.conf")
	content = []byte(strings.ReplaceAll(string(content), "REPLACE_WITH_RANDOM_SECRET", "e702acd09d76676cd386642a315038470691f711b3821e1552bfffd37cb15b983"))
	content = []byte(strings.ReplaceAll(string(content), "REPLACE_WITH_SEPARATE_RANDOM_SECRET", "938f251d85b54115ae5466f999682d803c8ad5926ba4b11ee7cd123a69cf790ed"))
	if err = os.WriteFile(filename, content, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(filename)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.API.Listen != "127.0.0.1:8080" {
		t.Fatalf("quick start must use loopback: %+v", cfg.API)
	}
	if len(cfg.Hosts()) != 1 || cfg.Hosts()[0].Uri != "qemu:///system" {
		t.Fatalf("quick start needs a cloud host connection: %+v", cfg.Hosts())
	}
	if len(cfg.API.Users) != 1 || cfg.API.Users[0].Id != "admin" {
		t.Fatalf("quick start needs an explicit admin user: %+v", cfg.API.Users)
	}
}

func TestAPIConfiguration(t *testing.T) {
	for _, tc := range []struct {
		body     string
		valid    bool
		disabled bool
		ttl      int
	}{
		{`api {}`, true, false, 3600},
		{`api { api_disabled = true console_link_lifetime_seconds = 90 }`, false, true, 90},
		{`api { console_link_lifetime_seconds = -1 }`, false, false, 0},
		{`api { console_link_lifetime_seconds = 2592001 }`, false, false, 0},
		{`api { console_signing_key = "short" }`, false, false, 0},
	} {
		file := filepath.Join(t.TempDir(), "vmango.conf")
		if err := os.WriteFile(file, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Parse(file)
		if (err == nil) != tc.valid {
			t.Fatalf("%s: %v", tc.body, err)
		}
		if err == nil && (cfg.API.APIDisabled != tc.disabled || cfg.API.ConsoleLinkLifetimeSeconds != tc.ttl) {
			t.Fatalf("API defaults/overrides lost")
		}
	}
}

func TestLegacyWebConfigurationFails(t *testing.T) {
	f := filepath.Join(t.TempDir(), "legacy.conf")
	if err := os.WriteFile(f, []byte(`web { api_disabled=true }`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(f); err == nil || !strings.Contains(err.Error(), "migrate web") {
		t.Fatal("missing migration instruction", err)
	}
}

func TestNativeTLSConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, body, wantError string
	}{
		{"HTTP defaults", ``, ""},
		{"proxy HTTPS", `base_url = "https://vmango.test" session_secure = true`, ""},
		{"certificate only", `tls_cert_file = "cert.pem"`, "configured together"},
		{"key only", `tls_key_file = "key.pem"`, "configured together"},
		{"native TLS", `tls_cert_file = "cert.pem" tls_key_file = "key.pem"`, ""},
		{"native public URL", `tls_cert_file = "cert.pem" tls_key_file = "key.pem" base_url = "https://vmango.test:8443"`, ""},
		{"HTTP public URL", `tls_cert_file = "cert.pem" tls_key_file = "key.pem" base_url = "http://vmango.test"`, "absolute HTTPS URL"},
		{"relative public URL", `tls_cert_file = "cert.pem" tls_key_file = "key.pem" base_url = "/vmango"`, "absolute HTTPS URL"},
		{"missing public host", `tls_cert_file = "cert.pem" tls_key_file = "key.pem" base_url = "https:///"`, "absolute HTTPS URL"},
		{"malformed public URL", `tls_cert_file = "cert.pem" tls_key_file = "key.pem" base_url = "https://["`, "absolute HTTPS URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "vmango.conf")
			if err := os.WriteFile(file, []byte("api { "+tc.body+" }"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := Parse(file)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("expected %q, got %v", tc.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.API.Listen != Default().API.Listen || strings.Contains(tc.body, "tls_cert_file") && (cfg.API.TLSCertFile != "cert.pem" || cfg.API.TLSKeyFile != "key.pem") {
				t.Fatalf("TLS settings/defaults were lost: %+v", cfg.API)
			}
		})
	}
}
