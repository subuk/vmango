package config

import (
	"os"

	"golang.org/x/sys/windows"
)

func lockSigningKey(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &windows.Overlapped{})
}
