package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func installFixture(t *testing.T, content string) *InstallDocument {
	t.Helper()
	p := filepath.Join(t.TempDir(), "vmango.conf")
	if err := os.WriteFile(p, []byte(content), 0640); err != nil {
		t.Fatal(err)
	}
	d, err := OpenInstallDocument(p)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func installerHost() InstallHost {
	return InstallHost{Cloud: "test", Zone: "zone", Node: "a", URI: "qemu:///session", Network: "default", NetworkID: "lan", Pool: "nvme", StorageClass: "fast"}
}

func TestInstallerPreservesExistingNoOpAndSecrets(t *testing.T) {
	source := "# Header stays exactly as typed\n" + cloudConfig + `api {
 listen="127.0.0.1:8080" # keep this comment
 base_url="http://localhost:8080"
 user "owner" { hashed_password="private-existing-hash" }
 oidc { client_secret="private-oidc-secret" }
}
custom "future" { value="retain me" }
`
	d := installFixture(t, source)
	c, err := d.Prepare(InstallEdits{Hosts: []InstallHost{installerHost()}})
	if err != nil {
		t.Fatal(err)
	}
	if c.Changed || !bytes.Equal(c.Bytes(), []byte(source)) {
		t.Fatalf("no-op changed file: %s", c.Bytes())
	}
	backup, err := d.Publish(c)
	if err != nil || backup != "" {
		t.Fatal(backup, err)
	}
	if len(c.Summary) != 0 {
		t.Fatal(c.Summary)
	}
	files, _ := filepath.Glob(d.Path() + ".backup-*")
	if len(files) != 0 {
		t.Fatal(files)
	}
}

func TestInstallerBoundedEditPreservesCommentsAndUnselectedHosts(t *testing.T) {
	source := strings.Replace(cloudConfig, `network="default"`, `network="default" /* keep network comment */`, 1) + "\n# final comment\n"
	d := installFixture(t, source)
	host := installerHost()
	host.Network = "new-nat"
	c, err := d.Prepare(InstallEdits{Hosts: []InstallHost{host}})
	if err != nil {
		t.Fatal(err)
	}
	expected := strings.Replace(source, `network="default"`, `network="new-nat"`, 1)
	if string(c.Bytes()) != expected {
		t.Fatalf("changed unrelated bytes:\n%s", c.Bytes())
	}
	if len(c.Config().Hosts()) != 2 {
		t.Fatal("removed unselected host")
	}
	backup, err := d.Publish(c)
	if err != nil {
		t.Fatal(err)
	}
	previous, err := os.ReadFile(backup)
	if err != nil || string(previous) != source {
		t.Fatal(err, string(previous))
	}
	info, err := os.Stat(d.Path())
	if err != nil || info.Mode().Perm() != 0640 {
		t.Fatal(info, err)
	}
	next, err := OpenInstallDocument(d.Path())
	if err != nil {
		t.Fatal(err)
	}
	again, err := next.Prepare(InstallEdits{Hosts: []InstallHost{host}})
	if err != nil || again.Changed {
		t.Fatal(again, err)
	}
	backup2, err := next.Publish(again)
	if err != nil || backup2 != "" {
		t.Fatal(backup2, err)
	}
}

func TestInstallerNewConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.conf")
	d, err := OpenInstallDocument(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Exists() {
		t.Fatal("missing file exists")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("installer-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	a := &InstallAPI{Listen: "127.0.0.1:8080", BaseURL: "http://localhost:8080", AdminUser: "admin", PasswordHash: string(hash)}
	h := installerHost()
	h.Node = "new-host"
	h.URI = "qemu+ssh://operator@host/system"
	c, err := d.Prepare(InstallEdits{Hosts: []InstallHost{h}, API: a})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("preparation created config")
	}
	cfg := c.Config()
	if len(cfg.Namespaces) != 1 || len(cfg.Clouds) != 1 || len(cfg.Clouds[0].Images) != 2 || len(cfg.Clouds[0].InstanceTypes) != 1 {
		t.Fatalf("incomplete initial configuration: %+v", cfg)
	}
	profile := cfg.Clouds[0].InstanceTypes[0]
	if profile.ID != "small" || profile.CPU != 2 || profile.MemoryMiB != 2048 || profile.BootDiskGiB != 8 {
		t.Fatalf("incorrect first-run profile: %+v", profile)
	}
	for _, image := range cfg.Clouds[0].Images {
		if image.OsArch == "x86_64" && (image.ID != "ubuntu-24.04-x86_64" || image.Firmware != "auto") {
			t.Fatalf("x86 installer image must use host-default firmware: %+v", image)
		}
		if image.OsArch == "aarch64" && image.Firmware != "efi" {
			t.Fatalf("AArch64 installer image must retain EFI firmware: %+v", image)
		}
	}
	for _, expected := range []string{"2 vCPU, 2048 MiB RAM, 8 GiB boot disk", "Ubuntu 24.04 / 20261017", "x86_64 with host-default firmware", "aarch64 UEFI", "Administrator credentials changed: true"} {
		if !strings.Contains(strings.Join(c.Summary, "\n"), expected) {
			t.Fatalf("review summary omits %q", expected)
		}
	}
	if strings.Contains(strings.Join(c.Summary, " "), string(hash)) {
		t.Fatal("summary exposed password hash")
	}
	if _, err := d.Publish(c); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal(info.Mode())
	}
}

func TestInstallerConflictAndCandidateIdentity(t *testing.T) {
	for _, kind := range []string{"external edit", "appeared", "tampered digest", "different document", "mode changed"} {
		t.Run(kind, func(t *testing.T) {
			d := installFixture(t, cloudConfig)
			h := installerHost()
			h.Network = "edited"
			c, err := d.Prepare(InstallEdits{Hosts: []InstallHost{h}})
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "external edit":
				os.WriteFile(d.Path(), []byte(cloudConfig+"\n# external edit\n"), 0640)
			case "appeared":
				d.exists = false
			case "tampered digest":
				c.Digest = "changed"
			case "different document":
				other, err := OpenInstallDocument(d.Path())
				if err != nil {
					t.Fatal(err)
				}
				d = other
			case "mode changed":
				os.Chmod(d.Path(), 0600)
			}
			before, _ := os.ReadFile(d.Path())
			if _, err := d.Publish(c); err == nil {
				t.Fatal("accepted conflicting publication")
			}
			after, _ := os.ReadFile(d.Path())
			if !bytes.Equal(before, after) {
				t.Fatal("conflict modified destination")
			}
		})
	}
}

func TestInstallerRejectsInvalidAmbiguousAndUnsafeFiles(t *testing.T) {
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.conf")
	os.WriteFile(invalid, []byte("api {"), 0600)
	if _, err := OpenInstallDocument(invalid); err == nil {
		t.Fatal("invalid config accepted")
	}
	original, _ := os.ReadFile(invalid)
	if string(original) != "api {" {
		t.Fatal("invalid config replaced")
	}
	target := filepath.Join(dir, "target.conf")
	os.WriteFile(target, []byte(cloudConfig), 0600)
	link := filepath.Join(dir, "linked.conf")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenInstallDocument(link); err == nil {
		t.Fatal("symlink accepted")
	}
	hard := filepath.Join(dir, "hard.conf")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenInstallDocument(target); err == nil {
		t.Fatal("hardlink accepted")
	}
	d := installFixture(t, cloudConfig+"\napi {listen=\"127.0.0.1:8080\"}\napi {listen=\"127.0.0.1:8080\"}\n")
	if _, err := d.Prepare(InstallEdits{API: &InstallAPI{Listen: "127.0.0.1:9090", BaseURL: "http://localhost:9090"}}); err == nil {
		t.Fatal("duplicate api blocks accepted")
	}
}

func TestInstallerRejectsIdentityMovesAndBridgeConversion(t *testing.T) {
	for _, mutation := range []string{"uri", "cloud", "zone", "bridge"} {
		t.Run(mutation, func(t *testing.T) {
			source := cloudConfig
			if mutation == "bridge" {
				source = strings.Replace(source, `network="default"`, `bridge="br0"`, 1)
			}
			d := installFixture(t, source)
			h := installerHost()
			switch mutation {
			case "uri":
				h.URI = "qemu:///system"
			case "cloud":
				h.Cloud = "new"
			case "zone":
				h.Zone = "new"
			}
			if _, err := d.Prepare(InstallEdits{Hosts: []InstallHost{h}}); err == nil {
				t.Fatal("unsupported change accepted")
			}
		})
	}
}

func TestInstallerAPIEditPreservesCredentials(t *testing.T) {
	source := cloudConfig + `api { listen="127.0.0.1:8080" base_url="http://localhost:8080" user "admin" { hashed_password="existing-private-hash" } oidc {client_secret="existing-private-secret"} }`
	d := installFixture(t, source)
	a := &InstallAPI{Listen: "127.0.0.1:9090", BaseURL: "https://vmango.example"}
	c, err := d.Prepare(InstallEdits{API: a})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c.Bytes()), "existing-private-hash") || !strings.Contains(string(c.Bytes()), "existing-private-secret") {
		t.Fatal("credentials changed")
	}
	if strings.Contains(strings.Join(c.Summary, " "), "existing-private") {
		t.Fatal("credentials exposed")
	}
	for _, expected := range []string{`API listener: "127.0.0.1:8080" -> "127.0.0.1:9090"`, `API public URL: "http://localhost:8080" -> "https://vmango.example"`, "Administrator credentials changed: false"} {
		if !strings.Contains(strings.Join(c.Summary, "\n"), expected) {
			t.Fatalf("review summary omits %q", expected)
		}
	}
	if c.Config().Clouds[0].InstanceTypes[0].BootDiskGiB != 20 {
		t.Fatal("existing instance profile changed during API edit")
	}
	a.Listen = "0.0.0.0:8080"
	a.BaseURL = "http://public.example"
	if _, err := d.Prepare(InstallEdits{API: a}); err == nil {
		t.Fatal("insecure public access accepted")
	}
}

func TestInstallerMissingParentAndReadOnly(t *testing.T) {
	if _, err := OpenInstallDocument(filepath.Join(t.TempDir(), "missing", "config")); err == nil {
		t.Fatal("missing parent accepted")
	}
	if os.Geteuid() == 0 {
		t.Skip("permission checks require a non-root user")
	}
	d := installFixture(t, cloudConfig)
	if err := os.Chmod(d.Path(), 0400); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenInstallDocument(d.Path()); err == nil {
		t.Fatal("read-only config accepted")
	}
}

func TestInstallerCandidateCopiesAndParserHasNoSideEffects(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "missing", "signing-key")
	cfg, err := ParseBytes([]byte(`api {jwt_signing_key_file=` + strconvQuote(key) + `}`))
	if err != nil || cfg.API.JWTSigningKey != "" {
		t.Fatal(cfg, err)
	}
	if _, err := os.Stat(filepath.Dir(key)); !os.IsNotExist(err) {
		t.Fatal("parser created credential directory")
	}
	d := installFixture(t, cloudConfig)
	c, err := d.Prepare(InstallEdits{})
	if err != nil {
		t.Fatal(err)
	}
	bytes := c.Bytes()
	bytes[0] = 'x'
	if string(c.Bytes()) != cloudConfig {
		t.Fatal("candidate bytes are mutable through accessor")
	}
}
func strconvQuote(s string) string { return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"` }

func TestInstallerPreservesImplicitExistingLogicalBindings(t *testing.T) {
	d := installFixture(t, strings.ReplaceAll(cloudConfig, `network "lan"`, `network "frontend"`))
	host := installerHost()
	host.NetworkID = ""
	host.StorageClass = ""
	candidate, err := d.Prepare(InstallEdits{Hosts: []InstallHost{host}})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Changed || !bytes.Equal(candidate.Bytes(), d.Bytes()) {
		t.Fatalf("introduced implicit catalog changes: %s", candidate.Bytes())
	}
	host.Network = "another-network"
	host.Pool = "another-pool"
	candidate, err = d.Prepare(InstallEdits{Hosts: []InstallHost{host}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(candidate.Bytes())
	if strings.Contains(text, `network "lan"`) || strings.Contains(text, `storage_class "standard"`) {
		t.Fatal("new logical IDs introduced")
	}
	if !strings.Contains(text, `network="another-network"`) || !strings.Contains(text, `pool="another-pool"`) {
		t.Fatal("selected binding unchanged")
	}
}

func TestInstallerAPIReviewTLSPathsAndURLRedaction(t *testing.T) {
	source := cloudConfig + `api { listen="127.0.0.1:8080" base_url="https://old.example" tls_cert_file="/certs/old.pem" tls_key_file="/keys/old.pem" user "admin" {hashed_password="unchanged-secret-hash"} }`
	d := installFixture(t, source)
	c, err := d.Prepare(InstallEdits{API: &InstallAPI{Listen: "127.0.0.1:8080", BaseURL: "https://new.example", TLSCertFile: "/certs/new.pem", TLSKeyFile: "/keys/new.pem"}})
	if err != nil {
		t.Fatal(err)
	}
	summary := strings.Join(c.Summary, "\n")
	for _, expected := range []string{`TLS certificate path (controller): "/certs/old.pem" -> "/certs/new.pem"`, `TLS private-key path (controller): "/keys/old.pem" -> "/keys/new.pem"`, "Administrator credentials changed: false"} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("review summary omits %q", expected)
		}
	}
	if strings.Contains(summary, "unchanged-secret-hash") {
		t.Fatal("review leaks hash")
	}
	source = cloudConfig + `api {listen="127.0.0.1:8080" base_url="https://private-user:private-password@old.example/base?private-query=token#private-fragment"}`
	d = installFixture(t, source)
	c, err = d.Prepare(InstallEdits{API: &InstallAPI{Listen: "127.0.0.1:8080", BaseURL: "https://new.example"}})
	if err != nil {
		t.Fatal(err)
	}
	summary = strings.Join(c.Summary, "\n")
	if strings.Contains(summary, "private-") || strings.Contains(summary, "token") {
		t.Fatalf("URL review leaks secret fields: %s", summary)
	}
	if !strings.Contains(summary, `"https://old.example/base" -> "https://new.example"`) {
		t.Fatal(summary)
	}
}
