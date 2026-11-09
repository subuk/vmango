package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
	"testing"
)

func TestImageConversion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.conf")
	text := `
hardware_profile "uefi" { firmware { type = "efi" } }
image "legacy" { hardware_profile="uefi" path="/images/legacy" os_name = "Ubuntu" os_arch = "aarch64" protected = true }
image "normal" { hardware_profile="default" path = "/images/shared" os_name = "Ubuntu" os_arch = "aarch64" }
hardware_profile "custom" { guest_agent=false graphics { type="spice" video_model="qxl" } disks { bus="scsi" controller_model="virtio-scsi" } config_drive {enabled=false} }
image "custom" { hardware_profile="custom" path = "/images/shared" name = "Custom Ubuntu" os_arch = "aarch64" minimum_memory_mib = 512 minimum_disk_gib = 10 }
`
	if err := os.WriteFile(path, []byte(withImages(text)+`volume "/dev/private" {hidden=true} volume "/images/shared" {protected=true} volume "/ordinary" {protected=true hidden=true}`), 0600); err != nil {
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
	profiles := make([]hardware.Profile, len(images))
	for n, image := range images {
		profiles[n], err = hardware.DecodePolicy(image.BackendPolicy)
		if err != nil {
			t.Fatal(err)
		}
	}
	if images[0].ID != "legacy" || images[0].CloudID != "test" || profiles[0].Firmware != "efi" {
		t.Fatalf("legacy = %+v", images[0])
	}
	if images[1].SourcePath != images[2].SourcePath || images[1].ID == images[2].ID {
		t.Fatal("source and identity were conflated")
	}
	if profiles[1].GuestAgent == nil || !*profiles[1].GuestAgent || profiles[2].GuestAgent == nil || *profiles[2].GuestAgent {
		t.Fatal("omitted and false were conflated")
	}
	if profiles[2].Graphics != "spice" || profiles[2].VideoModel != "qxl" {
		t.Fatal("image display policy lost")
	}
	if profiles[2].ConfigDrive.IsEnabled() || profiles[2].DiskBus != "scsi" || images[2].MinimumMemoryBytes != 512<<20 || images[2].MinimumDiskBytes != 10<<30 {
		t.Fatalf("custom = %+v", images[2])
	}
	if !policies["/dev/private"].Hidden || !policies[compute.VolumePolicyKey("a", "/images/legacy")].Protected || !policies["/images/shared"].Protected || !policies["/ordinary"].Hidden || !policies["/ordinary"].Protected {
		t.Fatalf("lost volume policies: %+v", policies)
	}
}

func TestInvalidImageConfiguration(t *testing.T) {
	for _, options := range []string{`minimum_disk_gib = 18446744073709551615`, `minimum_memory_mib = -1`} {
		t.Run(options, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.conf")
			os.WriteFile(path, []byte(withImages(`image "test" { hardware_profile="default" path = "/base" os_arch = "x86_64" `+options+` }`)), 0600)
			if _, err := Parse(path); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestImageDataDisksPreserveDeclarationOrder(t *testing.T) {
	cfg, err := parseCloudTest(t, withImages(`image "android" { hardware_profile="default"
		path = "/images/android.qcow2"
		os_arch = "aarch64"
		data_disk "userdata" { size_gib = 16 }
		data_disk "cache" { size_gib = 2 }
	}`))
	if err != nil {
		t.Fatal(err)
	}
	images, _, err := cfg.ImageDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	want := []compute.ImageDataDisk{{Name: "userdata", SizeBytes: 16 << 30}, {Name: "cache", SizeBytes: 2 << 30}}
	if len(images) != 1 || !reflect.DeepEqual(images[0].DataDisks, want) {
		t.Fatalf("image data disks = %+v, want %+v", images, want)
	}
}

func TestInvalidImageDataDiskConfiguration(t *testing.T) {
	for name, disks := range map[string]string{
		"missing size":     `data_disk "userdata" {}`,
		"zero size":        `data_disk "userdata" { size_gib = 0 }`,
		"negative size":    `data_disk "userdata" { size_gib = -1 }`,
		"fractional size":  `data_disk "userdata" { size_gib = 1.5 }`,
		"byte overflow":    `data_disk "userdata" { size_gib = 17179869184 }`,
		"integer overflow": `data_disk "userdata" { size_gib = 18446744073709551615 }`,
		"duplicate name":   `data_disk "userdata" { size_gib = 16 } data_disk "userdata" { size_gib = 2 }`,
		"reserved name":    `data_disk "root" { size_gib = 16 }`,
		"invalid name":     `data_disk "../userdata" { size_gib = 16 }`,
		"total overflow":   `data_disk "one" { size_gib = 17179869183 } data_disk "two" { size_gib = 1 }`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseCloudTest(t, withImages(`image "android" { hardware_profile="default" path = "/images/android.qcow2" os_arch = "aarch64" `+disks+` }`)); err == nil {
				t.Fatal("invalid image data disks accepted")
			}
		})
	}
}

func TestManagedSourceConfiguration(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	base := `image "managed" { hardware_profile="default" os_arch="aarch64" url="https://example.org/image" sha256="` + hash + `" source_format="qcow2" source_size_bytes=4096 }`
	path := filepath.Join(t.TempDir(), "config.conf")
	hostConfig := withImages(base)
	hostConfig = strings.Replace(hostConfig, `uri="qemu+ssh://b/system"`, `uri="qemu+ssh://b/system" image_pool="cloud"`, 1)
	if err := os.WriteFile(path, []byte(hostConfig), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	images, policies, err := cfg.ImageDefinitions()
	if err != nil || len(images) != 1 || !images[0].Managed() || images[0].SourcePath != "" || images[0].Download.SizeBytes != 4096 || len(policies) != 0 {
		t.Fatal(images, policies, err)
	}
	if cfg.Hosts()[0].ImagePool != "default" || cfg.Hosts()[1].ImagePool != "cloud" {
		t.Fatal(cfg.Hosts())
	}
	for _, invalid := range []string{
		`url="https://example.org/image"`,
		`sha256="` + hash + `"`,
		`url="ftp://example.org/image" sha256="` + hash + `" source_format="raw"`,
		`url="https://example.org/image" sha256="bad" source_format="qcow2"`,
		`url="https://example.org/image" sha256="` + hash + `" source_format="qcow2" path="/base"`,
		`url="https://example.org/image" sha256="` + hash + `" source_format="xz"`,
		`source_size_bytes=4096`,
	} {
		os.WriteFile(path, []byte(withImages(`image "invalid" { hardware_profile="default" os_arch="x86_64" `+invalid+` }`)), 0600)
		if _, err := Parse(path); err == nil {
			t.Fatal("invalid source accepted", invalid)
		}
	}
}

func TestArchiveImageConfiguration(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, format := range []string{"zip", "tar", "tar.gz"} {
		cfg, err := parseCloudTest(t, withImages(`image "archive" { hardware_profile="default"
            os_arch="aarch64" url="https://example.org/image" sha256="`+hash+`"
            source_format="qcow2" source_size_bytes=4096
            archive_format="`+format+`" archive_member="bundle/disk.qcow2" extracted_size_bytes=8192
        }`))
		if err != nil {
			t.Fatal(err)
		}
		images, _, err := cfg.ImageDefinitions()
		if err != nil {
			t.Fatal(err)
		}
		d := images[0].Download
		if d.ArchiveFormat != format || d.ArchiveMember != "bundle/disk.qcow2" || d.SizeBytes != 4096 || d.ExtractedSizeBytes != 8192 || d.Format != compute.VolumeFormatQcow2 {
			t.Fatalf("archive settings lost: %+v", d)
		}
		projected := administrationImage(images[0])
		if projected.Source.ArchiveFormat != format || projected.Source.ArchiveMember != d.ArchiveMember || projected.Source.ExtractedSizeBytes != 8192 {
			t.Fatalf("archive administration settings lost: %+v", projected.Source)
		}
	}
	base := `os_arch="aarch64" url="https://example.org/image" sha256="` + hash + `" source_format="qcow2" `
	for _, options := range []string{
		`archive_format="zip"`, `archive_member="disk.qcow2"`,
		`archive_format="rar" archive_member="disk.qcow2"`,
		`archive_format="zip" archive_member="/disk.qcow2"`,
		`archive_format="zip" archive_member="../disk.qcow2"`,
		`archive_format="zip" archive_member="bundle/../disk.qcow2"`,
		`archive_format="zip" archive_member="bundle//disk.qcow2"`,
		`archive_format="zip" archive_member="./disk.qcow2"`,
		`archive_format="zip" archive_member="C:/disk.qcow2"`,
		`archive_format="zip" archive_member="bundle\\disk.qcow2"`,
		`archive_format="zip" archive_member="disk.qcow2" extracted_size_bytes=-1`,
		`archive_format="zip" archive_member="disk.qcow2" extracted_size_bytes=18446744073709551615`,
		`extracted_size_bytes=8192`,
	} {
		if _, err := parseCloudTest(t, withImages(`image "invalid" { hardware_profile="default" `+base+options+` }`)); err == nil {
			t.Fatalf("invalid archive settings accepted: %s", options)
		}
	}
	for _, options := range []string{`archive_format="zip"`, `archive_member="disk.qcow2"`, `extracted_size_bytes=8192`} {
		if _, err := parseCloudTest(t, withImages(`image "invalid" { hardware_profile="default" path="/source" os_arch="aarch64" `+options+` }`)); err == nil {
			t.Fatalf("archive settings accepted for local image: %s", options)
		}
	}
}

func TestSharedArchiveMembersRemainIndependent(t *testing.T) {
	hash := strings.Repeat("b", 64)
	first := `image "first" { hardware_profile="default" os_arch="aarch64" url="https://example.org/archive" sha256="` + hash + `" source_format="qcow2" source_size_bytes=4096 archive_format="zip" archive_member="first.qcow2" extracted_size_bytes=8192 }`
	second := `image "second" { hardware_profile="default" os_arch="aarch64" url="https://example.org/archive" sha256="` + hash + `" source_format="raw" source_size_bytes=4096 archive_format="zip" archive_member="second.raw" extracted_size_bytes=16384 }`
	if _, err := parseCloudTest(t, withImages(first+second)); err != nil {
		t.Fatalf("different members of one archive conflict: %v", err)
	}
	for _, changed := range []string{
		strings.Replace(second, `source_size_bytes=4096`, `source_size_bytes=4097`, 1),
		strings.Replace(second, `archive_format="zip"`, `archive_format="tar"`, 1),
		strings.Replace(second, `archive_member="second.raw"`, `archive_member="first.qcow2"`, 1),
		strings.Replace(strings.Replace(second, `archive_member="second.raw"`, `archive_member="first.qcow2"`, 1), `source_format="raw"`, `source_format="qcow2"`, 1),
	} {
		if _, err := parseCloudTest(t, withImages(first+changed)); err == nil {
			t.Fatal("conflicting shared archive settings accepted")
		}
	}
}

func TestShippedManagedCatalogMatchesVerifiedProvenance(t *testing.T) {
	// Go package tests run in config/, while packaged Windows test binaries
	// run beside vmango.dist.conf in the extracted runtime directory.
	root := ".."
	if _, err := os.Stat(filepath.Join(root, "vmango.dist.conf")); os.IsNotExist(err) {
		root = "."
	}
	data, err := os.ReadFile(filepath.Join(root, "vmango.dist.conf"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "REPLACE_WITH_RANDOM_SECRET", "4e3068f439dc1b9a8f63cb61ab758150e01354a259bfb48346c04ab72233a5d1")
	text = strings.ReplaceAll(text, "REPLACE_WITH_SEPARATE_RANDOM_SECRET", "8af3d4c90e7b5a261f06081349a7b26fe110934bab138fcae53d906de871c5ab")
	path := filepath.Join(t.TempDir(), "dist.conf")
	if err = os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	images, _, err := cfg.ImageDefinitions()
	if err != nil {
		t.Fatal(err)
	}
	var verified []struct {
		ID        string `json:"id"`
		URL       string `json:"url"`
		SHA256    string `json:"sha256"`
		Size      uint64 `json:"size_bytes"`
		Capacity  uint64 `json:"virtual_size_bytes"`
		Features  uint64 `json:"incompatible_features"`
		Publisher string `json:"publisher_checksums"`
	}
	data, err = os.ReadFile(filepath.Join(root, "docs", "data", "image-sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &verified); err != nil {
		t.Fatal(err)
	}
	if len(images) != 12 || len(verified) != len(images) {
		t.Fatalf("expected six families in both architectures: %d/%d", len(images), len(verified))
	}
	families := map[string]map[compute.Arch]bool{}
	for _, i := range images {
		profile, err := hardware.DecodePolicy(i.BackendPolicy)
		if err != nil {
			t.Fatal(err)
		}
		if !i.Managed() || i.SourcePath != "" || i.Download.Format != compute.VolumeFormatQcow2 || profile.ConfigDrive.Format != "nocloud" {
			t.Fatalf("incomplete shipped definition: %+v", i)
		}
		found := false
		for _, v := range verified {
			if v.ID == i.ID {
				found = true
				if i.Download.URL != v.URL || i.Download.SHA256 != v.SHA256 || v.Size == 0 || v.Capacity == 0 || v.Features != 0 || !strings.HasPrefix(v.Publisher, "https://") {
					t.Fatalf("provenance differs for %s", i.ID)
				}
			}
		}
		if !found {
			t.Fatalf("missing provenance: %s", i.ID)
		}
		if families[i.OSName] == nil {
			families[i.OSName] = map[compute.Arch]bool{}
		}
		families[i.OSName][i.Arch] = true
	}
	if len(families) != 6 {
		t.Fatal(families)
	}
	for name, arches := range families {
		if !arches[compute.ArchAmd64] || !arches[compute.ArchAarch64] {
			t.Fatal("missing architecture", name)
		}
	}
}

func TestConflictingSharedArtifactDefinitions(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, other := range []string{`source_format="raw" source_size_bytes=4096`, `source_format="qcow2" source_size_bytes=8192`} {
		text := `image "first" { hardware_profile="default" os_arch="x86_64" url="https://example.org/one" sha256="` + hash + `" source_format="qcow2" source_size_bytes=4096 } image "second" { hardware_profile="default" os_arch="x86_64" url="https://example.org/two" sha256="` + hash + `" ` + other + ` }`
		path := filepath.Join(t.TempDir(), "conflict.conf")
		os.WriteFile(path, []byte(withImages(text)), 0600)
		if _, err := Parse(path); err == nil {
			t.Fatal("conflicting artifact definitions accepted")
		}
	}
}

func TestPrivateGraphicsListenerValidation(t *testing.T) {
	for _, address := range []string{"", "127.0.0.1", "0.0.0.0", "::1", "::", "not-an-ip", "127.0.0.1:5900"} {
		path := filepath.Join(t.TempDir(), "listener.conf")
		os.WriteFile(path, []byte(strings.Replace(cloudConfig, `uri="qemu:///session"`, `uri="qemu:///session" graphics_listen="`+address+`"`, 1)), 0600)
		_, err := Parse(path)
		if err == nil || !strings.Contains(err.Error(), "graphics_listen is unsupported") {
			t.Fatalf("removed binding %q must fail: %v", address, err)
		}
	}
}

func withImages(definitions string) string {
	return strings.Replace(cloudConfig, `image "linux" { hardware_profile="default" path="/source" os_arch="x86_64" }`, definitions, 1)
}

func TestCloudImagesAndPoliciesRemainIndependent(t *testing.T) {
	first := strings.Replace(cloudConfig, `path="/source"`, `path="/source" protected=true hidden=true`, 1)
	second := strings.ReplaceAll(cloudConfig, `cloud "test"`, `cloud "other"`)
	second = strings.ReplaceAll(second, `host "a"`, `host "other-a"`)
	second = strings.ReplaceAll(second, `host "b"`, `host "other-b"`)
	second = strings.ReplaceAll(second, `qemu:///session`, `qemu:///other`)
	second = strings.ReplaceAll(second, `qemu+ssh://b/system`, `qemu+ssh://other/system`)
	cfg, err := parseCloudTest(t, first+second)
	if err != nil {
		t.Fatal(err)
	}
	images, policies, err := cfg.ImageDefinitions()
	if err != nil || len(images) != 2 || images[0].ID != images[1].ID || images[0].CloudID == images[1].CloudID {
		t.Fatal(images, err)
	}
	if !policies[compute.VolumePolicyKey("a", "/source")].Hidden || policies[compute.VolumePolicyKey("other-a", "/source")].Hidden || policies["/source"].Protected {
		t.Fatal("image policies escaped their cloud", policies)
	}
	if _, err := parseCloudTest(t, strings.Replace(cloudConfig, `image "linux" { hardware_profile="default" path="/source" os_arch="x86_64" }`, `image "linux" { hardware_profile="default" path="/one" os_arch="x86_64" } image "linux" { hardware_profile="default" path="/two" os_arch="x86_64" }`, 1)); err == nil {
		t.Fatal("duplicate cloud image accepted")
	}
}

func TestRemovedImageSyntaxHasMigrationGuidance(t *testing.T) {
	for _, old := range []string{`image "/old/path" { hardware_profile="default" os_arch="x86_64" }`, `image "/policy-only" { hardware_profile="default" protected=true }`, strings.Replace(cloudConfig, `image "linux" { hardware_profile="default" path="/source" os_arch="x86_64" }`, `images=[]`, 1)} {
		_, err := parseCloudTest(t, old)
		if err == nil || !strings.Contains(err.Error(), "removed") || !strings.Contains(err.Error(), "docs/CLOUDS.md") {
			t.Fatalf("missing migration guidance: %v", err)
		}
	}
}
