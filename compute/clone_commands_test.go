package compute_test

import (
	"encoding/json"
	"github.com/rs/zerolog"
	"path/filepath"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"testing"
)

type capturedCloneBackend struct {
	*cloudVolumeBackend
	identity string
	clones   []compute.VolumeCloneParams
}

func (r *capturedCloneBackend) Get(path, node string) (*compute.Volume, error) {
	v, err := r.cloudVolumeBackend.Get(path, node)
	if err == nil && path == "/source" && r.identity != "" {
		v.Identity = r.identity
	}
	return v, err
}
func (r *capturedCloneBackend) Clone(p compute.VolumeCloneParams) (*compute.Volume, error) {
	r.mu.Lock()
	r.clones = append(r.clones, p)
	r.mu.Unlock()
	return r.cloudVolumeBackend.Clone(p)
}
func TestAcceptedCloneModeSurvivesConfigChangeAndRestart(t *testing.T) {
	for _, kind := range []string{"volume.clone", "machine.create"} {
		t.Run(kind, func(t *testing.T) {
			h, _, raw, _ := cloudFixtureHandler(t, 4, nil)
			backend := &capturedCloneBackend{cloudVolumeBackend: raw, identity: "original-source"}
			h.Volumes.VolumeRepository = backend
			cloud := &h.Clouds.Definitions[0]
			cloud.StorageClasses[0].CloneMode = compute.CloneModeOverlay
			for i := range cloud.Hosts {
				b := cloud.Hosts[i].Storage["fast"]
				b.Format = compute.VolumeFormatQcow2
				cloud.Hosts[i].Storage["fast"] = b
			}
			dir := filepath.Join(t.TempDir(), "tasks")
			repo, err := filesystem.NewTaskRepository(dir)
			if err != nil {
				t.Fatal(err)
			}
			service := tasks.New(repo, h, zerolog.Nop())
			command := compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("dependent")}
			if kind == "volume.clone" {
				command = compute.InfrastructureCommand{CloudID: "c", Node: "a", Path: "/source", StorageClass: "fast", CloneVolume: &compute.VolumeCloneParams{NodeId: "a", OriginalPath: "/source", NewName: "dependent", NewSize: compute.NewSize(1, compute.SizeUnitG)}}
			}
			task := submitCloud(t, service, "accepted-clone-policy", kind, command)
			repo.Close()
			cloud.StorageClasses[0].CloneMode = compute.CloneModeCopy
			reopened, err := filesystem.NewTaskRepository(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { reopened.Close() })
			resumed := tasks.New(reopened, h, zerolog.Nop())
			runCloudService(t, resumed)
			result := awaitCloudTask(t, resumed, task.ID)
			if result.State != tasks.Succeeded {
				t.Fatalf("task failed: %+v", result)
			}
			backend.mu.Lock()
			defer backend.mu.Unlock()
			if len(backend.clones) != 1 || backend.clones[0].CloneMode != compute.CloneModeOverlay || backend.clones[0].SourceIdentity != "original-source" {
				t.Fatalf("accepted clone changed: %+v", backend.clones)
			}
		})
	}
}
func TestQueuedImageRejectsChangedSourceIdentity(t *testing.T) {
	h, _, raw, _ := cloudFixtureHandler(t, 4, nil)
	backend := &capturedCloneBackend{cloudVolumeBackend: raw, identity: "accepted"}
	h.Volumes.VolumeRepository = backend
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := tasks.New(repo, h, zerolog.Nop())
	task := submitCloud(t, service, "changed-image-source", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("changed")})
	backend.identity = "replacement"
	runCloudService(t, service)
	result := awaitCloudTask(t, service, task.ID)
	if result.State != tasks.Failed || result.OutcomeUncertain || raw.created != 0 {
		t.Fatalf("replaced source used: %+v", result)
	}
}
func TestAcceptedCloneLocksSourceAndBackingBase(t *testing.T) {
	backend := &overlayLockBackend{cloudVolumeBackend: &cloudVolumeBackend{}}
	h := &compute.CommandHandler{Volumes: compute.NewVolumeService(backend)}
	command, err := h.Prepare(compute.NewCommand("volume.clone", compute.InfrastructureCommand{Node: "a", CloneVolume: &compute.VolumeCloneParams{NodeId: "a", OriginalPath: "/source", NewName: "independent", NewPool: "pool", Format: compute.VolumeFormatQcow2}}))
	if err != nil {
		t.Fatal(err)
	}
	var p compute.InfrastructureCommand
	if err = json.Unmarshal(command.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.CloneVolume.SourceIdentity != "a/source" || p.CloneVolume.CloneMode != compute.CloneModeCopy {
		t.Fatal("source was not pinned")
	}
	source, base := false, false
	for _, ref := range command.Resources {
		if ref.ID == "/source" && !ref.ReadOnly {
			source = true
		}
		if ref.ID == "/base" && ref.ReadOnly {
			base = true
		}
	}
	if !source || !base {
		t.Fatalf("missing source locks: %+v", command.Resources)
	}
}

type overlayLockBackend struct{ *cloudVolumeBackend }

func (r *overlayLockBackend) Get(path, node string) (*compute.Volume, error) {
	v, err := r.cloudVolumeBackend.Get(path, node)
	v.BackingStore = &compute.VolumeBackingStore{Path: "/base", Format: compute.VolumeFormatRaw}
	return v, err
}

func TestQueuedOverlayCopyExcludesBaseMutationUntilCancelled(t *testing.T) {
	backend := &overlayLockBackend{cloudVolumeBackend: &cloudVolumeBackend{}}
	handler := &compute.CommandHandler{Volumes: compute.NewVolumeService(backend)}
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := tasks.New(repo, handler, zerolog.Nop())
	task := submitCloud(t, service, "copy-overlay-lock", "volume.clone", compute.InfrastructureCommand{Node: "a", CloneVolume: &compute.VolumeCloneParams{NodeId: "a", OriginalPath: "/source", NewName: "independent", NewPool: "pool", Format: compute.VolumeFormatQcow2}})
	for _, kind := range []string{"volume.delete", "volume.resize"} {
		if _, err := service.Submit(tasks.Identity{ID: "admin"}, "conflicting-"+kind, compute.NewCommand(kind, compute.InfrastructureCommand{Node: "a", Path: "/base", Size: compute.NewSize(2, compute.SizeUnitG)})); err == nil {
			t.Fatal("base mutation admitted while a dependent source is being copied", kind)
		}
	}
	if _, err := service.Cancel(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Submit(tasks.Identity{ID: "admin"}, "base-after-cancel", compute.NewCommand("volume.delete", compute.InfrastructureCommand{Node: "a", Path: "/base"})); err != nil {
		t.Fatal("cancel retained source lock", err)
	}
}
