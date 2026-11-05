package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/config"
	"subuk/vmango/filesystem"
	"subuk/vmango/libvirt"
	"subuk/vmango/tasks"
	"subuk/vmango/util"
	"time"

	"github.com/rs/zerolog"
)

// NamespaceMigration describes explicit operator choices. It is deliberately
// independent of names, creators and placement-based inference.
type NamespaceMigration struct {
	Version   int                          `json:"version"`
	Resources []NamespaceResourceMigration `json:"resources"`
	Catalogs  []NamespaceCatalogMigration  `json:"catalogs"`
}
type NamespaceResourceMigration struct {
	Namespace compute.NamespaceID    `json:"namespace"`
	Cloud     string                 `json:"cloud"`
	Machines  []string               `json:"machines"`
	Volumes   []compute.LocalDiskRef `json:"volumes"`
}
type NamespaceCatalogMigration struct {
	Namespace compute.NamespaceID            `json:"namespace"`
	Keys      []string                       `json:"keys"`
	Templates []compute.CloudInitTemplateRef `json:"templates"`
}
type migrationCatalogSnapshot struct {
	Namespace compute.NamespaceID         `json:"namespace"`
	Keys      []*compute.Key              `json:"keys"`
	Templates []compute.CloudInitTemplate `json:"templates"`
}
type migrationManifest struct {
	Version           int                        `json:"version"`
	MappingHash       string                     `json:"mapping_hash"`
	ConfigurationHash string                     `json:"configuration_hash"`
	Plans             []compute.AssignmentPlan   `json:"plans"`
	Catalogs          []migrationCatalogSnapshot `json:"catalogs"`
	Completed         bool                       `json:"completed"`
}
type migrationEffect struct {
	At        time.Time           `json:"at"`
	Namespace compute.NamespaceID `json:"namespace"`
	Cloud     string              `json:"cloud,omitempty"`
	Resource  tasks.ResourceRef   `json:"resource"`
	Action    string              `json:"action"`
}

type namespaceRuntime struct {
	Workload       *compute.WorkloadService
	Tasks          *tasks.Service
	TaskRepository *filesystem.TaskRepository
	Catalogs       *filesystem.NamespaceCatalogRepository
	Namespaces     *compute.NamespaceService
	VMRepository   *libvirt.VirtualMachineRepository
	closers        []io.Closer
}

func (r *namespaceRuntime) Close() error {
	var result error
	for i := len(r.closers) - 1; i >= 0; i-- {
		result = errors.Join(result, r.closers[i].Close())
	}
	return result
}
func newNamespaceRuntime(cfg *config.Config) (_ *namespaceRuntime, err error) {
	r := &namespaceRuntime{}
	defer func() {
		if err != nil {
			_ = r.Close()
		}
	}()
	definitions, err := cfg.NamespaceDefinitions()
	if err != nil {
		return nil, err
	}
	r.Namespaces, err = compute.NewNamespaceService(definitions)
	if err != nil {
		return nil, err
	}
	clouds, err := cfg.CloudDefinitions()
	if err != nil {
		return nil, err
	}
	_, policies, err := cfg.ImageDefinitions()
	if err != nil {
		return nil, err
	}
	// Acquiring the task lock first excludes the running server and all other
	// offline tools. The executor is never started by migration or recovery.
	r.TaskRepository, err = filesystem.NewTaskRepository(util.ExpandHomeDir(cfg.Tasks.Directory))
	if err != nil {
		return nil, err
	}
	r.closers = append(r.closers, r.TaskRepository)
	r.Tasks = tasks.New(r.TaskRepository, nil, zerolog.Nop())
	ownership, err := filesystem.NewVolumeOwnershipRepository(util.ExpandHomeDir(cfg.Tasks.Directory) + "-volumes")
	if err != nil {
		return nil, err
	}
	r.closers = append(r.closers, ownership)
	assignments, err := filesystem.NewVolumeAssignmentRepository(filepath.Join(util.ExpandHomeDir(cfg.NamespaceDirectory), "volumes"))
	if err != nil {
		return nil, err
	}
	r.closers = append(r.closers, assignments)
	r.Catalogs, err = filesystem.NewNamespaceCatalogRepository(filepath.Join(util.ExpandHomeDir(cfg.NamespaceDirectory), "catalogs"))
	if err != nil {
		return nil, err
	}
	r.closers = append(r.closers, r.Catalogs)
	uris := map[string]string{}
	scopes := map[string]string{}
	order := []string{}
	settings := map[string]libvirt.NodeSettings{}
	for _, host := range cfg.Hosts() {
		uris[host.Node] = host.Uri
		order = append(order, host.Node)
		hash := sha256.Sum256([]byte(host.Uri))
		scopes[host.Node] = "libvirt:" + hex.EncodeToString(hash[:])
		settings[host.Node] = libvirt.NodeSettings{CdSuffix: host.ConfigDriveSuffix, Emulator: host.Emulator, GraphicsListen: host.GraphicsListen, QcowPreallocMetadata: host.QcowPreallocMetadata, Cache: host.Cache}
	}
	pool := libvirt.NewConnectionPool(uris, order, zerolog.Nop())
	r.VMRepository = libvirt.NewVirtualMachineRepository(pool, settings, zerolog.Nop())
	vms := compute.NewVirtualMachineService(r.VMRepository)
	volumes := compute.NewVolumeService(libvirt.NewVolumeRepository(pool, settings, policies, zerolog.Nop()))
	volumes.Ownership = ownership
	cloudService := &compute.CloudService{Definitions: clouds, VMs: vms, Volumes: volumes}
	r.Workload = &compute.WorkloadService{Namespaces: r.Namespaces, Clouds: cloudService, Assignments: assignments, Scopes: scopes}
	return r, nil
}

func readMigrationJSON(path string, target any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("invalid migration document: %w", err)
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("migration document has trailing content")
	}
	return nil
}
func migrationHash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func prepareMigration(r *namespaceRuntime, cfg *config.Config, mapping NamespaceMigration) (migrationManifest, error) {
	m := migrationManifest{Version: 1, Plans: []compute.AssignmentPlan{}, Catalogs: []migrationCatalogSnapshot{}}
	if mapping.Version != 1 {
		return m, errors.New("unsupported migration mapping version")
	}
	if len(mapping.Resources) == 0 && len(mapping.Catalogs) == 0 {
		return m, errors.New("migration mapping is empty")
	}
	selected := map[string]compute.NamespaceID{}
	// Validate all resources sharing a destination as one graph, so labels in
	// separate mapping entries cannot collide only after the first apply.
	groups := []NamespaceResourceMigration{}
	indexes := map[string]int{}
	for _, entry := range mapping.Resources {
		key := string(entry.Namespace) + "\x00" + entry.Cloud
		index, ok := indexes[key]
		if !ok {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, NamespaceResourceMigration{Namespace: entry.Namespace, Cloud: entry.Cloud})
		}
		groups[index].Machines = append(groups[index].Machines, entry.Machines...)
		groups[index].Volumes = append(groups[index].Volumes, entry.Volumes...)
	}
	for _, entry := range groups {
		if len(entry.Machines) == 0 && len(entry.Volumes) == 0 {
			return m, errors.New("resource mapping is empty")
		}
		p, err := r.Workload.PreviewAssignment(compute.WorkloadContext{Namespace: entry.Namespace, Cloud: entry.Cloud}, entry.Machines, entry.Volumes)
		if err != nil {
			return m, err
		}
		refs := []tasks.ResourceRef{}
		for _, vm := range p.Machines {
			refs = append(refs, compute.VMRef(vm.NodeId, vm.Id))
		}
		for _, v := range p.Volumes {
			refs = append(refs, compute.VolumeRef(v.NodeId, v.Path))
		}
		for _, ref := range refs {
			key := ref.Kind + "\x00" + ref.Scope + "\x00" + ref.ID
			if previous, ok := selected[key]; ok && previous != entry.Namespace {
				return m, errors.New("resource selected for multiple namespaces")
			}
			selected[key] = entry.Namespace
		}
		if err := checkMigrationLocks(r, refs); err != nil {
			return m, err
		}
		m.Plans = append(m.Plans, p)
	}
	var legacyKeys compute.KeyRepository
	var legacyTemplates *filesystem.CloudInitTemplateRepository
	defer func() {
		if legacyTemplates != nil {
			_ = legacyTemplates.Close()
		}
	}()
	for _, entry := range mapping.Catalogs {
		if _, err := r.Namespaces.Get(entry.Namespace); err != nil {
			return m, err
		}
		snapshot := migrationCatalogSnapshot{Namespace: entry.Namespace, Keys: []*compute.Key{}, Templates: []compute.CloudInitTemplate{}}
		if len(entry.Keys) > 0 && legacyKeys == nil {
			path := util.ExpandHomeDir(cfg.KeyFile)
			if _, err := os.Stat(path); err != nil {
				return m, err
			}
			var err error
			legacyKeys, err = filesystem.NewKeyRepository(path, zerolog.Nop())
			if err != nil {
				return m, err
			}
		}
		if len(entry.Templates) > 0 && legacyTemplates == nil {
			path := util.ExpandHomeDir(cfg.CloudInitTemplatesDir)
			if _, err := os.Stat(path); err != nil {
				return m, err
			}
			var err error
			legacyTemplates, err = filesystem.NewCloudInitTemplateRepository(path)
			if err != nil {
				return m, err
			}
		}
		for _, fingerprint := range entry.Keys {
			key, err := legacyKeys.Get(fingerprint)
			if err != nil {
				return m, err
			}
			snapshot.Keys = append(snapshot.Keys, key)
		}
		for _, ref := range entry.Templates {
			t, err := legacyTemplates.Get(ref.ID)
			if err != nil {
				return m, err
			}
			if t.Revision != ref.Revision {
				return m, fmt.Errorf("%w: legacy template revision changed", compute.ErrConflict)
			}
			snapshot.Templates = append(snapshot.Templates, t)
		}
		if err := r.Catalogs.Import(entry.Namespace, snapshot.Keys, snapshot.Templates, false); err != nil {
			return m, err
		}
		m.Catalogs = append(m.Catalogs, snapshot)
	}
	return m, nil
}
func checkMigrationLocks(r *namespaceRuntime, refs []tasks.ResourceRef) error {
	store, err := r.TaskRepository.Read()
	if err != nil {
		return err
	}
	for _, rec := range store.Records {
		if !rec.Task.BlocksResources {
			continue
		}
		for _, lock := range rec.Locks {
			for _, ref := range refs {
				canonical := r.Workload.Scopes[ref.Scope]
				if lock.Kind == ref.Kind && lock.ID == ref.ID && (lock.Scope == ref.Scope || lock.Scope == canonical) {
					return fmt.Errorf("%w: resource is blocked by task %s; inspect and recover it before migration", compute.ErrConflict, rec.Task.ID)
				}
			}
		}
	}
	return nil
}

// NamespaceMigrate validates the complete mapping before effects. Apply requires
// a durable private backup; reusing it resumes the exact accepted snapshots.
func NamespaceMigrate(configPath, mappingPath, backupDir string, apply bool, out io.Writer) error {
	cfg, err := config.Parse(configPath)
	if err != nil {
		return err
	}
	mappingBytes, err := os.ReadFile(mappingPath)
	if err != nil {
		return err
	}
	configuration, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var mapping NamespaceMigration
	if err := readMigrationJSON(mappingPath, &mapping); err != nil {
		return err
	}
	r, err := newNamespaceRuntime(cfg)
	if err != nil {
		return err
	}
	defer r.Close()
	manifest := migrationManifest{}
	resume := false
	if apply {
		if backupDir == "" || !filepath.IsAbs(backupDir) {
			return errors.New("apply requires an absolute --backup directory")
		}
		if _, err := os.Lstat(filepath.Join(backupDir, "manifest.json")); err == nil {
			if err := filesystem.CheckPrivateBackupPath(backupDir, true); err != nil {
				return err
			}
			if err := readMigrationJSON(filepath.Join(backupDir, "manifest.json"), &manifest); err != nil {
				return err
			}
			if manifest.Version != 1 || manifest.MappingHash != migrationHash(mappingBytes) || manifest.ConfigurationHash != migrationHash(configuration) {
				return errors.New("backup belongs to a different mapping or configuration")
			}
			resume = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if !resume {
		manifest, err = prepareMigration(r, cfg, mapping)
		if err != nil {
			return err
		}
		manifest.MappingHash = migrationHash(mappingBytes)
		manifest.ConfigurationHash = migrationHash(configuration)
	}
	fmt.Fprintf(out, "Validated %d resource graphs and %d catalog selections.\n", len(manifest.Plans), len(manifest.Catalogs))
	for _, p := range manifest.Plans {
		fmt.Fprintf(out, "Namespace %s, cloud %s: %d machines, %d volumes.\n", p.Context.Namespace, p.Context.Cloud, len(p.Machines), len(p.Volumes))
	}
	if !apply {
		fmt.Fprintln(out, "Dry-run complete; no resources or catalog entries assigned.")
		return nil
	}
	if !resume {
		if err := backupMigration(backupDir, configPath, mappingPath, cfg, r, manifest); err != nil {
			return err
		}
	}
	// Revalidate every destination before the first effect, including resumes.
	for _, p := range manifest.Plans {
		ids := []string{}
		disks := []compute.LocalDiskRef{}
		for _, vm := range p.Machines {
			ids = append(ids, vm.Id)
		}
		for _, v := range p.Volumes {
			disks = append(disks, compute.LocalDiskRef{Host: v.NodeId, Path: v.Path})
		}
		if _, err := r.Workload.PreviewAssignment(p.Context, ids, disks); err != nil {
			return err
		}
	}
	for _, p := range manifest.Plans {
		refs := []tasks.ResourceRef{}
		for _, vm := range p.Machines {
			refs = append(refs, compute.VMRef(vm.NodeId, vm.Id))
		}
		for _, v := range p.Volumes {
			refs = append(refs, compute.VolumeRef(v.NodeId, v.Path))
		}
		if err := checkMigrationLocks(r, refs); err != nil {
			return err
		}
		if err := r.Workload.ApplyAssignmentWithEffects(p, func(ref tasks.ResourceRef, action string) error {
			return appendMigrationEffect(backupDir, migrationEffect{At: time.Now().UTC(), Namespace: p.Context.Namespace, Cloud: p.Context.Cloud, Resource: ref, Action: action})
		}); err != nil {
			return fmt.Errorf("migration partially applied; retain backup %s and resume after resolving the error: %w", backupDir, err)
		}
	}
	for _, c := range manifest.Catalogs {
		if _, err := r.Namespaces.Get(c.Namespace); err != nil {
			return err
		}
		if err := r.Catalogs.Import(c.Namespace, c.Keys, c.Templates, true); err != nil {
			return fmt.Errorf("catalog migration incomplete; retain backup %s: %w", backupDir, err)
		}
		if err := appendMigrationEffect(backupDir, migrationEffect{At: time.Now().UTC(), Namespace: c.Namespace, Action: "catalog-imported"}); err != nil {
			return err
		}
	}
	manifest.Completed = true
	if err := writeMigrationJSON(filepath.Join(backupDir, "manifest.json"), manifest); err != nil {
		return err
	}
	fmt.Fprintf(out, "Migration complete. Retained backup and effects: %s\n", backupDir)
	return nil
}

func writeMigrationJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return filesystem.WritePrivateBackupFile(path, append(data, '\n'))
}
func copyMigrationPath(source, destination string) error {
	info, err := os.Lstat(source)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("migration backup refuses symlink sources")
	}
	if info.IsDir() {
		if err := filesystem.CreatePrivateBackupDirectory(destination); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Name() == ".lock" || strings.HasPrefix(entry.Name(), ".pending-") {
				continue
			}
			if err := copyMigrationPath(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				return err
			}
		}
		return filesystem.SyncPrivateBackupDirectory(destination)
	}
	if !info.Mode().IsRegular() {
		return errors.New("migration backup refuses special files")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return filesystem.WritePrivateBackupFile(destination, data)
}
func backupMigration(dir, configPath, mappingPath string, cfg *config.Config, r *namespaceRuntime, manifest migrationManifest) error {
	sources := map[string]string{"namespace-store": util.ExpandHomeDir(cfg.NamespaceDirectory), "tasks": util.ExpandHomeDir(cfg.Tasks.Directory), "volume-ownership": util.ExpandHomeDir(cfg.Tasks.Directory) + "-volumes", "legacy-templates": util.ExpandHomeDir(cfg.CloudInitTemplatesDir), "legacy-keys": util.ExpandHomeDir(cfg.KeyFile)}
	for _, source := range sources {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(absolute, dir)
		if err == nil && (relative == "." || (!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != "..")) {
			return errors.New("backup directory must be outside application stores")
		}
	}
	if err := filesystem.CreatePrivateBackupDirectory(dir); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if err := filesystem.CheckPrivateBackupPath(dir, true); err != nil {
			return err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return errors.New("new migration backup directory must be empty; use another directory after an interrupted backup")
		}
	}
	if err := copyMigrationPath(configPath, filepath.Join(dir, "vmango.conf")); err != nil {
		return err
	}
	if err := copyMigrationPath(mappingPath, filepath.Join(dir, "mapping.json")); err != nil {
		return err
	}
	for name, source := range sources {
		if err := copyMigrationPath(source, filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	xmlDir := filepath.Join(dir, "domains")
	if err := filesystem.CreatePrivateBackupDirectory(xmlDir); err != nil {
		return err
	}
	for _, p := range manifest.Plans {
		for _, vm := range p.Machines {
			xml, err := r.VMRepository.NamespaceBackupXML(vm.Id, vm.NodeId)
			if err != nil {
				return err
			}
			name := migrationHash([]byte(vm.NodeId+"\x00"+vm.Id)) + ".xml"
			if err := filesystem.WritePrivateBackupFile(filepath.Join(xmlDir, name), []byte(xml)); err != nil {
				return err
			}
		}
	}
	// The manifest is the commit marker for a complete, durable backup. A
	// partial backup has no manifest and can never be used for apply/resume.
	if err := filesystem.SyncPrivateBackupDirectory(dir); err != nil {
		return err
	}
	return writeMigrationJSON(filepath.Join(dir, "manifest.json"), manifest)
}
func appendMigrationEffect(dir string, effect migrationEffect) error {
	path := filepath.Join(dir, "effects.json")
	effects := []migrationEffect{}
	if _, err := os.Lstat(path); err == nil {
		if err := readMigrationJSON(path, &effects); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	effects = append(effects, effect)
	return writeMigrationJSON(path, effects)
}

// NamespaceTaskRecovery uses the existing task service without starting an
// executor. Recovery remains available when namespace/cloud definitions vanish.
func NamespaceTaskRecovery(configPath, action, id, operator, reason string, out io.Writer) error {
	cfg, err := config.Parse(configPath)
	if err != nil {
		return err
	}
	repo, err := filesystem.NewTaskRepository(util.ExpandHomeDir(cfg.Tasks.Directory))
	if err != nil {
		return err
	}
	defer repo.Close()
	service := tasks.New(repo, nil, zerolog.Nop())
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if action == "inspect" {
		if id != "" {
			task, err := service.Get(id)
			if err != nil {
				return err
			}
			return encoder.Encode(task)
		}
		before := ""
		for {
			page, err := service.List(tasks.Filter{Before: before, Limit: 100})
			if err != nil {
				return err
			}
			for _, task := range page.Items {
				if err := encoder.Encode(task); err != nil {
					return err
				}
			}
			if page.Next == "" {
				return nil
			}
			before = page.Next
		}
	}
	if id == "" || strings.TrimSpace(operator) == "" || len(strings.TrimSpace(reason)) < 3 {
		return errors.New("task mutation requires --id, --operator and --reason (at least 3 characters)")
	}
	if action != "cancel" && action != "release" {
		return errors.New("task action must be inspect, cancel or release")
	}
	if err := service.RecoverInterrupted(); err != nil {
		return err
	}
	who := tasks.Identity{ID: "operator:" + operator, Name: operator}
	var task tasks.Task
	switch action {
	case "cancel":
		task, err = service.CancelWithReason(id, who, reason)
	case "release":
		task, err = service.Release(id, who, reason)
	default:
		return errors.New("task action must be inspect, cancel or release")
	}
	if err != nil {
		return err
	}
	return encoder.Encode(task)
}
