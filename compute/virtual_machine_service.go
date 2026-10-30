package compute

import "fmt"

type VirtualMachineListOptions struct {
	NodeIds []string
	Strict  bool
}

type VirtualMachineRepository interface {
	ValidateNew(vm *VirtualMachine) error
	Capabilities(node string, arch Arch) (VirtualMachineCapabilities, error)
	List(options VirtualMachineListOptions) ([]*VirtualMachine, error)
	Get(id, node string) (*VirtualMachine, error)
	Save(vm *VirtualMachine) error
	Delete(id, node string) error
	AttachVolume(id, nodeId string, attachedVolume *VirtualMachineAttachedVolume) error
	DetachVolume(machineId, node, attachmentDeviceName string) error
	AttachInterface(id, node string, iface *VirtualMachineAttachedInterface) error
	DetachInterface(id, node, mac string) error
	GetConsoleStream(id, node string, force bool) (VirtualMachineConsoleStream, error)
	GetGraphicStream(id, node string) (VirtualMachineGraphicStream, error)
	Poweroff(id, node string) error
	Reboot(id, node string) error
	Start(id, node string) error
}

// MachineAdoptionRepository records management metadata without reconfiguring a VM.
type MachineAdoptionRepository interface {
	Adopt(id, node string, metadata *MachineMetadata) error
}
type NetworkValidator interface {
	ValidateNetwork(node string, nic *VirtualMachineAttachedInterface) error
}

type VirtualMachineService struct {
	VirtualMachineRepository
	CloudMode bool
}

func NewVirtualMachineService(repo VirtualMachineRepository) *VirtualMachineService {
	return &VirtualMachineService{VirtualMachineRepository: repo}
}

func (service *VirtualMachineService) Action(id string, node, action string) error {
	if err := service.RequireManaged(id, node); err != nil {
		return err
	}
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

// VirtualMachineCapabilities exposes supported launch options without backend XML.
type VirtualMachineCapabilities struct {
	Arch            Arch
	MaxVCPUs        uint
	Firmwares       []string
	DiskBuses       []DeviceBus
	NICModels       []string
	NetworkModels   map[string][]string
	DiskControllers []string
	Graphics        []GraphicType
	VideoModels     []VideoModel
}

// MachineResourceRepository preserves all non-resource guest configuration when
// applying an explicit instance type, including backend-only XML attributes.
type MachineResourceRepository interface {
	SaveResources(vm *VirtualMachine) error
}

func (s *VirtualMachineService) SaveResources(vm *VirtualMachine) error {
	if repo, ok := s.VirtualMachineRepository.(MachineResourceRepository); ok {
		return repo.SaveResources(vm)
	}
	return fmt.Errorf("%w: resource-only machine updates are unavailable", ErrUnavailable)
}
