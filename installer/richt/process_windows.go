//go:build windows

package richt

import "os/exec"

func configureCancellation(cmd *exec.Cmd) func() { return func() {} }
