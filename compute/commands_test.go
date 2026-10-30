package compute_test

import (
	"context"
	"path/filepath"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

type queuedPatchVMs struct {
	compute.VirtualMachineRepository
	mu      sync.Mutex
	vm      compute.VirtualMachine
	missing bool
	saves   int
}

func (r *queuedPatchVMs) Get(string, string) (*compute.VirtualMachine, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.missing {
		return nil, compute.ErrNotFound
	}
	copy := r.vm
	return &copy, nil
}
func (r *queuedPatchVMs) Save(vm *compute.VirtualMachine) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.vm = *vm
	r.saves++
	return nil
}

func (r *queuedPatchVMs) ValidateNew(*compute.VirtualMachine) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.missing {
		return compute.ErrConflict
	}
	return nil
}

func TestQueuedCreationRejectsVMCreatedBeforeExecution(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tasks")
	repo, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	backend := &queuedPatchVMs{missing: true}
	vms := compute.NewVirtualMachineService(backend)
	manager := compute.NewVirtualMachineManager(vms, nil, nil, nil, nil)
	handler := &compute.CommandHandler{VMs: vms, Manager: manager}
	service := tasks.New(repo, handler, zerolog.Nop())
	requested := &compute.VirtualMachine{Id: "v", NodeId: "n", Arch: compute.ArchAmd64, VCpus: 2, Memory: compute.NewSize(2, compute.SizeUnitG), Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone}
	accepted, err := service.Submit(tasks.Identity{ID: "admin"}, "queued-creation-submission", compute.NewCommand("machine.create", compute.InfrastructureCommand{Node: "n", VM: "v", Machine: requested}))
	if err != nil {
		t.Fatal(err)
	}
	repo.Close()
	backend.vm = *requested
	backend.vm.VCpus = 8
	backend.missing = false
	reopened, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	resumed := tasks.New(reopened, handler, zerolog.Nop())
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); resumed.Wait() }()
	if err := resumed.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	var result tasks.Task
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		result, err = resumed.Get(accepted.ID)
		if err != nil {
			t.Fatal(err)
		}
		if result.State.Terminal() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if result.State != tasks.Failed || result.OutcomeUncertain || backend.saves != 0 || backend.vm.VCpus != 8 || len(result.Stages) != 1 || result.Stages[0].Name != "validate-creation" {
		t.Fatalf("queued creation changed an existing VM: %+v saves=%d", result, backend.saves)
	}
}

func TestQueuedPatchReadsCurrentConfigurationAfterRestart(t *testing.T) {
	for _, missing := range []bool{false, true} {
		name := "preserve omitted fields"
		if missing {
			name = "do not recreate a missing VM"
		}
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "tasks")
			repo, err := filesystem.NewTaskRepository(dir)
			if err != nil {
				t.Fatal(err)
			}
			backend := &queuedPatchVMs{vm: compute.VirtualMachine{Id: "v", NodeId: "n", VCpus: 2, Memory: compute.NewSize(2, compute.SizeUnitG), GuestAgent: true, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone}}
			handler := &compute.CommandHandler{VMs: compute.NewVirtualMachineService(backend)}
			service := tasks.New(repo, handler, zerolog.Nop())
			autostart := true
			accepted, err := service.Submit(tasks.Identity{ID: "admin"}, "queued-patch-submission", compute.NewCommand("machine.update", compute.InfrastructureCommand{Node: "n", VM: "v", Patch: &compute.MachinePatch{Autostart: &autostart}}))
			if err != nil {
				t.Fatal(err)
			}
			repo.Close()
			// Infrastructure may change while Vmango is stopped. Those fields
			// were not part of the patch and must not be restored from a snapshot.
			backend.vm.VCpus = 4
			backend.vm.Memory = compute.NewSize(4, compute.SizeUnitG)
			backend.missing = missing
			reopened, err := filesystem.NewTaskRepository(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			resumed := tasks.New(reopened, handler, zerolog.Nop())
			ctx, cancel := context.WithCancel(context.Background())
			defer func() { cancel(); resumed.Wait() }()
			if err := resumed.Start(ctx, 1); err != nil {
				t.Fatal(err)
			}
			var result tasks.Task
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				result, err = resumed.Get(accepted.ID)
				if err != nil {
					t.Fatal(err)
				}
				if result.State.Terminal() {
					break
				}
				time.Sleep(time.Millisecond)
			}
			backend.mu.Lock()
			defer backend.mu.Unlock()
			if missing {
				if result.State != tasks.Failed || backend.saves != 0 || result.OutcomeUncertain || len(result.Stages) != 1 || result.Stages[0].Name != "read-current-configuration" {
					t.Fatalf("missing VM was written or failure lost: %+v saves=%d", result, backend.saves)
				}
			} else if result.State != tasks.Succeeded || backend.saves != 1 || !backend.vm.Autostart || backend.vm.VCpus != 4 || backend.vm.Memory.Bytes() != 4<<30 || !backend.vm.GuestAgent {
				t.Fatalf("queued patch overwrote omitted configuration: %+v task=%+v", backend.vm, result)
			}
		})
	}
}
