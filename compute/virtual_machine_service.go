package compute

import "fmt"

type VirtualMachineListOptions struct {
	NodeIds []string
}

type VirtualMachineRepository interface {
	List(options VirtualMachineListOptions) ([]*VirtualMachine, error)
	Get(id, node string) (*VirtualMachine, error)
	Save(vm *VirtualMachine) error
	Delete(id, node string) error
	AttachVolume(id, nodeId string, attachedVolume *VirtualMachineAttachedVolume) error
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
