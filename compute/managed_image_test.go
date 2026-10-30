package compute_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/configdrive"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type imageBackend struct {
	mu                        sync.Mutex
	volumes                   map[string]*compute.Volume
	creates, uploads, deletes int
	bytes                     atomic.Uint64
	readNotify                chan<- struct{}
	uploadFail                bool
	fingerprint               string
}

func (b *imageBackend) BindImage(node, scope, pool, name string, size uint64) (compute.ImageBinding, error) {
	return compute.ImageBinding{Node: node, Scope: scope, Pool: pool, PoolID: pool + "-uuid", Name: name, Path: "/" + pool + "/" + name}, nil
}
func (b *imageBackend) InspectImage(binding compute.ImageBinding) (*compute.Volume, string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	v, ok := b.volumes[binding.Node+binding.Path]
	if !ok {
		return nil, "", compute.ErrNotFound
	}
	copy := *v
	return &copy, b.fingerprint, nil
}
func (b *imageBackend) CreateImage(binding compute.ImageBinding, size uint64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := binding.Node + binding.Path
	if b.volumes[key] != nil {
		return compute.ErrConflict
	}
	b.creates++
	b.volumes[key] = &compute.Volume{NodeId: binding.Node, Path: binding.Path, Pool: binding.Pool, Name: binding.Name, Format: compute.VolumeFormatRaw, Size: compute.NewSize(size, compute.SizeUnitB)}
	return nil
}
func (b *imageBackend) UploadImage(ctx context.Context, binding compute.ImageBinding, r io.Reader, size uint64) error {
	b.mu.Lock()
	b.uploads++
	fail := b.uploadFail
	b.mu.Unlock()
	buf := make([]byte, 4096)
	notified := false
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := r.Read(buf)
		b.bytes.Add(uint64(n))
		if n > 0 && !notified && b.readNotify != nil {
			notified = true
			select {
			case b.readNotify <- struct{}{}:
			default:
			}
		}
		time.Sleep(time.Millisecond)
		if fail {
			return errors.New("simulated upload failure")
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
func (b *imageBackend) DeleteImage(binding compute.ImageBinding) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.volumes, binding.Node+binding.Path)
	b.deletes++
	return nil
}

type imageCommand struct {
	Image   compute.Image
	Binding compute.ImageBinding
}
type imageHandler struct{ s *compute.ManagedImageService }

func (h imageHandler) Prepare(c tasks.Command) (tasks.Command, error) { return c, nil }
func (h imageHandler) Execute(ctx context.Context, c tasks.Command, e *tasks.Execution) error {
	var p imageCommand
	if err := json.Unmarshal(c.Payload, &p); err != nil {
		return err
	}
	return h.s.Ensure(ctx, p.Binding, p.Image, e)
}
func imageSetup(t *testing.T, url string, data []byte) (*compute.ManagedImageService, *imageBackend, *filesystem.ImageArtifactRepository, compute.Image) {
	t.Helper()
	repo, err := filesystem.NewImageArtifactRepository(filepath.Join(t.TempDir(), "artifacts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	backend := &imageBackend{volumes: map[string]*compute.Volume{}, fingerprint: "backend-inode-and-timestamps"}
	service := compute.NewManagedImageService(backend, repo, map[string]compute.ImageBinding{"one": {Scope: "backend:one", Pool: "images-one"}, "two": {Scope: "backend:two", Pool: "images-two"}}, compute.ImageTransferLimits{MaxBytes: 4 << 20, Timeout: 5 * time.Second, IdleTimeout: time.Second})
	h := sha256.Sum256(data)
	image := compute.Image{ID: "image", Arch: compute.ArchAmd64, Download: &compute.ImageDownload{URL: url, SHA256: hex.EncodeToString(h[:]), Format: compute.VolumeFormatRaw}}
	return service, backend, repo, image
}
func imageTasks(t *testing.T, service *compute.ManagedImageService) (*tasks.Service, func()) {
	t.Helper()
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	s := tasks.New(repo, imageHandler{service}, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	if err = s.Start(ctx, 2); err != nil {
		t.Fatal(err)
	}
	stop := func() { cancel(); s.Wait(); repo.Close() }
	t.Cleanup(stop)
	return s, stop
}
func submitImage(t *testing.T, s *tasks.Service, images *compute.ManagedImageService, i compute.Image, node, key string) tasks.Task {
	t.Helper()
	b, err := images.Bind(node, i)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(imageCommand{i, b})
	task, err := s.Submit(tasks.Identity{ID: "admin"}, key, tasks.Command{Kind: "machine.create", Payload: payload, Resources: []tasks.ResourceRef{compute.VMRef(node, key)}})
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func waitImage(t *testing.T, s *tasks.Service, id string) tasks.Task {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		task, err := s.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if task.State.Terminal() {
			return task
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("image task did not finish")
	return tasks.Task{}
}
func TestImageStreamingSharingAndNodeIsolation(t *testing.T) {
	data := []byte(strings.Repeat("diskbytes", 32768))
	var requests atomic.Int32
	uploadRead := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(data[:4096])
		w.(http.Flusher).Flush()
		// Withhold the rest until the backend consumes the prefix. This proves
		// streaming without assuming the worker starts before a fixed delay.
		select {
		case <-uploadRead:
			w.Write(data[4096:])
		case <-r.Context().Done():
			t.Error("upstream request ended before the upload consumed its prefix")
		}
	}))
	defer server.Close()
	images, backend, repo, i := imageSetup(t, server.URL, data)
	backend.readNotify = uploadRead
	binding, _ := images.Bind("one", i)
	state, _ := images.Status(binding, i)
	if state != "downloadable" || requests.Load() != 0 {
		t.Fatal("discovery initiated download", state)
	}
	s, _ := imageTasks(t, images)
	first := submitImage(t, s, images, i, "one", "first-create")
	secondImage := i
	secondImage.ID = "different-defaults"
	secondImage.Defaults.Firmware = "efi"
	second := submitImage(t, s, images, secondImage, "one", "second-create")
	for _, task := range []tasks.Task{first, second} {
		result := waitImage(t, s, task.ID)
		if result.State != tasks.Succeeded {
			t.Fatalf("%+v", result)
		}
	}
	if requests.Load() != 1 || backend.creates != 1 || backend.uploads != 1 {
		t.Fatal("duplicate materialization", requests.Load(), backend.creates, backend.uploads)
	}
	// Recreate the service to prove readiness depends on durable verification.
	reopened := compute.NewManagedImageService(backend, repo, map[string]compute.ImageBinding{"one": {Scope: "backend:one", Pool: "images-one"}}, compute.ImageTransferLimits{MaxBytes: 4 << 20, Timeout: time.Second, IdleTimeout: time.Second})
	if state, _ = reopened.Status(binding, i); state != "ready" {
		t.Fatal("verified metadata did not survive restart", state)
	}
	reused := submitImage(t, s, images, i, "one", "third-create")
	if result := waitImage(t, s, reused.ID); result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	isolated := submitImage(t, s, images, i, "two", "other-node-create")
	if result := waitImage(t, s, isolated.ID); result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	if requests.Load() != 2 || backend.creates != 2 {
		t.Fatal("incorrect node isolation", requests.Load(), backend.creates)
	}
	result, _ := s.Get(first.ID)
	var transfer *tasks.TransferProgress
	for _, stage := range result.Stages {
		if stage.Name == "stream-image" {
			transfer = stage.Transfer
		}
	}
	if transfer == nil || transfer.Bytes != uint64(len(data)) || transfer.BytesPerSecond <= 0 || transfer.RemainingSeconds == nil {
		t.Fatal("transfer telemetry missing", transfer)
	}
	wrongSize := i
	copyDownload := *i.Download
	copyDownload.SizeBytes = uint64(len(data) + 1)
	wrongSize.Download = &copyDownload
	if state, _ = images.Status(binding, wrongSize); state != "failed" {
		t.Fatal("incompatible byte-size definition reused shared artifact")
	}
	backend.mu.Lock()
	backend.fingerprint = "unrelated replacement"
	backend.mu.Unlock()
	if state, _ = images.Status(binding, i); state != "failed" {
		t.Fatal("replaced volume was adopted")
	}
}
func TestImageTransferFailuresNeverBecomeReady(t *testing.T) {
	for _, failure := range []string{"checksum", "truncated", "oversized", "missing-size", "configured-size", "wrong-http-size", "upload", "http", "unsupported"} {
		t.Run(failure, func(t *testing.T) {
			data := []byte(strings.Repeat("raw", 1000))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if failure == "http" {
					w.WriteHeader(503)
					return
				}
				if failure == "wrong-http-size" {
					w.Header().Set("Content-Length", "3500")
				} else if failure == "missing-size" || failure == "configured-size" || failure == "truncated" || failure == "oversized" {
					w.(http.Flusher).Flush()
				} else {
					w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				}
				w.Write(data)
			}))
			defer server.Close()
			images, backend, _, i := imageSetup(t, server.URL, data)
			switch failure {
			case "checksum":
				i.Download.SHA256 = strings.Repeat("0", 64)
			case "truncated":
				i.Download.SizeBytes = 4000
			case "oversized":
				i.Download.SizeBytes = 2000
			case "configured-size":
				i.Download.SizeBytes = uint64(len(data))
			case "upload":
				backend.uploadFail = true
			case "unsupported":
				i.Download.Format = compute.VolumeFormatQcow2
			}
			s, _ := imageTasks(t, images)
			task := submitImage(t, s, images, i, "one", "failure-create")
			result := waitImage(t, s, task.ID)
			if failure == "configured-size" {
				if result.State != tasks.Succeeded {
					t.Fatal(result)
				}
				return
			}
			if result.State != tasks.Failed {
				t.Fatalf("unexpected state: %+v", result)
			}
			b, _ := images.Bind("one", i)
			if state, _ := images.Status(b, i); state == "ready" {
				t.Fatal("failed image published")
			}
			if backend.creates > 0 && backend.deletes != 1 {
				t.Fatal("owned staging not cleaned", backend.deletes)
			}
			if failure == "missing-size" && !strings.Contains(result.Error.Message, "source_size_bytes") {
				t.Fatal(result.Error)
			}
		})
	}
}
func TestUnrelatedImageDestinationIsNotAdopted(t *testing.T) {
	images, backend, _, i := imageSetup(t, "https://example.invalid/image", []byte("raw"))
	b, _ := images.Bind("one", i)
	backend.CreateImage(b, 3)
	s, _ := imageTasks(t, images)
	task := submitImage(t, s, images, i, "one", "existing-destination")
	result := waitImage(t, s, task.ID)
	if result.State != tasks.Failed || backend.uploads != 0 || backend.deletes != 0 {
		t.Fatal("unrelated disk was modified", result)
	}
}

func TestInterruptedImageRemainsUnverifiedAfterRestart(t *testing.T) {
	data := []byte(strings.Repeat("disk", 65536))
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(data[:4096])
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-release:
			w.Write(data[4096:])
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	images, backend, artifacts, i := imageSetup(t, server.URL, data)
	taskDir := filepath.Join(t.TempDir(), "tasks")
	repo, err := filesystem.NewTaskRepository(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	service := tasks.New(repo, imageHandler{images}, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	if err = service.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	task := submitImage(t, service, images, i, "one", "interrupt-create")
	<-started
	deadline := time.Now().Add(time.Second)
	for backend.bytes.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	service.Wait()
	result, _ := service.Get(task.ID)
	repo.Close()
	if result.State != tasks.Interrupted || backend.creates != 1 || backend.deletes != 0 {
		t.Fatalf("unexpected shutdown effects: %+v create=%d delete=%d", result, backend.creates, backend.deletes)
	}
	reopened, err := filesystem.NewTaskRepository(taskDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restartedImages := compute.NewManagedImageService(backend, artifacts, map[string]compute.ImageBinding{"one": {Scope: "backend:one", Pool: "images-one"}}, compute.ImageTransferLimits{MaxBytes: 4 << 20, Timeout: time.Second, IdleTimeout: time.Second})
	b, _ := restartedImages.Bind("one", i)
	if state, _ := restartedImages.Status(b, i); state != "failed" {
		t.Fatal("interrupted image became available", state)
	}
	restarted := tasks.New(reopened, imageHandler{restartedImages}, zerolog.Nop())
	afterCtx, afterCancel := context.WithCancel(context.Background())
	if err = restarted.Start(afterCtx, 1); err != nil {
		t.Fatal(err)
	}
	afterCancel()
	restarted.Wait()
	if backend.uploads != 1 {
		t.Fatal("interrupted transfer replayed")
	}
	close(release)
}

type managedVolumes struct {
	compute.VolumeRepository
	backend *imageBackend
	mu      sync.Mutex
	clones  []compute.VolumeCloneParams
}

func (v *managedVolumes) ResolvePath(node, pool, name string) (string, error) {
	return "/" + pool + "/" + name, nil
}
func (v *managedVolumes) ValidateCreate(p compute.VolumeCreateParams) error { return nil }
func (v *managedVolumes) Get(path, node string) (*compute.Volume, error) {
	v.backend.mu.Lock()
	defer v.backend.mu.Unlock()
	volume := v.backend.volumes[node+path]
	if volume == nil {
		return nil, compute.ErrNotFound
	}
	copy := *volume
	copy.Metadata.Hidden = true
	return &copy, nil
}
func (v *managedVolumes) Clone(p compute.VolumeCloneParams) (*compute.Volume, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.clones = append(v.clones, p)
	return &compute.Volume{NodeId: p.NodeId, Path: "/" + p.NewPool + "/" + p.NewName, Size: p.NewSize, Format: p.Format}, nil
}

type managedMachines struct {
	machines
	mu      sync.Mutex
	saved   map[string]*compute.VirtualMachine
	saveErr error
}

func (m *managedMachines) ValidateNew(vm *compute.VirtualMachine) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saved[vm.Id] != nil {
		return compute.ErrConflict
	}
	return nil
}
func (m *managedMachines) Save(vm *compute.VirtualMachine) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := *vm
	m.saved[vm.Id] = &copy
	return m.saveErr
}
func (m *managedMachines) Get(id, node string) (*compute.VirtualMachine, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	vm := m.saved[id]
	if vm == nil {
		return nil, compute.ErrNotFound
	}
	copy := *vm
	return &copy, nil
}

type noImageEvents struct{}

func (noImageEvents) Publish(compute.Event) error { return nil }

type mutableImageCatalog struct{ image compute.Image }

func (c *mutableImageCatalog) List() ([]compute.Image, error) { return []compute.Image{c.image}, nil }
func (c *mutableImageCatalog) Get(id string) (compute.Image, error) {
	if id == c.image.ID {
		return c.image, nil
	}
	return compute.Image{}, compute.ErrNotFound
}

func TestDurableCreatePinsImageAndClonesHiddenVerifiedSource(t *testing.T) {
	for _, inherited := range []bool{false, true} {
		t.Run(fmt.Sprint("inherited-initialization=", inherited), func(t *testing.T) {
			data := []byte(strings.Repeat("rawdisk", 4096))
			var requests atomic.Int32
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				w.Write(data)
			}))
			defer source.Close()
			managed, backend, _, image := imageSetup(t, source.URL, data)
			image.Defaults.Initialization = "none"
			if inherited {
				image.Defaults.Initialization = ""
			}
			image.Defaults.DiskBus = compute.DeviceBusVirtio
			image.Defaults.Firmware = "bios"
			image.Defaults.Graphics = "none"
			image.Defaults.VideoModel = "none"
			off := false
			image.Defaults.GuestAgent = &off
			catalog := &mutableImageCatalog{image}
			images := compute.NewImageService(catalog)
			images.ManagedSources = managed
			vmRepo := &managedMachines{machines: machines{caps: compute.VirtualMachineCapabilities{Arch: compute.ArchAmd64, MaxVCPUs: 8, Firmwares: []string{"bios", "efi"}, DiskBuses: []compute.DeviceBus{compute.DeviceBusVirtio}, Graphics: []compute.GraphicType{compute.GraphicTypeNone, compute.GraphicTypeVnc}, VideoModels: []compute.VideoModel{compute.VideoModelNone}}}, saved: map[string]*compute.VirtualMachine{}}
			volRepo := &managedVolumes{backend: backend}
			volumes := compute.NewVolumeService(volRepo)
			vms := compute.NewVirtualMachineService(vmRepo)
			settings := map[string]compute.VirtualMachineManagerNodeSettings{"one": {CdFormat: configdrive.FormatNoCloud, CdPool: "old-init", CdSuffix: "_config.iso"}}
			if inherited {
				vmRepo.saveErr = errors.New("intentional stop before config-drive generation")
			}
			manager := compute.NewVirtualMachineManager(vms, volumes, noImageEvents{}, settings, images)
			handler := &compute.CommandHandler{Manager: manager, Volumes: volumes, VMs: vms}
			dir := filepath.Join(t.TempDir(), "tasks")
			repo, err := filesystem.NewTaskRepository(dir)
			if err != nil {
				t.Fatal(err)
			}
			service := tasks.New(repo, handler, zerolog.Nop())
			vm := &compute.VirtualMachine{Id: "pinned", Name: "pinned", NodeId: "one", Arch: compute.ArchAmd64, VCpus: 1, Memory: compute.NewSize(512, compute.SizeUnitM), Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone}
			command := compute.NewCommand("machine.create", compute.InfrastructureCommand{Node: "one", VM: vm.Id, Machine: vm, Clones: []compute.VirtualMachineManagerClonedVolumeParams{{ImageID: image.ID, NewPool: "vm-disks", NewName: "root"}}})
			accepted, err := service.Submit(tasks.Identity{ID: "admin"}, "pinned-image-request", command)
			if err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 0 || backend.creates != 0 {
				t.Fatal("submission performed image IO")
			}
			// Change the catalog before restarting the executor with its accepted command.
			catalog.image.Download = &compute.ImageDownload{URL: "https://invalid.example/revision", SHA256: strings.Repeat("0", 64), Format: compute.VolumeFormatQcow2}
			catalog.image.Defaults.Firmware = "efi"
			catalog.image.Defaults.Graphics = "vnc"
			catalog.image.Defaults.VideoModel = "qxl"
			on := true
			catalog.image.Defaults.GuestAgent = &on
			settings["one"] = compute.VirtualMachineManagerNodeSettings{CdFormat: configdrive.FormatOpenstack, CdPool: "changed-init", CdSuffix: "_different.iso"}
			repo.Close()
			repo, err = filesystem.NewTaskRepository(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			service = tasks.New(repo, handler, zerolog.Nop())
			ctx, cancel := context.WithCancel(context.Background())
			defer func() { cancel(); service.Wait() }()
			if err = service.Start(ctx, 1); err != nil {
				t.Fatal(err)
			}
			result := waitImage(t, service, accepted.ID)
			expectedState := tasks.Succeeded
			if inherited {
				expectedState = tasks.Failed
			}
			if result.State != expectedState {
				t.Fatalf("%+v", result)
			}
			if len(volRepo.clones) != 1 || volRepo.clones[0].NewSize.Bytes() != uint64(len(data)) || requests.Load() != 1 || vmRepo.saved[vm.Id].Firmware != "bios" || vmRepo.saved[vm.Id].Graphic.Type != compute.GraphicTypeNone || vmRepo.saved[vm.Id].VideoModel != compute.VideoModelNone || vmRepo.saved[vm.Id].GuestAgent {
				t.Fatal("accepted image definition or observed source capacity lost", volRepo.clones)
			}
			if inherited && (vmRepo.saved[vm.Id].Config == nil || vmRepo.saved[vm.Id].Config.Initialization != "nocloud") {
				t.Fatal("accepted node initialization defaults changed", vmRepo.saved[vm.Id].Config)
			}
			if _, err = volumes.Clone(volRepo.clones[0]); !errors.Is(err, compute.ErrVolumeNotFound) {
				t.Fatal("public clone bypassed managed image protection", err)
			}
			duplicate, err := service.Submit(tasks.Identity{ID: "admin"}, "pinned-image-request", command)
			if err != nil || duplicate.ID != accepted.ID || len(volRepo.clones) != 1 {
				t.Fatal("idempotent create duplicated effects", err)
			}
		})
	}
}

func TestImageTransferLimitAndIdleTimeout(t *testing.T) {
	for _, mode := range []string{"limit", "idle"} {
		t.Run(mode, func(t *testing.T) {
			data := []byte(strings.Repeat("disk", 4096))
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "limit" {
					w.Header().Set("Content-Length", fmt.Sprint(5<<20))
					w.(http.Flusher).Flush()
					return
				}
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				w.Write(data[:4096])
				w.(http.Flusher).Flush()
				<-r.Context().Done()
			}))
			defer source.Close()
			images, backend, _, i := imageSetup(t, source.URL, data)
			service, _ := imageTasks(t, images)
			task := submitImage(t, service, images, i, "one", "timeout-create")
			result := waitImage(t, service, task.ID)
			if result.State != tasks.Failed {
				t.Fatal(result)
			}
			b, _ := images.Bind("one", i)
			if state, _ := images.Status(b, i); state == "ready" {
				t.Fatal("limited/interrupted source became ready")
			}
			if mode == "limit" && (backend.creates != 0 || backend.uploads != 0) {
				t.Fatal("oversized transfer mutated destination")
			}
			if mode == "idle" && !strings.Contains(result.Error.Message, "timeout") {
				t.Fatal("timeout diagnostic missing", result.Error)
			}
			if mode == "idle" && (backend.creates != 1 || backend.deletes != 0 || !result.OutcomeUncertain) {
				t.Fatal("timed-out stream did not retain uncertain staging", result)
			}
		})
	}
}

func TestQueuedImageFollowersObserveFailureWithoutAutomaticRetry(t *testing.T) {
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(503) }))
	defer source.Close()
	images, _, _, image := imageSetup(t, source.URL, []byte("raw"))
	binding, _ := images.Bind("one", image)
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := tasks.New(repo, imageHandler{images}, zerolog.Nop())
	submit := func(id string) tasks.Task {
		t.Helper()
		payload, _ := json.Marshal(imageCommand{image, binding})
		task, err := service.Submit(tasks.Identity{ID: "admin"}, id, tasks.Command{Kind: "machine.create", Payload: payload, Resources: []tasks.ResourceRef{compute.VMRef("one", id)}, ExecutionGroups: []string{"shared-artifact"}})
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	first, second := submit("first-shared-failure"), submit("second-shared-failure")
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); service.Wait() }()
	if err = service.Start(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if waitImage(t, service, first.ID).State != tasks.Failed {
		t.Fatal("first transfer succeeded unexpectedly")
	}
	follower := waitImage(t, service, second.ID)
	if follower.State != tasks.Failed || !strings.Contains(follower.Error.Message, "shared image preparation failed") || requests.Load() != 1 {
		t.Fatal("queued follower silently retried", follower, requests.Load())
	}
	retry := submit("explicit-shared-retry")
	if waitImage(t, service, retry.ID).State != tasks.Failed || requests.Load() != 2 {
		t.Fatal("new explicit attempt could not retry", requests.Load())
	}
}

func TestMissingVerifiedArtifactIsPreparedByANewExplicitCreate(t *testing.T) {
	data := []byte(strings.Repeat("image", 4096))
	var requests atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.Write(data)
	}))
	defer source.Close()
	images, backend, _, image := imageSetup(t, source.URL, data)
	service, _ := imageTasks(t, images)
	first := submitImage(t, service, images, image, "one", "first-verified-create")
	if result := waitImage(t, service, first.ID); result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	binding, err := images.Bind("one", image)
	if err != nil {
		t.Fatal(err)
	}
	if err = backend.DeleteImage(binding); err != nil {
		t.Fatal(err)
	}
	if state, _ := images.Status(binding, image); state != "downloadable" || requests.Load() != 1 {
		t.Fatal("discovery must expose missing bytes without downloading", state, requests.Load())
	}
	next := submitImage(t, service, images, image, "one", "reprepare-missing-create")
	if result := waitImage(t, service, next.ID); result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	if state, _ := images.Status(binding, image); state != "ready" || requests.Load() != 2 || backend.creates != 2 {
		t.Fatal("missing artifact was not prepared again", state, requests.Load(), backend.creates)
	}
}

func TestCloudCreateRetriesCleanedUpImageFailure(t *testing.T) {
	for _, failure := range []string{"http", "upload"} {
		t.Run(failure, func(t *testing.T) {
			data := []byte(strings.Repeat("disk", 4096))
			var requests atomic.Int32
			var restored atomic.Bool
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if failure == "http" && !restored.Load() {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				w.Header().Set("Content-Length", fmt.Sprint(len(data)))
				w.Write(data)
			}))
			defer source.Close()
			managed, backend, _, image := imageSetup(t, source.URL, data)
			image.Defaults.Initialization = "none"
			backend.uploadFail = failure == "upload"
			images := compute.NewImageService(&mutableImageCatalog{image})
			images.ManagedSources = managed
			vmBackend := &cloudVMBackend{machines: map[string]compute.VirtualMachine{}}
			vms := compute.NewVirtualMachineService(vmBackend)
			volumes := compute.NewVolumeService(&managedVolumes{backend: backend})
			manager := compute.NewVirtualMachineManager(vms, volumes, cloudEvents{}, nil, images)
			cloud := compute.Cloud{ID: "c", Zones: []compute.CatalogEntry{{ID: "z"}}, Images: []string{image.ID}, InstanceTypes: []compute.InstanceType{{ID: "small", VCPUs: 1, CPURequest: 1, MemoryBytes: 1 << 30, MemoryRequestBytes: 1 << 30, BootDiskBytes: 1 << 30}}, Networks: []compute.CatalogEntry{{ID: "lan"}}, StorageClasses: []compute.CatalogEntry{{ID: "fast"}}, Hosts: []compute.CloudHost{{Node: "one", Zone: "z", Networks: map[string]compute.NetworkBinding{"lan": {Network: "net"}}, Storage: map[string]compute.StorageBinding{"fast": {Pool: "pool", Format: compute.VolumeFormatRaw}}}}}
			clouds := &compute.CloudService{Definitions: []compute.Cloud{cloud}, Manager: manager, VMs: vms, Volumes: volumes, Nodes: compute.NewNodeService(cloudNodeBackend{cpus: 4}), Pools: compute.NewVolumePoolService(retryImagePools{})}
			manager.Clouds = clouds
			repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { repo.Close() })
			service := tasks.New(repo, &compute.CommandHandler{Clouds: clouds, Manager: manager, VMs: vms, Volumes: volumes, Nodes: clouds.Nodes}, zerolog.Nop())
			runCloudService(t, service)
			command := compute.InfrastructureCommand{CloudID: "c", CreateSpec: &compute.MachineCreateSpec{Name: "new", Image: image.ID, InstanceType: "small", Zone: "z", Network: "lan", StorageClass: "fast"}}
			first := submitCloud(t, service, "failed-image-create", "machine.create", command)
			if result := waitImage(t, service, first.ID); result.State != tasks.Failed {
				t.Fatal(result)
			}
			restored.Store(true)
			backend.mu.Lock()
			backend.uploadFail = false
			backend.mu.Unlock()
			offers, err := clouds.Offers(cloud)
			if err != nil || len(offers) != 1 || offers[0].Image != image.ID {
				t.Fatalf("retryable image missing from UI offers: %+v %v", offers, err)
			}
			if requests.Load() != 1 {
				t.Fatal("discovery retried the source")
			}
			duplicate := submitCloud(t, service, "failed-image-create", "machine.create", command)
			if duplicate.ID != first.ID || duplicate.State != tasks.Failed || requests.Load() != 1 {
				t.Fatal("failed creation automatically replayed", duplicate)
			}
			if failure == "upload" {
				// Cleanup confirms artifact absence but does not release uncertain task effects.
				if _, err := service.Submit(tasks.Identity{ID: "admin"}, "blocked-image-retry", compute.NewCommand("machine.create", command)); err == nil {
					t.Fatal("uncertain failed task lock was silently released")
				}
				binding, _ := managed.Bind("one", image)
				if _, _, err := backend.InspectImage(binding); !errors.Is(err, compute.ErrNotFound) {
					t.Fatal("staging cleanup was not confirmed", err)
				}
				if _, err := service.Release(first.ID, tasks.Identity{ID: "admin"}, "Inspected backend; owned staging is absent and transfer has stopped"); err != nil {
					t.Fatal(err)
				}
			}
			retry := submitCloud(t, service, "explicit-image-retry", "machine.create", command)
			if result := waitImage(t, service, retry.ID); result.State != tasks.Succeeded {
				t.Fatal(result)
			}
			binding, _ := managed.Bind("one", image)
			if state, _ := managed.Status(binding, image); state != "ready" || requests.Load() != 2 {
				t.Fatal("new creation did not verify the retry", state, requests.Load())
			}
		})
	}
}

type retryImagePools struct{ compute.VolumePoolRepository }

func (retryImagePools) List(o compute.VolumePoolListOptions) ([]*compute.VolumePool, error) {
	return []*compute.VolumePool{{NodeId: o.NodeIds[0], Name: "pool", Free: compute.NewSize(100, compute.SizeUnitG)}, {NodeId: o.NodeIds[0], Name: "images-one", Free: compute.NewSize(100, compute.SizeUnitG)}}, nil
}

func TestCleanedUpImageStatusStillRejectsUnsafeStorage(t *testing.T) {
	for _, state := range []string{"absent", "incomplete", "failed", "unknown"} {
		t.Run(state, func(t *testing.T) {
			images, backend, artifacts, image := imageSetup(t, "https://example.invalid/image", []byte("raw"))
			binding, _ := images.Bind("one", image)
			if err := backend.CreateImage(binding, 3); err != nil {
				t.Fatal(err)
			}
			if state != "unknown" {
				hash := sha256.Sum256([]byte(binding.Scope + "\x00" + binding.PoolID + "\x00" + binding.Path))
				if err := artifacts.Put(hex.EncodeToString(hash[:]), compute.ImageArtifact{Binding: binding, State: state, Error: "prior transfer failure", FailedAt: time.Now()}); err != nil {
					t.Fatal(err)
				}
			}
			if status, _ := images.Status(binding, image); status != "failed" {
				t.Fatal("unsafe image offered for retry", status)
			}
			service, _ := imageTasks(t, images)
			task := submitImage(t, service, images, image, "one", "unsafe-create")
			if result := waitImage(t, service, task.ID); result.State != tasks.Failed {
				t.Fatal(result)
			}
			if backend.creates != 1 || backend.uploads != 0 || backend.deletes != 0 {
				t.Fatal("unsafe image mutated")
			}
		})
	}
}
