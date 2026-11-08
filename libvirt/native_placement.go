package libvirt

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

// Libvirt has no read-only RPC for numad availability. Use the already configured
// local/SSH transport for this bounded prerequisite check, never start services
// or allocate CPU IDs. Other transports fail closed with actionable context.
func checkNativePlacement(uri string) error {
	u, err := url.Parse(uri)
	if err != nil {
		return err
	}
	command := "test -d /sys/devices/system/node && command -v numad >/dev/null && pgrep -x numad >/dev/null"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if u.Host == "" && u.Scheme == "qemu" {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	} else if strings.Contains(u.Scheme, "ssh") {
		args := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10"}
		if u.User != nil && u.User.Username() != "" {
			args = append(args, "-l", u.User.Username())
		}
		if u.Port() != "" {
			args = append(args, "-p", u.Port())
		}
		if key := u.Query().Get("keyfile"); key != "" {
			args = append(args, "-i", key)
		}
		args = append(args, "--", u.Hostname(), command)
		cmd = exec.CommandContext(ctx, "ssh", args...)
	} else {
		return fmt.Errorf("native auto placement prerequisites cannot be verified through this transport; use a local or SSH libvirt connection to a Linux host with active numad")
	}
	if err = cmd.Run(); err != nil {
		return fmt.Errorf("native auto placement requires Linux NUMA facilities and installed/running numad, verifiable through the configured connection; prepare the host explicitly")
	}
	return nil
}
