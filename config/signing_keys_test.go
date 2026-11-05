package config

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"subuk/vmango/auth"
	"sync"
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
	for _, path := range []string{c.JWTSigningKeyFile, c.ConsoleSigningKeyFile} {
		assertPrivateSigningKey(t, path)
	}

	for _, data := range []string{"", "short", strings.Repeat("abcdef01", 8), "-----BEGIN PRIVATE KEY-----\ncorrupt\n-----END PRIVATE KEY-----"} {
		for _, path := range []string{c.JWTSigningKeyFile, c.ConsoleSigningKeyFile} {
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.LoadSigningKeys(); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{c.JWTSigningKeyFile, c.ConsoleSigningKeyFile} {
			assertPrivateSigningKey(t, path)
		}
		if c.JWTSigningKey == jwt || c.ConsoleSigningKey == console {
			t.Fatal("invalid files did not receive new keys")
		}
		jwt, console = c.JWTSigningKey, c.ConsoleSigningKey
	}
}

func assertPrivateSigningKey(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.ParseSigningKey(string(data)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Windows does not expose Unix permission bits through os.Stat.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}

func TestSigningKeyPreservesUnsupportedPEM(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	for i, block := range []*pem.Block{
		{Type: "PRIVATE KEY", Bytes: der},
		{Type: "PRIVATE KEY", Bytes: []byte("invalid DER")},
		{Type: "PUBLIC KEY", Bytes: []byte("public")},
		{Type: "ENCRYPTED PRIVATE KEY", Bytes: []byte("encrypted")},
	} {
		t.Run(fmt.Sprintf("%d-%s", i, block.Type), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "key")
			data := pem.EncodeToMemory(block)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadSigningKey(path); err == nil {
				t.Fatal("unsupported PEM accepted")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(data) {
				t.Fatal("unsupported PEM was replaced", err)
			}
		})
	}
}

func TestSigningKeyReadErrorDoesNotGenerate(t *testing.T) {
	path := t.TempDir()
	if _, err := loadSigningKey(path); err == nil {
		t.Fatal("directory accepted as a key file")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatal("directory replaced", err)
	}
}

func TestSigningKeyRejectsSameMaterial(t *testing.T) {
	dir := t.TempDir()
	c := APIConfig{JWTSigningKeyFile: filepath.Join(dir, "jwt"), ConsoleSigningKeyFile: filepath.Join(dir, "console")}
	if err := c.LoadSigningKeys(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.ConsoleSigningKeyFile, []byte("\n"+c.JWTSigningKey+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c.LoadSigningKeys(); err == nil {
		t.Fatal("same key accepted with different whitespace")
	}
}

func TestSigningKeyConcurrentGeneration(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "missing"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "key")
			if legacy {
				if err := os.WriteFile(path, []byte(strings.Repeat("abcdef01", 8)), 0644); err != nil {
					t.Fatal(err)
				}
			}
			var wg sync.WaitGroup
			values := make([]string, 12)
			for i := range values {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var err error
					values[i], err = loadSigningKey(path)
					if err != nil {
						t.Error(err)
					}
				}()
			}
			wg.Wait()
			for _, value := range values {
				if value != values[0] {
					t.Fatal("concurrent starters loaded different keys")
				}
			}
			assertPrivateSigningKey(t, path)
		})
	}
}
