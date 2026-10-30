package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSigningKeyFiles(t *testing.T) {
	dir := t.TempDir()
	c := APIConfig{JWTSigningKeyFile: filepath.Join(dir, "private", "jwt"), ConsoleSigningKeyFile: filepath.Join(dir, "private", "console")}
	if err := c.LoadSigningKeys(); err != nil {
		t.Fatal(err)
	}
	jwt, console := c.JWTSigningKey, c.ConsoleSigningKey
	if jwt == console {
		t.Fatal("keys must differ")
	}
	if err := c.LoadSigningKeys(); err != nil {
		t.Fatal(err)
	}
	if jwt != c.JWTSigningKey || console != c.ConsoleSigningKey {
		t.Fatal("keys changed")
	}
	info, err := os.Stat(c.JWTSigningKeyFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
	if err := os.WriteFile(c.JWTSigningKeyFile, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.LoadSigningKeys(); err == nil {
		t.Fatal("invalid existing key accepted")
	}
	data, _ := os.ReadFile(c.JWTSigningKeyFile)
	if string(data) != "short" {
		t.Fatal("existing key overwritten")
	}
}
