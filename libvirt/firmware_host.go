package libvirt

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"subuk/vmango/libvirt/hardware"
	"time"
)

func shellLiteral(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }

// Firmware metadata has no libvirt RPC. Only bounded read-only commands use the
// same local/SSH identity as the libvirt connection. No paths are copied locally.
func readFirmwareHost(uri, command string) ([]byte, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if u.Host == "" && (u.Scheme == "qemu" || u.Scheme == "qemu+unix") {
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
		return nil, fmt.Errorf("firmware metadata/artifact validation requires a local or SSH connection; transport %s cannot verify host firmware files", u.Scheme)
	}
	var output bytes.Buffer
	cmd.Stdout = &boundedFirmwareOutput{buffer: &output, remaining: 4 << 20}
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("read-only firmware host validation failed: %w", err)
	}
	return output.Bytes(), nil
}

type boundedFirmwareOutput struct {
	buffer    *bytes.Buffer
	remaining int
}

func (w *boundedFirmwareOutput) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, fmt.Errorf("firmware metadata exceeds 4 MiB")
	}
	w.remaining -= len(p)
	return w.buffer.Write(p)
}

func hostFirmwareDescriptors(uri string) ([]hardware.FirmwareDescriptor, error) {
	// Directory precedence mirrors QEMU's metadata convention. Later entries of
	// the same filename override vendor entries, including zero-length masks.
	command := `for d in /usr/share/qemu/firmware /usr/local/share/qemu/firmware /opt/homebrew/share/qemu/firmware /etc/qemu/firmware /usr/local/etc/qemu/firmware /opt/homebrew/etc/qemu/firmware; do for f in "$d"/*.json; do if test -f "$f"; then printf '%s\000' "${f##*/}"; cat "$f"; printf '\000'; fi; done; done`
	u, _ := url.Parse(uri)
	if u != nil && u.Path == "/session" {
		command += `; for f in "${XDG_CONFIG_HOME:-$HOME/.config}/qemu/firmware/"*.json; do if test -f "$f"; then printf '%s\000' "${f##*/}"; cat "$f"; printf '\000'; fi; done`
	}
	raw, err := readFirmwareHost(uri, command)
	if err != nil {
		return nil, err
	}
	return hardware.DecodeFirmwareDescriptors(raw)
}
func validateFirmwareFiles(uri string, b hardware.FirmwareBinding) error {
	for _, v := range []struct{ path, format string }{{b.Loader, b.LoaderFormat}, {b.NVRAMTemplate, b.NVRAMFormat}} {
		if v.path == "" {
			continue
		}
		raw, err := readFirmwareHost(uri, "test -f "+shellLiteral(v.path)+" && test -r "+shellLiteral(v.path)+" && od -An -tx1 -N4 "+shellLiteral(v.path))
		if err != nil {
			return fmt.Errorf("firmware file %s is not readable on the hypervisor: %w", v.path, err)
		}
		header := strings.Join(strings.Fields(string(raw)), "")
		if len(header) != 8 {
			return fmt.Errorf("firmware file %s has an incomplete header", v.path)
		}
		qcow := header == "514649fb"
		if qcow != (v.format == "qcow2") {
			return fmt.Errorf("firmware file %s does not match declared %s format", v.path, v.format)
		}
	}
	return nil
}
