//go:build !linux && !darwin && !windows

package config

import "errors"

func preserveInstallAttributes(source, destination string) error {
	return errors.New("installer metadata preservation is supported on macOS and Linux only")
}
