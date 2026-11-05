//go:build !windows

package config

import (
	"os"

	"golang.org/x/sys/unix"
)

func lockSigningKey(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX)
}
