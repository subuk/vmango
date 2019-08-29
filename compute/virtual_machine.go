package compute

type VirtualMachineRepository interface {
	List() ([]*VirtualMachine, error)
	Get(id string) (*VirtualMachine, error)
	Create(id string, arch Arch, vcpus int, memoryKb uint, volumes []*VirtualMachineAttachedVolume, interfaces []*VirtualMachineAttachedInterface) (*VirtualMachine, error)
	Delete(id string) error
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

func (vm *VirtualMachine) Disks() []*VirtualMachineAttachedVolume {
	disks := []*VirtualMachineAttachedVolume{}
	for _, volume := range vm.Volumes {
		if volume.Device == DeviceTypeDisk {
			disks = append(disks, volume)
		}
	}
	return disks
}

func (vm *VirtualMachine) Cdroms() []*VirtualMachineAttachedVolume {
	cdroms := []*VirtualMachineAttachedVolume{}
	for _, volume := range vm.Volumes {
		if volume.Device == DeviceTypeCdrom {
			cdroms = append(cdroms, volume)
		}
	}
	return cdroms
}

type VirtualMachineAttachedVolume struct {
	Type   VolumeType
	Path   string
	Format VolumeFormat
	Device DeviceType
}

type VirtualMachineAttachedInterface struct {
	Type    NetworkType
	Network string
	Mac     string
	Model   string
}
