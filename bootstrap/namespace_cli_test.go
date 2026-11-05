package bootstrap

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/ssh"
)

func migrationTestConfig(t *testing.T) (string, string, string, string) {
	t.Helper()
	root := t.TempDir()
	cfgPath := filepath.Join(root, "vmango.conf")
	namespaceDir := filepath.Join(root, "namespaces")
	taskDir := filepath.Join(root, "tasks")
	legacyDir := filepath.Join(root, "templates")
	text := fmt.Sprintf(`namespace_directory=%q
key_file=%q
cloud_init_templates_dir=%q
tasks { directory=%q }
namespace "alpha" { clouds=["local"] }
cloud "local" {
 image "linux" { path="/unused.img" os_arch="x86_64" }
 zone "z" {}
 instance_type "small" { cpu=1 memory_mib=256 boot_disk_gib=1 }
 network "lan" {}
 storage_class "standard" {}
 host "node" { uri="qemu:///not-used-by-catalog-migration" zone="z" network "lan" {network="default"} storage_class "standard" {pool="default"} }
}
`, namespaceDir, filepath.Join(root, "keys"), legacyDir, taskDir)
	if err := os.WriteFile(cfgPath, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return cfgPath, namespaceDir, taskDir, legacyDir
}
func TestNamespaceCatalogMigrationDryRunApplyResume(t *testing.T) {
	cfg, namespaceDir, _, legacyDir := migrationTestConfig(t)
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	keyFile := filepath.Join(filepath.Dir(cfg), "keys")
	if err := os.WriteFile(keyFile, ssh.MarshalAuthorizedKey(key), 0600); err != nil {
		t.Fatal(err)
	}
	legacy, err := filesystem.NewCloudInitTemplateRepository(legacyDir)
	if err != nil {
		t.Fatal(err)
	}
	template, err := compute.NewCloudInitTemplateService(legacy).Create(compute.CloudInitTemplateInput{Name: "Initial", UserData: "#cloud-config\npassword: private-secret"})
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()
	mapping := NamespaceMigration{Version: 1, Catalogs: []NamespaceCatalogMigration{{Namespace: "alpha", Keys: []string{ssh.FingerprintLegacyMD5(key)}, Templates: []compute.CloudInitTemplateRef{{ID: template.ID, Revision: template.Revision}}}}}
	mappingPath := filepath.Join(filepath.Dir(cfg), "mapping.json")
	data, _ := json.Marshal(mapping)
	if err := os.WriteFile(mappingPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := NamespaceMigrate(cfg, mappingPath, "", false, &output); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(namespaceDir, "catalogs", "catalogs.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("dry-run wrote catalog", err)
	}
	backup := filepath.Join(filepath.Dir(cfg), "backup")
	if err := NamespaceMigrate(cfg, mappingPath, backup, true, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "private-secret") {
		t.Fatal("CLI disclosed template content")
	}
	if err := NamespaceMigrate(cfg, mappingPath, backup, true, &output); err != nil {
		t.Fatal("repeat apply failed", err)
	}
	catalog, err := filesystem.NewNamespaceCatalogRepository(filepath.Join(namespaceDir, "catalogs"))
	if err != nil {
		t.Fatal(err)
	}
	view, _ := catalog.Templates("alpha")
	got, err := view.Get(template.ID)
	if err != nil || got.Revision != template.Revision || got.UserData != template.UserData {
		t.Fatal("migration changed template", got, err)
	}
	catalog.Close()
	for _, name := range []string{"manifest.json", "effects.json", "legacy-templates/" + template.ID + ".json", "legacy-keys", "vmango.conf"} {
		if err := filesystem.CheckPrivateBackupPath(filepath.Join(backup, name), false); err != nil {
			t.Fatal("missing or unsafe backup", name, err)
		}
	}
	if data, err := os.ReadFile(keyFile); err != nil || !bytes.Equal(data, ssh.MarshalAuthorizedKey(key)) {
		t.Fatal("legacy keys changed", err)
	}
	mapping.Catalogs[0].Keys = nil
	data, _ = json.Marshal(mapping)
	os.WriteFile(mappingPath, data, 0600)
	if err := NamespaceMigrate(cfg, mappingPath, backup, true, &output); err == nil {
		t.Fatal("different mapping reused backup")
	}
}
func TestNamespaceTaskRecoveryWithoutNamespaceOrBackend(t *testing.T) {
	cfg, _, taskDir, _ := migrationTestConfig(t)
	// Recovery does not need any namespaces/clouds to remain configured.
	if err := os.WriteFile(cfg, []byte(fmt.Sprintf("tasks { directory=%q }\n", taskDir)), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := filesystem.NewTaskRepository(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	err = repo.Update(func(store *tasks.Store) error {
		store.Records[id] = tasks.Record{Task: tasks.Task{ID: id, State: tasks.Queued, CreatedAt: time.Now(), BlocksResources: true}, KeyHash: "key", Fingerprint: "fingerprint", Locks: []tasks.ResourceRef{{Kind: "machine", Scope: "node", ID: "vm"}}, Payload: json.RawMessage(`{"private":"must-not-be-printed"}`)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.Close()
	var output bytes.Buffer
	if err := NamespaceTaskRecovery(cfg, "inspect", id, "", "", &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "must-not-be-printed") {
		t.Fatal("task inspect leaked private command")
	}
	if err := NamespaceTaskRecovery(cfg, "cancel", id, "maintainer", "obsolete legacy work", &output); err != nil {
		t.Fatal(err)
	}
	repo, err = filesystem.NewTaskRepository(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	task, err := tasks.New(repo, nil, zerolog.Nop()).Get(id)
	if err != nil || task.State != tasks.Cancelled || len(task.Transitions) == 0 || !strings.Contains(task.Transitions[len(task.Transitions)-1].Reason, "operator:maintainer") {
		t.Fatal("cancel audit missing", task, err)
	}
}
func TestMigrationBackupRejectsSymlinkAndIncompleteDirectory(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	os.WriteFile(source, []byte("private"), 0600)
	link := filepath.Join(root, "link")
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	if err := copyMigrationPath(link, filepath.Join(root, "copy")); err == nil {
		t.Fatal("symlink backup accepted")
	}
	if err := filesystem.WritePrivateBackupFile(link, []byte("replacement")); err == nil {
		t.Fatal("symlink target overwritten")
	}
	data, _ := os.ReadFile(source)
	if string(data) != "private" {
		t.Fatal("source changed")
	}
}
