package compute_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/configdrive"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
)

// The stored payload is deliberately reduced to the pre-cloud-image v2 shape.
// Recovery must use accepted bytes/defaults and the original lock identities,
// even when today's same-ID catalog entry points to different source content.
func TestPreCloudImageCommandRecoversPinnedSourceAndRawLocks(t *testing.T) {
	data := []byte(strings.Repeat("old-accepted-bytes", 4096))
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		_, _ = w.Write(data)
	}))
	defer source.Close()
	managed, backend, _, image := imageSetup(t, source.URL, data)
	image.CloudID = "accepted-cloud"
	image.ID = "/pre-change/image-id"
	image.Defaults.Initialization = "none"
	image.Defaults.Firmware = "bios"
	image.Defaults.Graphics = "none"
	image.Defaults.VideoModel = "none"
	image.Defaults.DiskBus = compute.DeviceBusVirtio
	disabled := false
	image.Defaults.GuestAgent = &disabled
	catalog := &mutableImageCatalog{image}
	images := compute.NewImageService(catalog)
	images.ManagedSources = managed
	images.NodeClouds = map[string]string{"one": "accepted-cloud"}
	machines := &managedMachines{machines: machines{caps: compute.VirtualMachineCapabilities{
		Arch: compute.ArchAmd64, MaxVCPUs: 8, Firmwares: []string{"bios", "efi"},
		DiskBuses: []compute.DeviceBus{compute.DeviceBusVirtio}, Graphics: []compute.GraphicType{compute.GraphicTypeNone, compute.GraphicTypeVnc}, VideoModels: []compute.VideoModel{compute.VideoModelNone},
	}}, saved: map[string]*compute.VirtualMachine{}}
	volumeBackend := &managedVolumes{backend: backend}
	volumes := compute.NewVolumeService(volumeBackend)
	vms := compute.NewVirtualMachineService(machines)
	manager := compute.NewVirtualMachineManager(vms, volumes, noImageEvents{}, map[string]compute.VirtualMachineManagerNodeSettings{"one": {CdFormat: configdrive.FormatNoCloud, CdPool: "accepted-init", CdSuffix: "_config.iso"}}, images)
	handler := &compute.CommandHandler{Manager: manager, Volumes: volumes, VMs: vms, Scopes: map[string]string{"one": "canonical-backend"}}
	dir := filepath.Join(t.TempDir(), "tasks")
	repo, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	service := tasks.New(repo, handler, zerolog.Nop())
	vm := &compute.VirtualMachine{Id: "legacy-pinned", Name: "legacy-pinned", NodeId: "one", Arch: compute.ArchAmd64, VCpus: 1, Memory: compute.NewSize(512, compute.SizeUnitM), Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone, Metadata: &compute.MachineMetadata{Cloud: "accepted-cloud"}}
	command := compute.NewCommand("machine.create", compute.InfrastructureCommand{CloudID: "accepted-cloud", Node: "one", VM: vm.Id, Machine: vm, Clones: []compute.VirtualMachineManagerClonedVolumeParams{{ImageID: image.ID, NewPool: "vm-disks", NewName: "root"}}})
	accepted, err := service.Submit(tasks.Identity{ID: "operator"}, "pre-cloud-image-task", command)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Update(func(store *tasks.Store) error {
		record := store.Records[accepted.ID]
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(record.Payload, &payload); err != nil {
			return err
		}
		var pinned []map[string]json.RawMessage
		if err := json.Unmarshal(payload["Images"], &pinned); err != nil {
			return err
		}
		for _, image := range pinned {
			delete(image, "CloudID")
			delete(image, "ConfiguredPolicy")
		}
		payload["Images"], _ = json.Marshal(pinned)
		record.Payload, _ = json.Marshal(payload)
		store.Records[accepted.ID] = record
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	original := before.Records[accepted.ID]
	if len(original.Locks) == 0 || original.Locks[0].Scope != "accepted-cloud" {
		t.Fatal("missing accepted cloud resource", original.Locks)
	}
	rawLock := false
	for _, lock := range original.Locks {
		if lock.Kind == "volume" && lock.Scope == "canonical-backend" && strings.HasPrefix(lock.ID, "/") {
			rawLock = true
		}
	}
	if !rawLock {
		t.Fatal("fixture did not contain canonical raw-path lock", original.Locks)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	catalog.image.CloudID = "different-cloud"
	catalog.image.Download = &compute.ImageDownload{URL: "https://invalid.example/new-bytes", SHA256: strings.Repeat("0", 64), Format: compute.VolumeFormatQcow2}
	catalog.image.Defaults.Firmware = "efi"
	repo, err = filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	recovered, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recovered.Records[accepted.ID], original) {
		t.Fatal("restart rewrote durable command or safety records")
	}
	service = tasks.New(repo, handler, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); service.Wait() }()
	if err := service.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	result := waitImage(t, service, accepted.ID)
	if result.State != tasks.Succeeded || result.OutcomeUncertain || requests.Load() != 1 || len(volumeBackend.clones) != 1 || machines.saved[vm.Id].Firmware != "bios" {
		t.Fatal("legacy command did not execute accepted source/defaults", result)
	}
	after, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	saved := after.Records[accepted.ID]
	if !reflect.DeepEqual(saved.Locks, original.Locks) || !reflect.DeepEqual(saved.Task.Resources, original.Task.Resources) || len(saved.Task.Stages) == 0 || len(saved.Task.Effects) == 0 {
		t.Fatal("legacy references, locks or history were lost", saved.Task)
	}
	binding, err := managed.Bind("one", image)
	if err != nil {
		t.Fatal(err)
	}
	if state, _ := managed.Status(binding, image); state != "ready" {
		t.Fatal("accepted artifact identity was not retained", state)
	}
}
