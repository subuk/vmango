package filesystem

import (
	"errors"
	"os"
	"path/filepath"
)

// CreatePrivateBackupDirectory creates a new directory restricted to the
// current account. Existing directories are never repaired or reused here.
func CreatePrivateBackupDirectory(path string) error {
	return createAuthDirectory(path)
}

// CheckPrivateBackupPath validates the object type, owner and private access
// using Unix permissions or Windows ACLs, without changing existing access.
func CheckPrivateBackupPath(path string, directory bool) error {
	return checkAuthPath(path, directory)
}

// SyncPrivateBackupDirectory commits directory changes where supported.
// Windows file replacements use a write-through move instead.
func SyncPrivateBackupDirectory(path string) error {
	return syncAuthDirectory(path)
}

// WritePrivateBackupFile atomically replaces a private regular file. Both the
// directory and any existing destination must already have private access.
func WritePrivateBackupFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := checkAuthPath(dir, true); err != nil {
		return err
	}
	if err := checkAuthPath(path, false); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	f, err := createAuthTemp(dir)
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = replaceAuthFile(f.Name(), path); err != nil {
		return err
	}
	return syncAuthDirectory(dir)
}
