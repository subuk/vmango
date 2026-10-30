//go:build !windows

package filesystem

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func makeAuthDirectory(path string) error { return os.MkdirAll(path, 0700) }
func checkAuthPath(path string, dir bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	mode := os.FileMode(0600)
	if dir {
		mode = 0700
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != mode || info.Mode()&os.ModeSymlink != 0 || (dir && !info.IsDir()) || (!dir && !info.Mode().IsRegular()) {
		return fmt.Errorf("unsafe authentication store owner or permissions: %s (require %04o)", path, mode)
	}
	return nil
}
func openAuthFile(path string, create bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC
	if create {
		flags = unix.O_RDWR | unix.O_CREAT | unix.O_NOFOLLOW | unix.O_CLOEXEC
	}
	fd, err := unix.Open(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func lockAuthFile(f *os.File) error         { return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB) }
func replaceAuthFile(old, new string) error { return os.Rename(old, new) }
func syncAuthDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
