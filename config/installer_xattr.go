//go:build linux || darwin

package config

import (
	"bytes"
	"errors"
	"golang.org/x/sys/unix"
)

func preserveInstallAttributes(source, destination string) error {
	n, err := unix.Listxattr(source, nil)
	if errors.Is(err, unix.ENOTSUP) {
		return nil
	}
	if err != nil {
		return err
	}
	names := make([]byte, n)
	n, err = unix.Listxattr(source, names)
	if err != nil {
		return err
	}
	for _, raw := range bytes.Split(names[:n], []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		name := string(raw)
		size, err := unix.Getxattr(source, name, nil)
		if err != nil {
			return err
		}
		value := make([]byte, size)
		size, err = unix.Getxattr(source, name, value)
		if err != nil {
			return err
		}
		if err := unix.Setxattr(destination, name, value[:size], 0); err != nil {
			return err
		}
	}
	return nil
}
