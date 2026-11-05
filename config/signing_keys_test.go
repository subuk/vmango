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

func TestSigningKeyFile(t *testing.T) {
	dir := t.TempDir()
	c := APIConfig{JWTSigningKeyFile: filepath.Join(dir, "private", "jwt")}
	if err := c.LoadSigningKey(); err != nil {
		t.Fatal(err)
	}
	key := c.JWTSigningKey
	if err := c.LoadSigningKey(); err != nil {
		t.Fatal(err)
	}
	if key != c.JWTSigningKey {
		t.Fatal("key changed")
	}
	assertPrivateSigningKey(t, c.JWTSigningKeyFile)

	for _, data := range []string{"", "short", strings.Repeat("abcdef01", 8), "-----BEGIN PRIVATE KEY-----\ncorrupt\n-----END PRIVATE KEY-----"} {
		if err := os.WriteFile(c.JWTSigningKeyFile, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := c.LoadSigningKey(); err != nil {
			t.Fatal(err)
		}
		assertPrivateSigningKey(t, c.JWTSigningKeyFile)
		if c.JWTSigningKey == key {
			t.Fatal("invalid file did not receive a new key")
		}
		key = c.JWTSigningKey
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
