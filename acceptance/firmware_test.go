//go:build acceptance

package acceptance

import (
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"testing"
)

// All callers supply paths obtained from this run's disposable domain XML.
func firmwareHostCommand(t *testing.T, uri string, args ...string) ([]byte, error) {
	t.Helper()
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	if u.Host == "" {
		return exec.Command(args[0], args[1:]...).Output()
	}
	command := []string{}
	for _, arg := range args {
		command = append(command, "'"+strings.ReplaceAll(arg, "'", "'\"'\"'")+"'")
	}
	ssh := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=10"}
	if u.User != nil {
		ssh = append(ssh, "-l", u.User.Username())
	}
	if u.Port() != "" {
		ssh = append(ssh, "-p", u.Port())
	}
	ssh = append(ssh, "--", u.Hostname(), strings.Join(command, " "))
	return exec.Command("ssh", ssh...).Output()
}
func firmwareRead(t *testing.T, uri, path string) []byte {
	t.Helper()
	raw, err := firmwareHostCommand(t, uri, "cat", path)
	if err != nil {
		t.Fatal(fmt.Errorf("read test firmware file %s: %w", path, err))
	}
	return raw
}
