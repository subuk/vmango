package compute

// <disk type='file' device='cdrom'>
//   <driver name='qemu' type='raw'/>
//   <source file='/var/lib/libvirt/images/ifunny-mongo-customs-rs1-1_config.iso'/>
//   <target dev='hda' bus='ide'/>
//   <readonly/>
//   <address type='drive' controller='0' bus='0' target='0' unit='0'/>
// </disk>
// <disk type='block' device='disk'>
//   <driver name='qemu' type='raw' cache='none' io='native'/>
//   <source dev='/dev/io101-data/ifunny_mongo_customs_rs1_1'/>
//   <target dev='vda' bus='virtio'/>
//   <address type='pci' domain='0x0000' bus='0x01' slot='0x01' function='0x0'/>
// </disk>

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
