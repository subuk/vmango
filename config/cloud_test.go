package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cloudConfig = `
image "linux" { path="/source" os_arch="x86_64" }
cloud "test" {
 name="Test cloud"
 images=["linux"]
 zone "zone" { name="Zone" }
 instance_type "small" { cpu=2 cpu_request=1 memory_mib=2048 boot_disk_gib=20 }
 network "lan" { name="LAN" }
 storage_class "fast" { name="Fast" }
 host "a" { uri="qemu:///session" zone="zone" cpu_reserve=1 memory_reserve_mib=512 labels={rack="one"} network "lan" {network="default"} storage_class "fast" {pool="nvme"} }
 host "b" { uri="qemu+ssh://b/system" zone="zone" labels={rack="two"} network "lan" {network="br-lan"} storage_class "fast" {pool="ssd" format="raw"} }
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

func TestAdministratorPolicyOwnershipAndDefaults(t *testing.T) {
	cfg, err := parseCloudTest(t, cloudConfig)
	if err != nil {
		t.Fatal(err)
	}
	clouds, _ := cfg.CloudDefinitions()
	images, _, _ := cfg.ImageDefinitions()
	g, v := images[0].Defaults.Display()
	if clouds[0].InstanceTypes[0].Autostart || images[0].Defaults.GuestAgent != nil || g.String() != "vnc" || v.String() != "none" || cfg.Hosts()[0].GraphicsListen != "" {
		t.Fatal("omission defaults changed")
	}
	cfg, err = parseCloudTest(t, strings.Replace(cloudConfig, "cpu=2 cpu_request=1", "autostart=true cpu=2 cpu_request=1", 1))
	if err != nil {
		t.Fatal(err)
	}
	clouds, _ = cfg.CloudDefinitions()
	if !clouds[0].InstanceTypes[0].Autostart {
		t.Fatal("explicit type autostart lost")
	}
	for _, tt := range []struct{ old, new, field string }{
		{`os_arch="x86_64"`, `os_arch="x86_64" autostart=true`, "autostart"},
		{`os_arch="x86_64"`, `os_arch="x86_64" graphics_listen="127.0.0.1"`, "graphics_listen"},
		{`cpu=2 cpu_request=1`, `cpu=2 cpu_request=1 guest_agent=true`, "guest_agent"},
		{`cpu=2 cpu_request=1`, `cpu=2 cpu_request=1 graphics="vnc"`, "graphics"},
		{`uri="qemu:///session"`, `uri="qemu:///session" video_model="qxl"`, "video_model"},
	} {
		_, err := parseCloudTest(t, strings.Replace(cloudConfig, tt.old, tt.new, 1))
		if err == nil || !strings.Contains(err.Error(), tt.field+" belongs only") {
			t.Fatalf("misplaced %s accepted: %v", tt.field, err)
		}
	}
}

func TestCloudHostBackendSettings(t *testing.T) {
	settings := `uri="qemu:///session" image_pool="images" config_drive_pool="seed" config_drive_suffix="-seed.iso" config_drive_write_format="openstack" emulator="/usr/local/bin/qemu-custom" graphics_listen="::1" qcow_prealloc_metadata=true cache=true`
	cfg, err := parseCloudTest(t, strings.Replace(cloudConfig, `uri="qemu:///session"`, settings, 1))
	if err != nil {
		t.Fatal(err)
	}
	hosts := cfg.Hosts()
	if len(hosts) != 2 || hosts[0].Node != "a" || hosts[1].Node != "b" {
		t.Fatalf("host order/identity lost: %+v", hosts)
	}
	h := hosts[0]
	if h.Uri != "qemu:///session" || h.ImagePool != "images" || h.ConfigDrivePool != "seed" || h.ConfigDriveSuffix != "-seed.iso" || h.ConfigDriveWriteFormat != "openstack" || h.Emulator != "/usr/local/bin/qemu-custom" || h.GraphicsListen != "::1" || !h.QcowPreallocMetadata || !h.Cache {
		t.Fatalf("backend settings lost: %+v", h)
	}
	other := hosts[1]
	if other.ImagePool != "default" || other.ConfigDrivePool != "default" || other.ConfigDriveSuffix != "_config.iso" || other.ConfigDriveWriteFormat != "nocloud" || other.Emulator != "" || other.GraphicsListen != "" || other.QcowPreallocMetadata || other.Cache {
		t.Fatalf("defaults leaked between hosts: %+v", other)
	}
	clouds, err := cfg.CloudDefinitions()
	if err != nil || clouds[0].Hosts[0].Node != "a" || clouds[0].Hosts[0].Storage["fast"].Pool != "nvme" {
		t.Fatal("domain bindings changed", clouds, err)
	}
}

func TestInvalidCloudHostBackendSettings(t *testing.T) {
	for _, tc := range []struct{ old, replacement, message string }{
		{`uri="qemu:///session"`, ``, "uri is required"},
		{`uri="qemu:///session"`, `uri=""`, "uri is required"},
		{`uri="qemu:///session"`, `uri="qemu:///session" graphics_listen="localhost"`, "graphics_listen"},
		{`uri="qemu:///session"`, `uri="qemu:///session" config_drive_write_format="invalid"`, "config_drive_write_format"},
		{`host "b"`, `host "a"`, "exactly one cloud"},
	} {
		t.Run(tc.message+tc.replacement, func(t *testing.T) {
			_, err := parseCloudTest(t, strings.Replace(cloudConfig, tc.old, tc.replacement, 1))
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("expected %q: %v", tc.message, err)
			}
		})
	}
}

func TestCloudHostConnectionsAreGloballyUnique(t *testing.T) {
	second := cloudConfig[strings.Index(cloudConfig, `cloud "test"`):]
	second = strings.NewReplacer(`cloud "test"`, `cloud "other"`, `host "a"`, `host "c"`, `host "b"`, `host "d"`, `qemu:///session`, `qemu+ssh://c/system`, `qemu+ssh://b/system`, `qemu+ssh://d/system`).Replace(second)
	cfg, err := parseCloudTest(t, cloudConfig+second)
	if err != nil {
		t.Fatal(err)
	}
	if hosts := cfg.Hosts(); len(hosts) != 4 || hosts[2].Node != "c" || hosts[3].Node != "d" {
		t.Fatalf("hosts from other cloud missing: %+v", hosts)
	}
	for _, duplicate := range []string{
		strings.Replace(second, `host "c"`, `host "a"`, 1),
		strings.Replace(second, `qemu+ssh://c/system`, `qemu:///session`, 1),
	} {
		if _, err := parseCloudTest(t, cloudConfig+duplicate); err == nil || !strings.Contains(err.Error(), "exactly one cloud") {
			t.Fatalf("duplicate host/connection accepted: %v", err)
		}
	}
}

func TestRemovedLibvirtConfigurationRequiresMigration(t *testing.T) {
	for _, old := range []string{
		`libvirt "a" { uri="qemu:///session" }`,
		`libvirt_uri = "qemu:///session"`,
		`libvirt_uri = ""`,
		`libvirt_config_drive_pool = "seed"`,
		`libvirt_config_drive_suffix = "-seed.iso"`,
		`libvirt_config_drive_write_format = "openstack"`,
	} {
		_, err := parseCloudTest(t, cloudConfig+old)
		if err == nil || !strings.Contains(err.Error(), "move libvirt settings") || !strings.Contains(err.Error(), "docs/CLOUDS.md") {
			t.Fatalf("missing migration guidance for %s: %v", old, err)
		}
	}
	for _, misplaced := range []string{
		`uri="qemu:///system"`,
		`host "orphan" { uri="qemu:///system" }`,
		strings.Replace(cloudConfig, `host "a" {`, `host "a" { libvirt { uri="qemu:///session" }`, 1),
		strings.Replace(cloudConfig, `cloud "test" {`, `cloud "test" { uri="qemu:///system"`, 1),
	} {
		if _, err := parseCloudTest(t, misplaced); err == nil {
			t.Fatal("misplaced backend settings accepted")
		}
	}
}
