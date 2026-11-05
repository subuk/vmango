package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"subuk/vmango/auth"
	"subuk/vmango/util"
)

// LoadSigningKeys loads persistent Ed25519 keys, replacing missing or non-PEM files.
func (c *APIConfig) LoadSigningKeys() error {
	for _, key := range []struct {
		path  string
		value *string
	}{{c.JWTSigningKeyFile, &c.JWTSigningKey}, {c.ConsoleSigningKeyFile, &c.ConsoleSigningKey}} {
		path := util.ExpandHomeDir(key.path)
		value, err := loadSigningKey(path)
		if err != nil {
			return fmt.Errorf("signing key file %s: %w", path, err)
		}
		*key.value = value
	}
	return auth.ValidateSigningKeys(c.JWTSigningKey, c.ConsoleSigningKey)
}

func readSigningKey(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", true, nil
	}
	if err != nil {
		return "", false, err
	}
	if block, _ := pem.Decode(data); block == nil {
		return "", true, nil
	}
	// A recognized PEM with an unsupported key is a configuration error.
	// Only missing files and data that cannot be decoded as PEM are replaced.
	value := string(data)
	return value, false, auth.ValidateKey(value)
}

func loadSigningKey(path string) (string, error) {
	value, generate, err := readSigningKey(path)
	if err != nil || !generate {
		return value, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	// Keep a stable lock inode across atomic replacements so concurrent starters
	// cannot generate different keys from the same legacy file.
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer lock.Close()
	if err := lockSigningKey(lock); err != nil {
		return "", err
	}
	value, generate, err = readSigningKey(path)
	if err != nil || !generate {
		return value, err
	}
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	f, err := os.CreateTemp(filepath.Dir(path), ".signing-key-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer os.Remove(name)
	_, writeErr := f.Write(data)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil {
		return "", writeErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := os.Rename(name, path); err != nil {
		return "", err
	}
	return string(data), nil
}
