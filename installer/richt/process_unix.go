//go:build !windows

package richt

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

func configureCancellation(cmd *exec.Cmd) func() {
	done := make(chan struct{})
	var once sync.Once
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		// Give Richt time to persist interrupted task progress. The command's
		// WaitDelay provides a bound if the executor does not acknowledge SIGINT.
		pid := cmd.Process.Pid
		err := syscall.Kill(-pid, syscall.SIGINT)
		go func() {
			timer := time.NewTimer(4 * time.Second)
			defer timer.Stop()
			select {
			case <-done:
			case <-timer.C:
				_ = syscall.Kill(-pid, syscall.SIGKILL)
			}
		}()
		return err
	}
	cmd.WaitDelay = 5 * time.Second
	return func() { once.Do(func() { close(done) }) }
}
