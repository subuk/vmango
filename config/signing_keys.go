package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"subuk/vmango/auth"
	"subuk/vmango/util"
)

// LoadSigningKeys loads persistent secrets, creating missing files privately.
func (c *APIConfig) LoadSigningKeys() error {
	for _, key := range []struct {
		path  string
		value *string
	}{{c.JWTSigningKeyFile, &c.JWTSigningKey}, {c.ConsoleSigningKeyFile, &c.ConsoleSigningKey}} {
		path := util.ExpandHomeDir(key.path)
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				return err
			}
			secret := make([]byte, 32)
			if _, err := rand.Read(secret); err != nil {
				return err
			}
			data = []byte(hex.EncodeToString(secret))
			// Publish a complete file atomically without replacing another starter's key.
			f, err := os.CreateTemp(filepath.Dir(path), ".signing-key-*")
			if err != nil {
				return err
			}
			name := f.Name()
			defer os.Remove(name)
			_, writeErr := f.Write(data)
			closeErr := f.Close()
			if writeErr != nil {
				return writeErr
			}
			if closeErr != nil {
				return closeErr
			}
			if err := os.Link(name, path); err != nil && !os.IsExist(err) {
				return err
			}
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		} else if err != nil {
			return fmt.Errorf("read signing key %s: %w", path, err)
		}
		value := strings.TrimSpace(string(data))
		if err := auth.ValidateKey(value); err != nil {
			return fmt.Errorf("signing key file %s: %w", path, err)
		}
		*key.value = value
	}
	if c.JWTSigningKey == c.ConsoleSigningKey {
		return fmt.Errorf("signing key files must contain distinct secrets")
	}
	return nil
}
