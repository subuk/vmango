package libvirt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	vir "libvirt.org/go/libvirt"
	"path/filepath"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/filesystem"
	"subuk/vmango/tasks"
	"testing"
	"time"
)

func TestRepositoryErrorCategories(t *testing.T) {
	for _, tc := range []struct {
		code vir.ErrorNumber
		want error
	}{{vir.ERR_NO_DOMAIN, compute.ErrNotFound}, {vir.ERR_NO_STORAGE_VOL, compute.ErrNotFound}, {vir.ERR_OPERATION_INVALID, compute.ErrConflict}, {vir.ERR_INVALID_ARG, compute.ErrInvalid}, {vir.ERR_NO_CONNECT, compute.ErrUnavailable}} {
		err := repositoryError(vir.Error{Code: tc.code, Message: "backend detail"}, "operation failed")
		if !errors.Is(err, tc.want) {
			t.Fatalf("%v: %v", tc.code, err)
		}
		if !errors.Is(err, tc.code) {
			t.Fatal("backend cause lost")
		}
	}
}

type diagnosticVMRepo struct {
	compute.VirtualMachineRepository
	vm         *compute.VirtualMachine
	allowStart bool
	started    bool
}

func (r *diagnosticVMRepo) ValidateNew(*compute.VirtualMachine) error { return nil }
func (r *diagnosticVMRepo) Save(vm *compute.VirtualMachine) error {
	copy := *vm
	r.vm = &copy
	return nil
}
func (r *diagnosticVMRepo) Get(string, string) (*compute.VirtualMachine, error) {
	if r.started {
		return nil, errors.New("read after Start is deliberately unavailable")
	}
	return r.vm, nil
}
func (r *diagnosticVMRepo) Start(string, string) error {
	if r.allowStart {
		r.started = true
		return nil
	}
	return repositoryError(fmt.Errorf("adapter wrapper: %w", vir.Error{Code: vir.ERR_OPERATION_FAILED, Domain: vir.FROM_QEMU, Message: "QEMU driver: disk image is locked; password=bootstrap-private-secret"}), "virDomainCreate")
}

type diagnosticEvents struct{}

func (diagnosticEvents) Publish(compute.Event) error { return nil }

func TestLibvirtCauseSurvivesServiceProvisioningAndDurableTaskHistory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tasks")
	repo, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	backend := &diagnosticVMRepo{}
	vms := compute.NewVirtualMachineService(backend)
	manager := compute.NewVirtualMachineManager(vms, nil, diagnosticEvents{}, nil, nil)
	handler := &compute.CommandHandler{Manager: manager, VMs: vms}
	service := tasks.New(repo, handler, zerolog.Nop())
	vm := &compute.VirtualMachine{Id: "v", Name: "v", NodeId: "n", Arch: compute.ArchAmd64, VCpus: 2, Memory: compute.NewSize(1, compute.SizeUnitG), Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone}
	task, err := service.Submit(tasks.Identity{ID: "admin"}, "backend-cause-submission", compute.NewCommand("machine.create", compute.InfrastructureCommand{Node: "n", VM: "v", Machine: vm, Start: true}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := service.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err = service.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if task.State.Terminal() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	service.Wait()
	repo.Close()
	reopened, err := filesystem.NewTaskRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, err := tasks.New(reopened, handler, zerolog.Nop()).Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.State != tasks.Failed || saved.Error == nil || saved.Error.Code != fmt.Sprint(int(vir.ERR_OPERATION_FAILED)) || saved.Error.Domain != fmt.Sprint(int(vir.FROM_QEMU)) || saved.Error.Operation != "virDomainCreate" || !strings.Contains(saved.Error.Message, "disk image is locked") || len(saved.Effects) != 1 || saved.Stages[len(saved.Stages)-1].Name != "start-vm" {
		t.Fatalf("lost libvirt cause through provisioning: %+v", saved)
	}
	encoded, _ := json.Marshal(saved)
	if strings.Contains(string(encoded), "bootstrap-private-secret") {
		t.Fatal("secret in persisted public diagnostic")
	}
}

func TestSuccessfulStartDoesNotRequireRunningStateReadback(t *testing.T) {
	repo, err := filesystem.NewTaskRepository(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	backend := &diagnosticVMRepo{allowStart: true}
	vms := compute.NewVirtualMachineService(backend)
	manager := compute.NewVirtualMachineManager(vms, nil, diagnosticEvents{}, nil, nil)
	service := tasks.New(repo, &compute.CommandHandler{Manager: manager, VMs: vms}, zerolog.Nop())
	vm := &compute.VirtualMachine{Id: "v", Name: "v", NodeId: "n", Arch: compute.ArchAmd64, VCpus: 2, Memory: compute.NewSize(1, compute.SizeUnitG), Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeNone}, VideoModel: compute.VideoModelNone}
	task, err := service.Submit(tasks.Identity{ID: "admin"}, "successful-start-command", compute.NewCommand("machine.create", compute.InfrastructureCommand{Node: "n", VM: "v", Machine: vm, Start: true}))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); service.Wait() }()
	if err := service.Start(ctx, 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		task, err = service.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if task.State.Terminal() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if task.State != tasks.Succeeded || len(task.Stages) == 0 || task.Stages[len(task.Stages)-1].Name != "start-vm" {
		t.Fatalf("Start return should be sufficient: %+v", task)
	}
}
