package compute

type VirtualMachineRepository interface {
	List() ([]*VirtualMachine, error)
	Get(id string) (*VirtualMachine, error)
	Create(id string, arch Arch, vcpus int, memoryKb uint, volumes []*VirtualMachineAttachedVolume, interfaces []*VirtualMachineAttachedInterface, config *VirtualMachineConfig) (*VirtualMachine, error)
	Delete(id string) error
	AttachVolume(id, path string, typ VolumeType, format VolumeFormat, device DeviceType) (*VirtualMachineAttachedVolume, error)
	DetachVolume(id, path string) error
	AttachInterface(id, network, mac, model string, accessVlan uint, netType NetworkType) (*VirtualMachineAttachedInterface, error)
	DetachInterface(id, mac string) error
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

type VirtualMachineCpuPin struct {
	Vcpus    map[uint][]uint
	Emulator []uint
}

type VirtualMachineConfig struct {
	Hostname string
	Keys     []*Key
	Userdata []byte
}

type VirtualMachine struct {
	Id         string
	VCpus      int
	Arch       Arch
	State      VirtualMachineState
	Memory     uint // KiB
	Interfaces []*VirtualMachineAttachedInterface
	Volumes    []*VirtualMachineAttachedVolume
	Config     *VirtualMachineConfig
	Cpupin     *VirtualMachineCpuPin
}

func (vm *VirtualMachine) AttachmentInfo(path string) *VirtualMachineAttachedVolume {
	for _, attachedVolume := range vm.Volumes {
		if attachedVolume.Path == path {
			return attachedVolume
		}
	}
	return nil
}

func (vm *VirtualMachine) IpAddressList() []string {
	iplist := []string{}
	for _, iface := range vm.Interfaces {
		iplist = append(iplist, iface.IpAddressList...)
	}
	return iplist
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
	Type          NetworkType
	Network       string
	Mac           string
	Model         string
	IpAddressList []string
	AccessVlan    uint
}
