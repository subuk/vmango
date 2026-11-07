package richt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// SupportedRevision pins the separately built local Richt executable. Build
// with -X github.com/subuk/richt/cli.Version=<revision>; do not use a dev binary.
const SupportedRevision = "43f24f90aae04766f8c5979b06ce08cb33e703eb"

var runtimeManifest = struct{ Version string }{SupportedRevision}

func discover(_ context.Context, explicit, _ string) (string, string, error) {
	path := explicit
	var err error
	if path == "" {
		path, err = exec.LookPath("richt")
		if err != nil {
			return "", "", errors.New("Richt is required: provide --richt /absolute/path/to/richt or install the pinned binary on PATH")
		}
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	// Resolve an operator-supplied PATH symlink once, then retain and verify the
	// resolved binary throughout this installation session.
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", "", err
	}
	if !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 || st.Size() > 128<<20 {
		return "", "", errors.New("Richt path must be an executable regular file of at most 128 MiB")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", "", err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if err = verifyExecutable(path, sum); err != nil {
		return "", "", err
	}
	return path, sum, nil
}
