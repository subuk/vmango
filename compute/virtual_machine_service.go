package compute

import "fmt"

type VirtualMachineUpdateParams struct {
	Vcpus         *int
	Memory        *Size
	Autostart     *bool
	GuestAgent    *bool
	GraphicType   *GraphicType
	GraphicListen *string
}

type VirtualMachineRepository interface {
	List() ([]*VirtualMachine, error)
	Get(id, node string) (*VirtualMachine, error)
	Create(id, node string, arch Arch, vcpus int, memory Size, volumes []*VirtualMachineAttachedVolume, interfaces []*VirtualMachineAttachedInterface, config *VirtualMachineConfig) (*VirtualMachine, error)
	Delete(id, node string) error
	Update(id, node string, params VirtualMachineUpdateParams) error
	AttachVolume(machineId, node string, attachedVolume *VirtualMachineAttachedVolume) error
	DetachVolume(machineId, node, attachmentDeviceName string) error
	AttachInterface(id, node string, iface *VirtualMachineAttachedInterface) error
	DetachInterface(id, node, mac string) error
	GetConsoleStream(id, node string) (VirtualMachineConsoleStream, error)
	GetGraphicStream(id, node string) (VirtualMachineGraphicStream, error)
	Poweroff(id, node string) error
	Reboot(id, node string) error
	Start(id, node string) error
}

type VirtualMachineService struct {
	VirtualMachineRepository
}

func NewVirtualMachineService(repo VirtualMachineRepository) *VirtualMachineService {
	return &VirtualMachineService{repo}
}

func (service *VirtualMachineService) Action(id string, node, action string) error {
	switch action {
	default:
		return fmt.Errorf("unknown action %s", action)
	case "reboot":
		return service.VirtualMachineRepository.Reboot(id, node)
	case "poweroff":
		return service.VirtualMachineRepository.Poweroff(id, node)
	case "start":
		return service.VirtualMachineRepository.Start(id, node)
	}
}
