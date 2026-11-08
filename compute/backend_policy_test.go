package compute_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type powerPolicyBackend struct {
	*cloudVMBackend
	powerError     error
	autostartError error
	unconfirmed    bool
	updates        int
}

func (r *powerPolicyBackend) Shutdown(id, node string) error {
	if r.powerError != nil {
		return r.powerError
	}
	if r.unconfirmed {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	vm := r.machines[node+"/"+id]
	vm.State = compute.StateStopped
	r.machines[node+"/"+id] = vm
	return nil
}
func (r *powerPolicyBackend) Poweroff(id, node string) error { return r.Shutdown(id, node) }
func (r *powerPolicyBackend) Reboot(id, node string) error   { return r.powerError }
func (r *powerPolicyBackend) SetAutostart(id, node string, enabled bool) error {
	r.updates++
	if r.autostartError != nil {
		return r.autostartError
	}
	return r.cloudVMBackend.SetAutostart(id, node, enabled)
}
func TestPowerAutostartSynchronizationPreservesPartialResults(t *testing.T) {
	for _, test := range []struct {
		name, action                    string
		failed, unconfirmed, syncFailed bool
	}{
		{name: "shutdown", action: "shutdown"}, {name: "poweroff", action: "poweroff"}, {name: "reboot", action: "reboot"},
		{name: "failed shutdown", action: "shutdown", failed: true}, {name: "unconfirmed shutdown", action: "shutdown", unconfirmed: true}, {name: "partial shutdown", action: "shutdown", syncFailed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, base, _, _ := cloudFixtureHandler(t, 4, nil)
			backend := &powerPolicyBackend{cloudVMBackend: base, unconfirmed: test.unconfirmed}
			if test.failed {
				backend.powerError = errors.New("shutdown failed")
			}
			if test.syncFailed {
				backend.autostartError = errors.New("autostart failed")
			}
			h.VMs.VirtualMachineRepository = backend
			base.machines["a/v"] = compute.VirtualMachine{Id: "v", NodeId: "a", Name: "v", State: compute.StateRunning, Autostart: true, Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z"}}
			repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
			if err != nil {
				t.Fatal(err)
			}
			defer repo.Close()
			service := tasks.New(repo, h, zerolog.Nop())
			task := submitCloud(t, service, "power-policy-test", "machine."+test.action, compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "v"})
			ctx, cancel := context.WithCancel(context.Background())
			defer func() { cancel(); service.Wait() }()
			if err := service.Start(ctx, 1); err != nil {
				t.Fatal(err)
			}
			result := awaitCloudTask(t, service, task.ID)
			vm, _ := base.Get("v", "a")
			unchanged := test.failed || test.unconfirmed || test.syncFailed || test.action == "reboot"
			if vm.Autostart != unchanged {
				t.Fatal("incorrect autostart", vm.Autostart, result)
			}
			if test.failed || test.unconfirmed || test.action == "reboot" {
				if backend.updates != 0 {
					t.Fatal("unexpected autostart write")
				}
			} else if backend.updates != 1 {
				t.Fatal("missing synchronization")
			}
			if test.syncFailed {
				if result.State != tasks.Failed || len(result.Effects) == 0 || !strings.Contains(result.Error.Message, "power operation completed") {
					t.Fatal("lost partial result", result)
				}
			}
		})
	}
}

func TestSelectedPlanIsDurableBeforeProvisioning(t *testing.T) {
	h, _, volumes, _ := cloudFixtureHandler(t, 4, nil)
	volumes.cloneStarted = make(chan struct{})
	volumes.cloneRelease = make(chan struct{})
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := tasks.New(repo, h, zerolog.Nop())
	accepted := submitCloud(t, service, "durable-final-plan", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("one")})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); service.Wait() }()
	if err := service.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-volumes.cloneStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("provisioning did not begin")
	}
	store, err := repo.Read()
	if err != nil {
		t.Fatal(err)
	}
	rec := store.Records[accepted.ID]
	var c compute.InfrastructureCommand
	if err := json.Unmarshal(rec.Payload, &c); err != nil {
		t.Fatal(err)
	}
	if c.Node != "a" || c.Machine == nil || c.Machine.NodeId != "a" || c.CreateSpec != nil || len(c.Clones) != 1 {
		t.Fatal("selected final plan was not persisted", c)
	}
	for _, stage := range rec.Task.Stages {
		if stage.Name == "cloud-inventory" || stage.Name == "schedule-machine" {
			t.Fatal("inventory and scheduling were independently staged")
		}
	}
	close(volumes.cloneRelease)
	if result := awaitCloudTask(t, service, accepted.ID); result.State != tasks.Succeeded {
		t.Fatal(result)
	}
}

// policyTestBackend exercises the opaque plan contract without importing a
// provider's hardware types into the common lifecycle tests.
type policyTestBackend struct {
	*cloudVMBackend
	resolutions               int
	rejectCurrentCapabilities bool
	capabilityReads           int
}

func (r *policyTestBackend) ResolveHardware(vm *compute.VirtualMachine, policy, binding json.RawMessage) error {
	r.resolutions++
	raw, _ := json.Marshal(struct {
		Policy  json.RawMessage
		Binding json.RawMessage
	}{policy, binding})
	return r.ApplyHardware(vm, raw)
}
func (r *policyTestBackend) ApplyHardware(vm *compute.VirtualMachine, raw json.RawMessage) error {
	vm.AppliedPolicy = append(json.RawMessage(nil), raw...)
	vm.Graphic = compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone, BindingResolved: true}
	vm.VideoModel = compute.VideoModelNone
	vm.Config = nil
	vm.DiskController = "virtio-scsi"
	for _, nic := range vm.Interfaces {
		nic.Model = "virtio"
	}
	return nil
}
func (r *policyTestBackend) ValidateAppliedPolicy(vm *compute.VirtualMachine) error {
	if vm.Metadata == nil || len(vm.AppliedPolicy) == 0 {
		return errors.New("explicit adoption required")
	}
	return nil
}
func (r *policyTestBackend) ResolveDiskAttachment(vm *compute.VirtualMachine, disk *compute.VirtualMachineAttachedVolume) error {
	if len(vm.AppliedPolicy) == 0 {
		return errors.New("missing policy")
	}
	if disk.DeviceBus != compute.DeviceBusUnknown && disk.DeviceBus != compute.DeviceBusScsi {
		return errors.New("conflicting bus")
	}
	disk.DeviceBus = compute.DeviceBusScsi
	disk.DeviceType = compute.DeviceTypeDisk
	return nil
}
func (r *policyTestBackend) ValidateResourceChange(current, requested *compute.VirtualMachine) error {
	return r.ValidateAppliedPolicy(current)
}
func (r *policyTestBackend) ValidateAdoptionPolicy(vm *compute.VirtualMachine, raw json.RawMessage) error {
	return nil
}
func (r *policyTestBackend) PrepareAdoption(vm *compute.VirtualMachine, cloud, ref string) (json.RawMessage, error) {
	return json.RawMessage(`{"explicit":true}`), nil
}
func (r *policyTestBackend) AdoptWithPolicy(id, node string, m *compute.MachineMetadata, policy json.RawMessage) error {
	return nil
}
func (r *policyTestBackend) Capabilities(node string, arch compute.Arch) (compute.VirtualMachineCapabilities, error) {
	r.capabilityReads++
	if r.rejectCurrentCapabilities {
		return compute.VirtualMachineCapabilities{}, errors.New("the newly configured emulator is unavailable")
	}
	caps, err := r.cloudVMBackend.Capabilities(node, arch)
	caps.DiskBuses = append(caps.DiskBuses, compute.DeviceBusScsi)
	caps.DiskControllers = []string{"virtio-scsi"}
	return caps, err
}
func TestBackendPlanAndHostSettingsSurviveQueuedCatalogChanges(t *testing.T) {
	h, base, volumes, _ := cloudFixtureHandler(t, 4, nil)
	backend := &policyTestBackend{cloudVMBackend: base}
	h.VMs.VirtualMachineRepository = backend
	imageCatalog := &mutableImageCatalog{image: compute.Image{CloudID: "c", ID: "image", Name: "Image", Arch: compute.ArchAmd64, SourcePath: "/source", BackendPolicy: json.RawMessage(`{"revision":"accepted"}`), DataDisks: []compute.ImageDataDisk{{Name: "required", SizeBytes: 1 << 30}}}}
	images := compute.NewImageService(imageCatalog)
	acceptedSettings := map[string]compute.VirtualMachineManagerNodeSettings{"a": {CdPool: "accepted-pool", CdSuffix: "-accepted.iso"}, "b": {CdPool: "accepted-pool", CdSuffix: "-accepted.iso"}}
	h.Manager = compute.NewVirtualMachineManager(h.VMs, h.Volumes, cloudEvents{}, acceptedSettings, images)
	h.Manager.Clouds = h.Clouds
	h.Clouds.Manager = h.Manager
	volumes.cloneStarted = make(chan struct{})
	volumes.cloneRelease = make(chan struct{})
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := tasks.New(repo, h, zerolog.Nop())
	accepted := submitCloud(t, service, "opaque-policy-snapshot", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("one")})
	imageCatalog.image.BackendPolicy = json.RawMessage(`{"revision":"edited"}`)
	base.listeners["a"] = "192.0.2.77"
	acceptedSettings["a"] = compute.VirtualMachineManagerNodeSettings{CdPool: "changed-pool", CdSuffix: "-changed.iso"}
	beforeResolutions := backend.resolutions
	backend.rejectCurrentCapabilities = true
	beforeCapabilityReads := backend.capabilityReads
	// A fresh service simulates a restart with edited catalogs and bindings.
	service = tasks.New(repo, h, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); service.Wait() }()
	if err := service.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	select {
	case <-volumes.cloneStarted:
	case <-time.After(3 * time.Second):
		task, _ := service.Get(accepted.ID)
		t.Fatal("provisioning did not begin", task)
	}
	store, err := repo.Read()
	if err != nil {
		close(volumes.cloneRelease)
		t.Fatal(err)
	}
	var c compute.InfrastructureCommand
	err = json.Unmarshal(store.Records[accepted.ID].Payload, &c)
	close(volumes.cloneRelease)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(c.Machine.AppliedPolicy), "accepted") || strings.Contains(string(c.Machine.AppliedPolicy), "edited") || !strings.Contains(string(c.Machine.AppliedPolicy), "127.0.0.2") {
		t.Fatal("accepted backend plan was not retained", string(c.Machine.AppliedPolicy))
	}
	if c.Settings.CdPool != "accepted-pool" || c.Settings.CdSuffix != "-accepted.iso" {
		t.Fatal("host-local bindings were reread", c.Settings)
	}
	if backend.capabilityReads != beforeCapabilityReads {
		t.Fatal("accepted plan queried current emulator capabilities")
	}
	if backend.resolutions != beforeResolutions {
		t.Fatal("accepted policy was re-resolved at execution")
	}
	if c.Clones[0].DeviceBus != compute.DeviceBusScsi || c.NewVolumes[0].DeviceBus != compute.DeviceBusScsi {
		t.Fatal("ordinary disks ignored applied policy")
	}
	if result := awaitCloudTask(t, service, accepted.ID); result.State != tasks.Succeeded {
		t.Fatal(result)
	}
}

func TestOrdinaryDiskRequestsCannotUseGeneratedArtifactIdentity(t *testing.T) {
	h, backend, volumes, _ := cloudFixtureHandler(t, 4, nil)
	backend.machines["a/v"] = compute.VirtualMachine{Id: "v", Name: "v", NodeId: "a", State: compute.StateStopped, Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z"}}
	for _, test := range []struct {
		kind    string
		command compute.InfrastructureCommand
	}{
		{"machine.attach-disk", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "v", Disk: &compute.VirtualMachineAttachedVolume{Path: "/ordinary", Alias: "configdrive", DeviceType: compute.DeviceTypeDisk, DeviceBus: compute.DeviceBusVirtio}}},
		{"machine.create-disk", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "v", NewDisk: &compute.DataDiskSpec{Name: "configdrive", StorageClass: "fast", SizeBytes: 1 << 30}}},
	} {
		if _, err := h.Prepare(compute.NewCommand(test.kind, test.command)); err == nil {
			t.Fatal("reserved generated identity accepted", test.kind)
		}
	}
	if backend.mutations != 0 || volumes.created != 0 {
		t.Fatal("reserved identity rejection caused side effects")
	}
}

func (r *policyTestBackend) ResolveInterfaceAttachment(vm *compute.VirtualMachine, nic *compute.VirtualMachineAttachedInterface) error {
	if len(vm.AppliedPolicy) == 0 {
		return errors.New("missing applied policy")
	}
	if nic.Model != "" && nic.Model != "virtio" {
		return &compute.ValidationError{Field: "model", Message: "NIC model conflicts with applied policy"}
	}
	nic.Model = "virtio"
	return nil
}

func TestInstanceTypeChangeValidatesAppliedBindingInsteadOfCurrentEmulator(t *testing.T) {
	h, base, _, _ := cloudFixtureHandler(t, 4, nil)
	backend := &policyTestBackend{cloudVMBackend: base, rejectCurrentCapabilities: true}
	h.VMs.VirtualMachineRepository = backend
	base.machines["a/v"] = compute.VirtualMachine{Id: "v", Name: "v", NodeId: "a", Arch: compute.ArchAmd64, VCpus: 1, Memory: compute.NewSize(1, compute.SizeUnitG), State: compute.StateStopped, AppliedPolicy: json.RawMessage(`{"accepted":"old-emulator"}`), Metadata: &compute.MachineMetadata{Cloud: "c", Zone: "z", CPURequest: 1, MemoryRequestBytes: 1 << 30}}
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := tasks.New(repo, h, zerolog.Nop())
	accepted := submitCloud(t, service, "type-change-pinned-emulator", "machine.change-type", compute.InfrastructureCommand{CloudID: "c", Node: "a", VM: "v", InstanceType: "small"})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); service.Wait() }()
	if err := service.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if result := awaitCloudTask(t, service, accepted.ID); result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	vm, _ := base.Get("v", "a")
	if backend.capabilityReads != 0 || vm.VCpus != 2 || string(vm.AppliedPolicy) != `{"accepted":"old-emulator"}` {
		t.Fatal("current emulator replaced applied binding", vm)
	}
}

func TestBackendValidatedGuestArchitectureIsIndependentOfHostCPU(t *testing.T) {
	h, base, _, cloud := cloudFixtureHandler(t, 4, nil)
	backend := &policyTestBackend{cloudVMBackend: base}
	h.VMs.VirtualMachineRepository = backend
	images := compute.NewImageService(&mutableImageCatalog{image: compute.Image{CloudID: "c", ID: "image", Arch: compute.ArchAarch64, SourcePath: "/source", BackendPolicy: json.RawMessage(`{"emulation":"explicit"}`)}})
	h.Manager = compute.NewVirtualMachineManager(h.VMs, h.Volumes, cloudEvents{}, nil, images)
	h.Manager.Clouds = h.Clouds
	h.Clouds.Manager = h.Manager
	offers, err := h.Clouds.Offers(cloud)
	if err != nil || len(offers) == 0 {
		t.Fatalf("backend-supported guest architecture hidden by host CPU: %+v %v", offers, err)
	}
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	service := tasks.New(repo, h, zerolog.Nop())
	accepted := submitCloud(t, service, "emulated-architecture", "machine.create", compute.InfrastructureCommand{CloudID: "c", CreateSpec: createSpec("emulated")})
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); service.Wait() }()
	if err := service.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	result := awaitCloudTask(t, service, accepted.ID)
	if result.State != tasks.Succeeded {
		t.Fatal(result)
	}
	vm, err := base.Get(taskMachineID(t, result), "a")
	if err != nil || vm.Arch != compute.ArchAarch64 {
		t.Fatal("guest architecture replaced", vm, err)
	}
}
