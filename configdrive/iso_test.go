package configdrive

import (
	"bytes"
	"os"
	"os/exec"
	"reflect"
	"testing"
)

func TestISORoundTrip(t *testing.T) {
	for _, tool := range []string{"mkisofs", "isoinfo"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is required for the config-drive round trip", tool)
		}
	}
	userData := []byte("#cloud-config\nwrite_files:\n  - path: /etc/example\n    content: |\n      keep indentation\n\n")
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample release-smoke"
	cases := []struct {
		name   string
		format Format
		data   Data
	}{
		{"nocloud", FormatNoCloud, &NoCloud{Userdata: userData, Metadata: NoCloudMetadata{
			InstanceId: "release-smoke", Hostname: "release-smoke", PublicKeys: []string{key},
		}}},
		{"openstack", FormatOpenstack, &Openstack{Userdata: userData, Metadata: OpenstackMetadata{
			UUID: "release-smoke", Hostname: "release-smoke", PublicKeys: map[string]string{"test": key},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			iso, err := GenerateIso(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			defer iso.Close()
			defer os.Remove(iso.Name())
			parsed, err := ParseIso([]Format{tc.format}, iso)
			if err != nil {
				t.Fatal(err)
			}
			var actual []byte
			switch data := parsed.(type) {
			case *NoCloud:
				actual = data.Userdata
			case *Openstack:
				actual = data.Userdata
			}
			if !bytes.Equal(actual, userData) {
				t.Fatal("user-data changed during config-drive round trip")
			}
			if parsed.Hostname() != tc.data.Hostname() || !reflect.DeepEqual(parsed.PublicKeys(), tc.data.PublicKeys()) {
				t.Fatalf("config-drive data changed: %#v", parsed)
			}
		})
	}
}
