package config

import (
	"os"
	"path/filepath"
	"subuk/vmango/compute"
	"testing"
)

func TestImageConversion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.conf")
	text := `
image "/images/legacy" { os_name = "Ubuntu" os_arch = "aarch64" efi = true protected = true }
image "normal" { path = "/images/shared" os_name = "Ubuntu" os_arch = "aarch64" }
image "custom" { path = "/images/shared" name = "Custom Ubuntu" os_arch = "aarch64" guest_agent = false initialization = "none" disk_bus = "scsi" disk_controller = "virtio-scsi" minimum_memory_mib = 512 minimum_disk_gib = 10 }
image "/dev/private" { hidden = true }
volume "/images/shared" { protected = true }
volume "/ordinary" { protected = true hidden = true }
`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	images, policies, err := cfg.ImageDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	if len(images) != 3 {
		t.Fatalf("images = %+v", images)
	}
	if images[0].ID != "/images/legacy" || images[0].Defaults.Firmware != "efi" {
		t.Fatalf("legacy = %+v", images[0])
	}
	if images[1].SourcePath != images[2].SourcePath || images[1].ID == images[2].ID {
		t.Fatal("source and identity were conflated")
	}
	if images[1].Defaults.GuestAgent != nil || images[2].Defaults.GuestAgent == nil || *images[2].Defaults.GuestAgent {
		t.Fatal("omitted and false were conflated")
	}
	if images[2].Defaults.Initialization != "none" || images[2].Defaults.DiskBus != compute.DeviceBusScsi || images[2].MinimumMemoryBytes != 512<<20 || images[2].MinimumDiskBytes != 10<<30 {
		t.Fatalf("custom = %+v", images[2])
	}
	if !policies["/dev/private"].Hidden || !policies["/images/legacy"].Protected || !policies["/images/shared"].Protected || !policies["/ordinary"].Hidden || !policies["/ordinary"].Protected {
		t.Fatalf("lost volume policies: %+v", policies)
	}
}

func TestInvalidImageConfiguration(t *testing.T) {
	for _, options := range []string{`disk_bus = "banana"`, `initialization = "invalid"`, `efi = true firmware = "bios"`, `disk_controller = "virtio-scsi"`, `minimum_disk_gib = 18446744073709551615`} {
		t.Run(options, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.conf")
			os.WriteFile(path, []byte(`image "test" { path = "/base" os_arch = "x86_64" `+options+` }`), 0600)
			if _, err := Parse(path); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}
