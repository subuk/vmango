package libvirt

import (
	"errors"
	"net"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"testing"
)

func TestGraphicsSSHRetainsConnectionSettings(t *testing.T) {
	for _, tt := range []struct {
		name, uri, consoleHost string
		wantConnection         []string
	}{
		{"alias uses SSH config user and port", "qemu+ssh://test-alias/system", "127.0.0.1", []string{"--", "test-alias"}},
		{"explicit user", "qemu+ssh://operator@test-alias/system", "127.0.0.1", []string{"-l", "operator", "--", "test-alias"}},
		{"explicit port without user", "qemu+ssh://test-alias:2222/system", "127.0.0.1", []string{"-p", "2222", "--", "test-alias"}},
		{"explicit user and port", "qemu+ssh://operator@test-alias:2222/system", "127.0.0.1", []string{"-l", "operator", "-p", "2222", "--", "test-alias"}},
		{"IPv6 host and console", "qemu+ssh://operator@[2001:db8::10]:2200/system", "::1", []string{"-l", "operator", "-p", "2200", "--", "2001:db8::10"}},
		{"empty URI user preserves SSH config", "qemu+ssh://@test-alias/system", "127.0.0.1", []string{"--", "test-alias"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			uri, err := url.Parse(tt.uri)
			if err != nil {
				t.Fatal(err)
			}
			got := graphicsSSHArguments(uri, tt.consoleHost, 5903)
			want := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=15", "-o", "ExitOnForwardFailure=yes"}
			connection := tt.wantConnection
			want = append(want, connection[:len(connection)-2]...)
			want = append(want, "-W", net.JoinHostPort(tt.consoleHost, "5903"))
			want = append(want, connection[len(connection)-2:]...)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v; want %v", got, want)
			}
		})
	}
}

func TestNonSSHLibvirtNeverReusesAPIPort(t *testing.T) {
	for _, uriString := range []string{"qemu+tls://libvirt-user@hypervisor:16514/system", "qemu+tcp://hypervisor:16509/system"} {
		uri, _ := url.Parse(uriString)
		args := strings.Join(graphicsSSHArguments(uri, "127.0.0.1", 5900), " ")
		if !strings.Contains(args, "-p 22 -W 127.0.0.1:5900 -- hypervisor") || strings.Contains(args, "16514") || strings.Contains(args, "16509") || strings.Contains(args, "libvirt-user") {
			t.Fatal(args)
		}
	}
}
func TestGraphicsTunnelFailureIsActionableAndDoesNotFallback(t *testing.T) {
	original := graphicsSSHCommand
	t.Cleanup(func() { graphicsSSHCommand = original })
	graphicsSSHCommand = func(args []string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGraphicsSSHHelper$")
		cmd.Env = append(os.Environ(), "VMANGO_GRAPHICS_SSH_TEST_HELPER=1")
		return cmd
	}

	uri, _ := url.Parse("qemu+tls://unreachable.example:16514/system")
	tunnel, err := openGraphicsTunnel(uri, "127.0.0.1", 5900)
	var unavailable *compute.ConsoleUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("console failure must carry safe API diagnostic: %v", err)
	}
	if tunnel != nil || err == nil || !strings.Contains(err.Error(), "VM management remains available") || !strings.Contains(err.Error(), "trusted host keys") {
		t.Fatalf("unexpected tunnel result: %v %v", tunnel, err)
	}
}

func TestGraphicsSSHHelper(t *testing.T) {
	if os.Getenv("VMANGO_GRAPHICS_SSH_TEST_HELPER") == "1" {
		os.Exit(1)
	}
}
