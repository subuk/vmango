//go:build windows

package config

import (
	"errors"
	"os"
)

func checkInstallWritable(string, bool) error {
	return errors.New("installer is supported on macOS and Linux only")
}
func checkInstallLinks(os.FileInfo) error            { return nil }
func sameInstallOwner(os.FileInfo, os.FileInfo) bool { return false }
func preserveInstallMetadata(string, *os.File, os.FileInfo) error {
	return errors.New("installer is unsupported on Windows")
}
