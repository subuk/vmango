package compute

import "testing"

type shutdownRepository struct {
	VirtualMachineRepository
	called bool
}

func (r *shutdownRepository) Shutdown(id, node string) error {
	r.called = id == "vm" && node == "node"
	return nil
}

func TestShutdownAction(t *testing.T) {
	repo := &shutdownRepository{}
	if err := NewVirtualMachineService(repo).Action("vm", "node", "shutdown"); err != nil {
		t.Fatal(err)
	}
	if !repo.called {
		t.Fatal("shutdown request was not forwarded to the repository")
	}
}
