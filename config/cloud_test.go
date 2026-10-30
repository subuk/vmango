package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cloudConfig = `
libvirt "a" { uri="qemu:///session" }
libvirt "b" { uri="qemu+ssh://b/system" }
image "linux" { path="/source" os_arch="x86_64" }
cloud "test" {
 name="Test cloud"
 images=["linux"]
 zone "zone" { name="Zone" }
 instance_type "small" { cpu=2 cpu_request=1 memory_mib=2048 boot_disk_gib=20 }
 network "lan" { name="LAN" }
 storage_class "fast" { name="Fast" }
 host "a" { zone="zone" cpu_reserve=1 memory_reserve_mib=512 labels={rack="one"} network "lan" {network="default"} storage_class "fast" {pool="nvme"} }
 host "b" { zone="zone" labels={rack="two"} network "lan" {network="br-lan"} storage_class "fast" {pool="ssd" format="raw"} }
}
`

func parseCloudTest(t *testing.T, text string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vmango.conf")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return Parse(path)
}
func TestCloudBindingsAndRequests(t *testing.T) {
	cfg, err := parseCloudTest(t, cloudConfig)
	if err != nil {
		t.Fatal(err)
	}
	clouds, err := cfg.CloudDefinitions()
	if err != nil || len(clouds) != 1 {
		t.Fatal(clouds, err)
	}
	c := clouds[0]
	if len(c.Hosts) != 2 || c.Hosts[0].Storage["fast"].Pool != "nvme" || c.Hosts[1].Storage["fast"].Pool != "ssd" || c.InstanceTypes[0].CPURequest != 1 || c.InstanceTypes[0].MemoryRequestBytes != 2<<30 || c.InstanceTypes[0].BootDiskBytes != 20<<30 || c.Hosts[0].MemoryReserveBytes != 512<<20 {
		t.Fatalf("incorrect cloud conversion: %+v", c)
	}
}
func TestInvalidCloudConfiguration(t *testing.T) {
	for _, tt := range []struct{ old, new, reason string }{{"memory_mib=2048", "memory_mib=2048 memory_request_mib=1024", "memory overselling is not supported yet"}, {"cpu_request=1", "cpu_request=3", "invalid instance type"}, {"cpu_request=1", "cpu_request=0", "invalid instance type"}, {"zone=\"zone\"", "zone=\"unknown\"", "invalid host"}, {"images=[\"linux\"]", "images=[\"unknown\"]", "unknown image"}, {"pool=\"nvme\"", "pool=\"nvme\" format=\"invalid\"", "invalid storage binding"}, {"qemu+ssh://b/system", "qemu:///session", "exactly one cloud"}, {"memory_mib=2048", "memory_mib=-1", "invalid or overflowing"}} {
		t.Run(tt.reason, func(t *testing.T) {
			_, err := parseCloudTest(t, strings.Replace(cloudConfig, tt.old, tt.new, 1))
			if err == nil || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("expected %s: %v", tt.reason, err)
			}
		})
	}
}
