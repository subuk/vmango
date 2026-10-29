package configdrive

import (
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
	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample release-smoke"
	cases := []struct {
		name   string
		format Format
		data   Data
	}{
		{"nocloud", FormatNoCloud, &NoCloud{Metadata: NoCloudMetadata{
			InstanceId: "release-smoke", Hostname: "release-smoke", PublicKeys: []string{key},
		}}},
		{"openstack", FormatOpenstack, &Openstack{Metadata: OpenstackMetadata{
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
			parsed, err := ParseIso([]Format{tc.format}, iso)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Hostname() != tc.data.Hostname() || !reflect.DeepEqual(parsed.PublicKeys(), tc.data.PublicKeys()) {
				t.Fatalf("config-drive data changed: %#v", parsed)
			}
		})
	}
}
