//go:build windows

package installer

import (
	"fmt"
	"os"
)

func lockConfig(path string) (*os.File, error) {
	return nil, fmt.Errorf("the installer supports macOS and Linux controllers")
}
