package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Publish validates the exact prepared bytes and verifies the original file
// immediately before atomic replacement. A cooperating installer lock prevents
// concurrent installers; arbitrary external writers must still be stopped.
func (d *InstallDocument) Publish(c *InstallCandidate) (string, error) {
	if c == nil || c.document != d || c.Digest != installDigest(c.data) {
		return "", errors.New("candidate does not belong to this configuration or has changed")
	}
	if _, err := ParseBytes(c.data); err != nil {
		return "", fmt.Errorf("candidate validation: %w", err)
	}
	if err := d.checkOriginal(); err != nil {
		return "", err
	}
	if bytes.Equal(d.original, c.data) {
		return "", nil
	}
	parent := filepath.Dir(d.path)
	tmp, err := os.CreateTemp(parent, ".vmango-config-*")
	if err != nil {
		return "", fmt.Errorf("cannot stage configuration: %w", err)
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if _, err = tmp.Write(c.data); err != nil {
		return "", err
	}
	if d.exists {
		err = preserveInstallMetadata(d.path, tmp, d.info)
	} else {
		err = tmp.Chmod(0600)
	}
	if err != nil {
		return "", fmt.Errorf("cannot preserve configuration metadata: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	backup := ""
	if d.exists {
		// A content-addressed backup name is stable across interrupted retries.
		backup = d.path + ".backup-" + installDigest(d.original)
		if err = installBackup(backup, d.original); err != nil {
			return "", err
		}
		if err = syncInstallDirectory(parent); err != nil {
			return backup, err
		}
	}
	if err = d.checkOriginal(); err != nil {
		return backup, err
	}
	if err = os.Rename(tmp.Name(), d.path); err != nil {
		return backup, err
	}
	// Publication has happened even if subsequent durability confirmation fails.
	d.original = append([]byte(nil), c.data...)
	d.exists = true
	d.cfg = c.cfg
	d.info, _ = os.Lstat(d.path)
	if err = syncInstallDirectory(parent); err != nil {
		return backup, fmt.Errorf("configuration was published but directory sync failed: %w", err)
	}
	return backup, nil
}

func (d *InstallDocument) checkOriginal() error {
	parent, err := filepath.EvalSymlinks(filepath.Dir(d.requested))
	if err != nil {
		return err
	}
	if filepath.Join(parent, filepath.Base(d.requested)) != d.path {
		return errors.New("configuration parent link changed after preparation")
	}
	data, info, err := readInstallFile(d.path)
	if !d.exists {
		if errors.Is(err, os.ErrNotExist) {
			return checkInstallWritable(d.path, false)
		}
		if err != nil {
			return err
		}
		return errors.New("configuration appeared after preparation; reload and review")
	}
	if err != nil {
		return fmt.Errorf("configuration changed after preparation: %w", err)
	}
	if !bytes.Equal(data, d.original) {
		return errors.New("configuration contents changed after preparation; reload and review")
	}
	if info.Mode() != d.info.Mode() || !sameInstallOwner(info, d.info) {
		return errors.New("configuration permissions or ownership changed after preparation; reload and review")
	}
	return checkInstallWritable(d.path, true)
}

func installBackup(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		existing, info, e := readInstallFile(path)
		if e != nil || !bytes.Equal(existing, data) || info.Mode().Perm()&0077 != 0 {
			return errors.New("configuration backup conflicts with an existing file")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot create configuration backup: %w", err)
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(path)
		}
	}()
	if _, err = f.Write(data); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}
func syncInstallDirectory(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
