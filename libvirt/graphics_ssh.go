package libvirt

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"subuk/vmango/compute"
	"time"
)

// graphicsSSHArguments preserves SSH URI connection settings. Other libvirt
// transports use port 22 explicitly, never the libvirt API port.
func graphicsSSHArguments(uri *url.URL, consoleHost string, consolePort int) []string {
	args := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=15", "-o", "ExitOnForwardFailure=yes"}
	if strings.Contains(uri.Scheme, "ssh") {
		if uri.User != nil && uri.User.Username() != "" {
			args = append(args, "-l", uri.User.Username())
		}
		if port := uri.Port(); port != "" {
			args = append(args, "-p", port)
		}
		if key := uri.Query().Get("keyfile"); key != "" {
			args = append(args, "-i", key)
		}
	} else {
		args = append(args, "-p", "22")
	}
	return append(args, "-W", net.JoinHostPort(consoleHost, strconv.Itoa(consolePort)), "--", uri.Hostname())
}

type graphicsTunnel struct {
	stdin  io.WriteCloser
	stdout io.Reader
	cmd    *exec.Cmd
	done   <-chan error
}

func (t *graphicsTunnel) Read(p []byte) (int, error)  { return t.stdout.Read(p) }
func (t *graphicsTunnel) Write(p []byte) (int, error) { return t.stdin.Write(p) }
func (t *graphicsTunnel) Close() error {
	_ = t.stdin.Close()
	if t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	return nil
}

var graphicsSSHCommand = func(args []string) *exec.Cmd { return exec.Command("ssh", args...) }

func openGraphicsTunnel(uri *url.URL, host string, port int) (*graphicsTunnel, error) {
	cmd := graphicsSSHCommand(graphicsSSHArguments(uri, host, port))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// SSH diagnostics may contain local paths and usernames. Expose an actionable
	// transport error without echoing them to ordinary console users.
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return nil, &compute.ConsoleUnavailableError{Message: fmt.Sprintf("Graphical console unavailable: cannot start SSH tunnel to %s; install SSH and configure non-interactive authentication and trusted host keys. VM management remains available.", uri.Hostname())}
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	type greetingResult struct {
		data []byte
		err  error
	}
	greeting := make(chan greetingResult, 1)
	go func() {
		data := make([]byte, 12)
		_, err := io.ReadFull(stdout, data)
		greeting <- greetingResult{data, err}
	}()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	fail := func(cause string) (*graphicsTunnel, error) {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		return nil, &compute.ConsoleUnavailableError{Message: fmt.Sprintf("Graphical console unavailable: SSH tunnel to %s failed (%s); verify SSH access, non-interactive authentication, trusted host keys and forwarding to the host's loopback VNC port. VM management remains available.", uri.Hostname(), cause)}
	}
	select {
	case result := <-greeting:
		if result.err != nil || !bytes.HasPrefix(result.data, []byte("RFB ")) {
			return fail("no VNC greeting")
		}
		return &graphicsTunnel{stdin: stdin, stdout: io.MultiReader(bytes.NewReader(result.data), stdout), cmd: cmd, done: done}, nil
	case <-timer.C:
		return fail("timeout")
	}
}
