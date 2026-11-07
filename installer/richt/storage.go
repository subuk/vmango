package richt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

// InspectStorage observes the filesystem that will contain a selected pool.
// It never creates paths. Every existing component must be a real directory;
// symlinks are rejected so a reviewed pool cannot silently name another path.
func (c *Client) InspectStorage(ctx context.Context, host Host, directory string) (uint64, error) {
	if err := host.Validate(); err != nil {
		return 0, err
	}
	if !path.IsAbs(directory) || path.Clean(directory) != directory || directory == "/" || strings.ContainsAny(directory, "\x00\r\n") {
		return 0, errors.New("pool path must be a clean absolute non-root directory")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := c.target(ctx, host, []byte(storageProbe), true, "python3", "-", directory)
	var result struct {
		FreeBytes uint64 `json:"free_bytes"`
		Error     string `json:"error"`
	}
	if decodeErr := json.Unmarshal(out, &result); decodeErr != nil {
		return 0, fmt.Errorf("cannot inspect target storage path: %w", errors.Join(err, decodeErr))
	}
	if result.Error != "" {
		return 0, fmt.Errorf("cannot inspect target storage path: %s", result.Error)
	}
	if err != nil {
		return 0, fmt.Errorf("cannot inspect target storage path: %w", err)
	}
	return result.FreeBytes, nil
}

const storageProbe = `import json, os, stat, sys
try:
    requested = sys.argv[1]
    current = "/"
    nearest = current
    for component in requested.split("/")[1:]:
        current = os.path.join(current, component)
        try:
            info = os.lstat(current)
        except FileNotFoundError:
            break
        if stat.S_ISLNK(info.st_mode):
            raise ValueError("pool path contains a symbolic link")
        if not stat.S_ISDIR(info.st_mode):
            raise ValueError("pool path contains a non-directory entry")
        nearest = current
    space = os.statvfs(nearest)
    print(json.dumps({"free_bytes": space.f_bavail * space.f_frsize}))
except (OSError, ValueError) as error:
    print(json.dumps({"error": str(error)}))
    sys.exit(1)
`
