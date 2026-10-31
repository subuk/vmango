package config

import (
	"encoding/json"
	"strings"
	"subuk/vmango/compute"
	"testing"
	"time"
)

func TestAcceptedAdministrationProjectionSafeAndComplete(t *testing.T) {
	text := strings.Replace(cloudConfig, `path="/source" os_arch="x86_64"`, `url="https://images.example:8443/private-signed-path?token=secret-query" sha256="`+strings.Repeat("a", 64)+`" source_format="qcow2" source_size_bytes=0 os_arch="x86_64" guest_agent=false protected=true hidden=false minimum_memory_mib=128 minimum_disk_gib=2`, 1)
	cfg, err := parseCloudTest(t, text)
	if err != nil {
		t.Fatal(err)
	}
	cfg.API.Oidc.ClientSecret = "private-client-secret"
	cfg.API.Oidc.AllowedEmails = []string{"private-email@example.test"}
	cfg.API.JWTSigningKey = "private-signing-key"
	cfg.KeyFile = "/private/credential-directory"
	cfg.Subscribes = []SubscribeConfig{{Script: "/private/hook"}}
	snapshot, err := cfg.AdministrationSnapshot("test-version")
	if err != nil {
		t.Fatal(err)
	}
	service := compute.NewAdministrationService(snapshot, nil)
	if snapshot.Installation.SnapshotID == "" || snapshot.Installation.Version != "test-version" || snapshot.Installation.Activation != "restart" {
		t.Fatal(snapshot.Installation)
	}
	if _, err := time.Parse(time.RFC3339, snapshot.Installation.LoadedAt); err != nil {
		t.Fatal(err)
	}
	image := snapshot.Images[0]
	if image.Defaults.GuestAgent == nil || *image.Defaults.GuestAgent || image.Source.SizeBytes != 0 || !image.Protected || image.Hidden || image.MinimumMemoryBytes != 128<<20 || image.MinimumDiskBytes != 2<<30 {
		t.Fatal(image)
	}
	if image.Source.Remote == nil || image.Source.Remote.Scheme != "https" || image.Source.Remote.Host != "images.example" || image.Source.Remote.Port != "8443" || !image.Source.Remote.Redacted {
		t.Fatal(image.Source)
	}
	if image.Defaults.Graphics != "vnc" || image.Defaults.VideoModel != "none" {
		t.Fatal("domain display defaults lost", image.Defaults)
	}
	h := snapshot.Hosts[0]
	if h.MemoryReserveBytes != 512<<20 || h.CPUOvercommit != 4 || h.ConfigDriveFormat != "nocloud" || h.Emulator.State != "unresolved" || h.Networks[0].Backend != "default" || h.Storage[0].Format != "qcow2" {
		t.Fatal(h)
	}
	raw, _ := json.Marshal(snapshot)
	for _, secret := range []string{"private-signed-path", "secret-query", "private-client-secret", "private-email", "private-signing-key", "credential-directory", "/private/hook", "qemu+ssh://", "qemu:///"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("secret leaked: %s", secret)
		}
	}
	cfg.Clouds[0].Name = "edited file"
	cfg.Clouds[0].Hosts[0].Labels["rack"] = "edited"
	*cfg.Clouds[0].Images[0].GuestAgent = true
	c, _ := service.Cloud("test")
	saved, _ := service.Image("test", "linux")
	savedHost, _ := service.HostConfiguration("test", "a")
	if c.Name != "Test cloud" || *saved.Defaults.GuestAgent || savedHost.Labels["rack"] != "one" {
		t.Fatal("snapshot changed after config edit")
	}
}
