package compute_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"subuk/vmango/catalog"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
)

func installArchiveTemplateImage(t *testing.T, handler *compute.CommandHandler, image compute.Image) *compute.ImageService {
	t.Helper()
	repo, err := catalog.NewImageRepository([]compute.Image{image})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := filesystem.NewImageArtifactRepository(filepath.Join(t.TempDir(), "images"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { artifacts.Close() })
	images := compute.NewImageService(repo)
	images.ManagedSources = compute.NewManagedImageService(&imageBackend{volumes: map[string]*compute.Volume{}, fingerprint: "identity"}, artifacts, map[string]compute.ImageBinding{
		"a": {Scope: "backend:a", Pool: "pool"},
		"b": {Scope: "backend:b", Pool: "pool"},
	}, compute.ImageTransferLimits{MaxBytes: 4 << 20, Timeout: time.Second, IdleTimeout: time.Second})
	manager := compute.NewVirtualMachineManager(handler.VMs, handler.Volumes, cloudEvents{}, map[string]compute.VirtualMachineManagerNodeSettings{
		"a": {CdPool: "pool", CdSuffix: "_config.iso"},
		"b": {CdPool: "pool", CdSuffix: "_config.iso"},
	}, images)
	manager.Clouds = handler.Clouds
	handler.Manager = manager
	handler.Clouds.Manager = manager
	return images
}

func archivedTemplateImage(initialization string) compute.Image {
	return compute.Image{CloudID: "c", ID: "image", Arch: compute.ArchAmd64,
		Defaults: compute.ImageDefaults{Initialization: initialization, Graphics: "spice", VideoModel: "qxl"},
		Download: &compute.ImageDownload{URL: "https://example.invalid/linux.zip", SHA256: strings.Repeat("a", 64), Format: compute.VolumeFormatRaw, SizeBytes: 4096, ArchiveFormat: "zip", ArchiveMember: "images/system.raw", ExtractedSizeBytes: 1024},
	}
}

func TestArchivedImageAndCloudInitTemplateRemainPinnedTogether(t *testing.T) {
	handler, _, _, cloud := cloudFixtureInitialization(t, 4, nil, "nocloud")
	originalImage := archivedTemplateImage("nocloud")
	installArchiveTemplateImage(t, handler, originalImage)
	templates := templateFixture(t)
	handler.CloudInitTemplates = templates
	originalData := "#cloud-config\nwrite_files:\n  - path: /etc/archive-test\n    content: accepted configuration\n"
	template, err := templates.Create(compute.CloudInitTemplateInput{Name: "Archived Linux", UserData: originalData})
	if err != nil {
		t.Fatal(err)
	}
	spec := createSpec("archived-template")
	spec.UserDataTemplate = &compute.CloudInitTemplateRef{ID: template.ID, Revision: template.Revision}
	command := compute.NewCommand("machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: spec})
	dir := filepath.Join(t.TempDir(), "tasks")
	repo, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { repo.Close() }()
	service := tasks.New(repo, handler, zerolog.Nop())
	identity := tasks.Identity{ID: "admin"}
	accepted, err := service.Submit(identity, "archived-template", command)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := templates.Update(template.ID, template.Revision, compute.CloudInitTemplateInput{Name: "Changed", UserData: "#!/bin/sh\necho changed\n"})
	if err != nil {
		t.Fatal(err)
	}
	if err = templates.Delete(edited.ID, edited.Revision); err != nil {
		t.Fatal(err)
	}
	if err = repo.Close(); err != nil {
		t.Fatal(err)
	}

	// Reconstruct services with a different archive member and initialization
	// policy. The accepted template no longer exists in the template catalog.
	restarted, _, _, _ := cloudFixtureInitialization(t, 4, nil, "none")
	restarted.CloudInitTemplates = templates
	replacement := archivedTemplateImage("none")
	replacement.Download.ArchiveMember = "replacement/system.raw"
	replacement.Download.SHA256 = strings.Repeat("b", 64)
	currentImages := installArchiveTemplateImage(t, restarted, replacement)
	repo, err = filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	service = tasks.New(repo, restarted, zerolog.Nop())
	replayed, err := service.Submit(identity, "archived-template", command)
	if err != nil || replayed.ID != accepted.ID {
		t.Fatalf("accepted replay consulted changed catalogs: %+v %v", replayed, err)
	}
	state, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	var snapshot compute.InfrastructureCommand
	if err = json.Unmarshal(state.Records[accepted.ID].Payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.CreateSpec.UserDataTemplate != nil || snapshot.CreateSpec.UserData != originalData || len(snapshot.Images) != 1 || *snapshot.Images[0].Download != *originalImage.Download {
		t.Fatal("archive identity and resolved template were not preserved together")
	}
	inventory, err := restarted.Clouds.Inventory(cloud)
	if err != nil {
		t.Fatal(err)
	}
	pinned := currentImages.ForCloud("c").Snapshot(snapshot.Images, nil, true)
	resolved, err := restarted.Clouds.ResolveCreate(*snapshot.Catalog, *snapshot.CreateSpec, pinned, inventory, snapshot.CreationBindings)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Machine.Config == nil || resolved.Machine.Config.Initialization != "nocloud" || string(resolved.Machine.Config.Userdata) != originalData {
		t.Fatal("archived image scheduling lost the accepted cloud-init document")
	}
	binding, err := pinned.ManagedSources.Bind(resolved.Node, originalImage)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Clones) != 1 || resolved.Clones[0].OriginalPath != binding.Path {
		t.Fatal("scheduling selected the replacement archive member")
	}
}

func TestArchivedAndroidRejectsCloudInitTemplates(t *testing.T) {
	handler, _, _, _ := cloudFixtureInitialization(t, 4, nil, "none")
	image := archivedTemplateImage("none")
	image.OSName = "Android"
	image.Arch = compute.ArchAarch64
	image.DataDisks = []compute.ImageDataDisk{{Name: "userdata", SizeBytes: 16 << 30}}
	installArchiveTemplateImage(t, handler, image)
	templates := templateFixture(t)
	handler.CloudInitTemplates = templates
	template, err := templates.Create(compute.CloudInitTemplateInput{Name: "Setup", UserData: "#cloud-config\npackages: [curl]\n"})
	if err != nil {
		t.Fatal(err)
	}
	spec := createSpec("android-template")
	spec.UserDataTemplate = &compute.CloudInitTemplateRef{ID: template.ID, Revision: template.Revision}
	_, err = handler.Prepare(compute.NewCommand("machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: spec}))
	var validation *compute.ValidationError
	if !errors.Is(err, compute.ErrInvalid) || !errors.As(err, &validation) || validation.Field != "user_data" {
		t.Fatalf("archived Android accepted cloud-init: %v", err)
	}
}
