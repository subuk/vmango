package libvirt

import (
	"fmt"
	"subuk/vmango/compute"
	"subuk/vmango/util"

	"github.com/libvirt/libvirt-go"
	"github.com/libvirt/libvirt-go-xml"
)

type VirtualMachineRepository struct {
	pool *ConnectionPool
}

func NewVirtualMachineRepository(pool *ConnectionPool) *VirtualMachineRepository {
	return &VirtualMachineRepository{pool: pool}
}

func (repo *VirtualMachineRepository) domainToVm(domain *libvirt.Domain) (*compute.VirtualMachine, error) {
	domainXml, err := domain.GetXMLDesc(libvirt.DOMAIN_XML_MIGRATABLE)
	if err != nil {
		return nil, util.NewError(err, "cannot get domain xml")
	}
	domainConfig := &libvirtxml.Domain{}
	if err := domainConfig.Unmarshal(domainXml); err != nil {
		return nil, util.NewError(err, "cannot unmarshal domain xml")
	}
	domainInfo, err := domain.GetInfo()
	if err != nil {
		return nil, util.NewError(err, "cannot get domain info")
	}
	vm, err := VirtualMachineFromDomainConfig(domainConfig, domainInfo)
	if err != nil {
		return nil, util.NewError(err, "cannot create virtual machine from domain config")
	}
	return vm, nil
}

func (repo *VirtualMachineRepository) Create(id string, arch compute.Arch, vcpus int, memoryKb uint, volumes []*compute.VirtualMachineAttachedVolume, interfaces []*compute.VirtualMachineAttachedInterface) (*compute.VirtualMachine, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(conn)

	libvirtArch := ""
	switch arch {
	default:
		return nil, compute.ErrArchNotsupported
	case compute.ArchAmd64:
		libvirtArch = "x86_64"
	}

	domCapsXml, err := conn.GetDomainCapabilities("", libvirtArch, "", "", 0)
	if err != nil {
		return nil, util.NewError(err, "cannot fetch domain capabilities")
	}
	domCapsConfig := &libvirtxml.DomainCaps{}
	if err := domCapsConfig.Unmarshal(domCapsXml); err != nil {
		return nil, util.NewError(err, "cannot parse domain capabilities")
	}

	features := map[string]bool{}
	for _, mode := range domCapsConfig.CPU.Modes {
		if mode.Name == "host-passthrough" && mode.Supported == "yes" {
			features["host-cpu-passthrough"] = true
		}
	}
	if !features["host-cpu-passthrough"] {
		return nil, fmt.Errorf("host cpu passthrough is not supported")
	}

	virDomainConfig := &libvirtxml.Domain{}
	virDomainConfig.Type = domCapsConfig.Domain
	virDomainConfig.Name = id
	virDomainConfig.VCPU = &libvirtxml.DomainVCPU{Placement: "static", Value: vcpus}
	virDomainConfig.Memory = &libvirtxml.DomainMemory{Unit: "KiB", Value: memoryKb}
	virDomainConfig.OS = &libvirtxml.DomainOS{
		Type: &libvirtxml.DomainOSType{Type: "hvm", Machine: domCapsConfig.Machine, Arch: libvirtArch},
		BootDevices: []libvirtxml.DomainBootDevice{
			{Dev: "hd"},
		},
	}
	virDomainConfig.Features = &libvirtxml.DomainFeatureList{
		ACPI: &libvirtxml.DomainFeature{},
		APIC: &libvirtxml.DomainFeatureAPIC{},
	}
	virDomainConfig.CPU = &libvirtxml.DomainCPU{}
	if features["host-cpu-passthrough"] {
		virDomainConfig.CPU.Mode = "host-passthrough"
	}
	virDomainConfig.Clock = &libvirtxml.DomainClock{Offset: "utc"}
	virDomainConfig.OnPoweroff = "destroy"
	virDomainConfig.OnReboot = "restart"
	virDomainConfig.OnCrash = "destroy"

	virDomainConfig.Devices = &libvirtxml.DomainDeviceList{Emulator: domCapsConfig.Path}
	virDomainConfig.Devices.Consoles = append(virDomainConfig.Devices.Consoles, libvirtxml.DomainConsole{})

	diskLastLetter := 'a'
	cdromLastLetter := 'c'
	for _, volume := range volumes {
		disk := libvirtxml.DomainDisk{
			Driver: &libvirtxml.DomainDiskDriver{Name: "qemu"},
			Target: &libvirtxml.DomainDiskTarget{},
		}
		switch volume.Format {
		default:
			return nil, fmt.Errorf("unsupported volume format '%s'", volume.Format)
		case compute.FormatQcow2:
			disk.Driver.Type = "qcow2"
		case compute.FormatRaw:
			disk.Driver.Type = "raw"
		case compute.FormatIso:
			disk.Driver.Type = "raw"
		}
		switch volume.Device {
		default:
			return nil, fmt.Errorf("unsupported volume device type '%s'", volume.Device)
		case compute.DeviceTypeCdrom:
			disk.Device = "cdrom"
			disk.ReadOnly = &libvirtxml.DomainDiskReadOnly{}
			disk.Target.Bus = "ide"
			disk.Target.Dev = "hd" + string(cdromLastLetter)
			cdromLastLetter++
		case compute.DeviceTypeDisk:
			disk.Device = "disk"
			disk.Target.Bus = "virtio"
			disk.Target.Dev = "vd" + string(diskLastLetter)
			diskLastLetter++
		}
		switch volume.Type {
		default:
			return nil, fmt.Errorf("unknown volume type '%s'", volume.Type)
		case compute.VolumeTypeFile:
			disk.Source = &libvirtxml.DomainDiskSource{
				File: &libvirtxml.DomainDiskSourceFile{File: volume.Path},
			}
		case compute.VolumeTypeBlock:
			disk.Source = &libvirtxml.DomainDiskSource{
				Block: &libvirtxml.DomainDiskSourceBlock{Dev: volume.Path},
			}
		}
		virDomainConfig.Devices.Disks = append(virDomainConfig.Devices.Disks, disk)
	}

	for _, attachedIface := range interfaces {
		if attachedIface.Model == "" {
			attachedIface.Model = "virtio"
		}
		domainIface := libvirtxml.DomainInterface{}
		if attachedIface.Mac != "" {
			domainIface.MAC = &libvirtxml.DomainInterfaceMAC{Address: attachedIface.Mac}
		}
		domainIface.Source = &libvirtxml.DomainInterfaceSource{}
		domainIface.Model = &libvirtxml.DomainInterfaceModel{Type: attachedIface.Model}
		switch attachedIface.Type {
		default:
			return nil, fmt.Errorf("unsupported interface type %s", attachedIface.Type)
		case compute.NetworkTypeLibvirt:
			domainIface.Source.Network = &libvirtxml.DomainInterfaceSourceNetwork{
				Network: attachedIface.Network,
			}
		case compute.NetworkTypeBridge:
			domainIface.Source.Bridge = &libvirtxml.DomainInterfaceSourceBridge{
				Bridge: attachedIface.Network,
			}
		}
		virDomainConfig.Devices.Interfaces = append(virDomainConfig.Devices.Interfaces, domainIface)
	}
	virDomainXml, err := virDomainConfig.Marshal()
	if err != nil {
		return nil, util.NewError(err, "cannot marshal domain xml")
	}
	virDomain, err := conn.DomainDefineXML(virDomainXml)
	if err != nil {
		fmt.Println(virDomainXml)
		return nil, util.NewError(err, "cannot define new domain")
	}
	vm, err := repo.domainToVm(virDomain)
	if err != nil {
		return nil, util.NewError(err, "cannot parse domain to vm")
	}
	return vm, nil
}

func (repo *VirtualMachineRepository) Delete(id string) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(conn)

	virDomain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "lookup domain failed")
	}
	virDomainRunning, err := virDomain.IsActive()
	if err != nil {
		return util.NewError(err, "cannot check if domain is running")
	}
	if virDomainRunning {
		if err := virDomain.Destroy(); err != nil {
			return util.NewError(err, "cannot destroy domain")
		}
	}
	if err := virDomain.Undefine(); err != nil {
		return util.NewError(err, "cannot undefine domain")
	}
	return nil
}

func (repo *VirtualMachineRepository) List() ([]*compute.VirtualMachine, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(conn)

	vms := []*compute.VirtualMachine{}
	domains, err := conn.ListAllDomains(0)
	for _, domain := range domains {
		vm, err := repo.domainToVm(&domain)
		if err != nil {
			return nil, util.NewError(err, "cannot convert libvirt domain to vm")
		}
		vms = append(vms, vm)
	}
	return vms, nil
}

func (repo *VirtualMachineRepository) Get(id string) (*compute.VirtualMachine, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	domain, err := conn.LookupDomainByName(id)
	if err != nil {
		return nil, util.NewError(err, "failed to lookup vm")
	}
	vm, err := repo.domainToVm(domain)
	if err != nil {
		return nil, util.NewError(err, "cannot convert libvirt domain to vm")
	}
	return vm, nil
}

func (repo *VirtualMachineRepository) Poweroff(id string) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	domain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "domain lookup failed")
	}
	return domain.Destroy()
}

func (repo *VirtualMachineRepository) Reboot(id string) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	domain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "domain lookup failed")
	}
	return domain.Reboot(libvirt.DOMAIN_REBOOT_DEFAULT)
}

func (repo *VirtualMachineRepository) Start(id string) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	domain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "domain lookup failed")
	}
	return domain.Create()
}
