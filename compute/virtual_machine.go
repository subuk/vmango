package compute

type VirtualMachineConsoleStream interface {
	Read(buf []byte) (int, error)
	Write(buf []byte) (int, error)
	Close() error
}

type VirtualMachineGraphicStream VirtualMachineConsoleStream

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

type VirtualMachineGraphic struct {
	Type   GraphicType
	Listen string
	Port   int
}

func (g VirtualMachineGraphic) Vnc() bool {
	return g.Type == GraphicTypeVnc
}

type VirtualMachine struct {
	Id         string
	Firmware   string
	NodeId     string
	VCpus      int
	Arch       Arch
	State      VirtualMachineState
	Memory     Size
	Interfaces []*VirtualMachineAttachedInterface
	Volumes    []*VirtualMachineAttachedVolume
	Config     *VirtualMachineConfig
	Cpupin     *VirtualMachineCpuPin
	GuestAgent bool
	Autostart  bool
	Graphic    VirtualMachineGraphic
	VideoModel VideoModel
	Hugepages  bool
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

type VirtualMachineAttachedVolume struct {
	Path       string
	Alias      string
	DeviceType DeviceType
	DeviceBus  DeviceBus
}

type VirtualMachineAttachedInterface struct {
	NetworkName   string
	Mac           string
	Model         string
	IpAddressList []string
	AccessVlan    uint
}
