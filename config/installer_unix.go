//go:build !windows

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func checkInstallWritable(path string, exists bool) error {
	if err := unix.Access(filepath.Dir(path), unix.W_OK|unix.X_OK); err != nil {
		return fmt.Errorf("configuration directory is not writable: %w", err)
	}
	if exists {
		if err := unix.Access(path, unix.R_OK|unix.W_OK); err != nil {
			return fmt.Errorf("configuration is not readable and writable: %w", err)
		}
	}
	return nil
}
func checkInstallLinks(info os.FileInfo) error {
	if s, ok := info.Sys().(*syscall.Stat_t); ok && s.Nlink != 1 {
		return errors.New("hardlinked configuration files are unsupported; use a separate regular file")
	}
	return nil
}
func sameInstallOwner(a, b os.FileInfo) bool {
	aa, aok := a.Sys().(*syscall.Stat_t)
	bb, bok := b.Sys().(*syscall.Stat_t)
	return aok && bok && aa.Uid == bb.Uid && aa.Gid == bb.Gid
}
func preserveInstallMetadata(source string, destination *os.File, info os.FileInfo) error {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("configuration ownership is unavailable")
	}
	current, err := destination.Stat()
	if err != nil {
		return err
	}
	if !sameInstallOwner(current, info) {
		if err := destination.Chown(int(s.Uid), int(s.Gid)); err != nil {
			return err
		}
	}
	if err := destination.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	return preserveInstallAttributes(source, destination.Name())
}
