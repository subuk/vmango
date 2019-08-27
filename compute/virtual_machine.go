package compute

type VirtualMachineRepository interface {
	List() ([]*VirtualMachine, error)
	Get(id string) (*VirtualMachine, error)
	Poweroff(id string) error
	Reboot(id string) error
	Start(id string) error
}

type VirtualMachineState int

const (
	StateUnknown = VirtualMachineState(0)
	StateStopped = VirtualMachineState(1)
	StateRunning = VirtualMachineState(2)
)

func (state VirtualMachineState) String() string {
	switch state {
	default:
		return "unknown"
	case StateStopped:
		return "stopped"
	case StateRunning:
		return "running"
	}
}

type VirtualMachine struct {
	Id         string
	VCpus      int
	Arch       Arch
	State      VirtualMachineState
	Memory     uint // KiB
	Interfaces []*VirtualMachineAttachedInterface
	Volumes    []*VirtualMachineAttachedVolume
}

func (vm *VirtualMachine) IsRunning() bool {
	return vm.State == StateRunning
}

func (vm *VirtualMachine) MemoryMiB() uint {
	return vm.Memory / 1024
}

type VirtualMachineAttachedVolume struct {
	Type string
	Path string
}

type VirtualMachineAttachedInterface struct {
	Mac string
}
