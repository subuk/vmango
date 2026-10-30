package libvirt

import (
	"errors"
	"subuk/vmango/compute"
	"testing"

	"github.com/rs/zerolog"
	vir "libvirt.org/go/libvirt"
	"libvirt.org/go/libvirtxml"
)

func TestDomainIdentityIsIndependentOfName(t *testing.T) {
	const id = "550e8400-e29b-41d4-a716-446655440000"
	vm := &compute.VirtualMachine{Id: id, Name: "display-name", VCpus: 2, Memory: compute.NewSize(1, compute.SizeUnitG)}
	config := newDomainConfig(vm, &libvirtxml.DomainCaps{Domain: "kvm", Arch: "x86_64"})
	if config.UUID != id || config.Name != vm.Name {
		t.Fatalf("identity changed while generating XML: %+v", config)
	}
	config.VCPU = &libvirtxml.DomainVCPU{Value: 2}
	config.Memory = &libvirtxml.DomainMemory{Value: 1, Unit: "GiB"}
	config.Name = "renamed-externally"
	observed, err := VirtualMachineFromDomainConfig(config, &vir.DomainInfo{State: vir.DOMAIN_SHUTOFF})
	if err != nil || observed.Id != id || observed.Name != config.Name {
		t.Fatalf("lost stable identity: %+v %v", observed, err)
	}
}

func TestUUIDOperationsWithLibvirtTestDriver(t *testing.T) {
	pool := NewConnectionPool(map[string]string{"n": "test:///default"}, []string{"n"}, zerolog.Nop())
	conn, err := pool.Acquire("n")
	if err != nil {
		t.Skipf("optional libvirt test driver unavailable: %v", err)
	}
	defer conn.Close()
	domain, err := conn.LookupDomainByName("test")
	if err != nil {
		pool.Release("n")
		t.Fatal(err)
	}
	id, err := domain.GetUUIDString()
	domain.Free()
	pool.Release("n")
	if err != nil {
		t.Fatal(err)
	}
	repo := NewVirtualMachineRepository(pool, map[string]NodeSettings{"n": {}}, zerolog.Nop())
	vm, err := repo.Get(id, "n")
	if err != nil || vm.Id != id || vm.Name != "test" {
		t.Fatalf("UUID read: %+v %v", vm, err)
	}
	if _, err := repo.Get("test", "n"); err == nil {
		t.Fatal("display name was accepted as an identity")
	}
	if err := repo.ValidateNew(&compute.VirtualMachine{Id: "550e8400-e29b-41d4-a716-446655440000", Name: "test", NodeId: "n"}); !errors.Is(err, compute.ErrConflict) {
		t.Fatalf("name collision: %v", err)
	}
	if err := repo.ValidateNew(&compute.VirtualMachine{Id: id, Name: "another-name", NodeId: "n"}); !errors.Is(err, compute.ErrConflict) {
		t.Fatalf("UUID collision: %v", err)
	}
	if err := repo.Adopt(id, "n", &compute.MachineMetadata{Cloud: "c", Zone: "z", CPURequest: vm.VCpus, MemoryRequestBytes: vm.Memory.Bytes()}); err != nil {
		t.Fatal(err)
	}
	vm, err = repo.Get(id, "n")
	if err != nil || vm.Metadata == nil || vm.Metadata.Cloud != "c" {
		t.Fatalf("UUID adoption: %+v %v", vm, err)
	}
	if err := repo.Poweroff(id, "n"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Start(id, "n"); err != nil {
		t.Fatal(err)
	}
}
