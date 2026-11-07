package libvirt

import (
	"net/url"
	"reflect"
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
			want := []string{"-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=15"}
			want = append(want, tt.wantConnection...)
			want = append(want, graphicsForwardCommand(tt.consoleHost, "5903"))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v; want %v", got, want)
			}
		})
	}
}
