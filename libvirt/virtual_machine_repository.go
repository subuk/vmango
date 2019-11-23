package libvirt

import (
	"fmt"
	"io/ioutil"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/configdrive"
	"subuk/vmango/util"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/google/uuid"
	"github.com/libvirt/libvirt-go"
	libvirtxml "github.com/libvirt/libvirt-go-xml"
	"github.com/rs/zerolog"
)

const ConfigDriveMaxSize = 5 * 1024 * 1024

type VirtualMachineRepository struct {
	pool                   *ConnectionPool
	logger                 zerolog.Logger
	configDriveVolumePool  string
	configDriveSuffix      string
	configDriveWriteFormat configdrive.Format
	configCache            map[string]*compute.VirtualMachineConfig
	configCacheMu          *sync.Mutex
}

func NewVirtualMachineRepository(pool *ConnectionPool, configDriveVolumePool, configDriveSuffix string, configDriveWriteFormat configdrive.Format, logger zerolog.Logger) *VirtualMachineRepository {
	if configDriveSuffix == "" {
		panic("No config drive suffix specified")
	}
	return &VirtualMachineRepository{
		pool:                   pool,
		logger:                 logger,
		configDriveVolumePool:  configDriveVolumePool,
		configDriveSuffix:      configDriveSuffix,
		configDriveWriteFormat: configDriveWriteFormat,
		configCache:            map[string]*compute.VirtualMachineConfig{},
		configCacheMu:          &sync.Mutex{},
	}
}

func (repo *VirtualMachineRepository) generateConfigDrive(conn *libvirt.Connect, domainConfig *libvirtxml.Domain, config *compute.VirtualMachineConfig) (*compute.VirtualMachineAttachedVolume, error) {
	virPool, err := conn.LookupStoragePoolByName(repo.configDriveVolumePool)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup configdrive storage pool")
	}

	configVolumeName := strings.Replace(domainConfig.Name, "-", "_", -1) + repo.configDriveSuffix
	if existingVolume, err := virPool.LookupStorageVolByName(configVolumeName); err == nil && existingVolume != nil {
		existingVolumeInfo, err := existingVolume.GetInfo()
		if err == nil && existingVolumeInfo.Capacity <= 2*1024*1024 {
			if err := existingVolume.Delete(libvirt.STORAGE_VOL_DELETE_NORMAL); err != nil {
				return nil, util.NewError(err, "cannot delete existing configdrive volume")
			}
		}
	}

	var data configdrive.Data
	switch repo.configDriveWriteFormat {
	default:
		panic(fmt.Errorf("unknown configdrive write format '%s'", repo.configDriveWriteFormat))
	case configdrive.FormatOpenstack:
		osData := &configdrive.Openstack{
			Userdata: config.Userdata,
			Metadata: configdrive.OpenstackMetadata{
				Az:          "none",
				Files:       []struct{}{},
				Hostname:    config.Hostname,
				LaunchIndex: 0,
				Name:        config.Hostname,
				Meta:        map[string]string{},
				PublicKeys:  map[string]string{},
				UUID:        domainConfig.UUID,
			},
		}
		for _, key := range config.Keys {
			osData.Metadata.PublicKeys[key.Comment] = string(key.Value)
		}
		data = osData
	case configdrive.FormatNoCloud:
		nocloudData := &configdrive.NoCloud{
			Userdata: config.Userdata,
			Metadata: configdrive.NoCloudMetadata{
				InstanceId:    domainConfig.UUID,
				Hostname:      config.Hostname,
				LocalHostname: config.Hostname,
			},
		}
		for _, key := range config.Keys {
			nocloudData.Metadata.PublicKeys = append(nocloudData.Metadata.PublicKeys, string(key.Value))
		}
		data = nocloudData
	}

	localConfigDriveFile, err := configdrive.GenerateIso(data)
	if err != nil {
		return nil, util.NewError(err, "cannot generate iso")
	}
	configdriveContent, err := ioutil.ReadAll(localConfigDriveFile)
	if err != nil {
		return nil, util.NewError(err, "cannot read local configdrive file")
	}

	virVolumeConfig := &libvirtxml.StorageVolume{}
	virVolumeConfig.Name = configVolumeName
	virVolumeConfig.Target = &libvirtxml.StorageVolumeTarget{Format: &libvirtxml.StorageVolumeTargetFormat{Type: "raw"}}
	virVolumeConfig.Capacity = &libvirtxml.StorageVolumeSize{Unit: "bytes", Value: uint64(len(configdriveContent))}
	virVolumeXml, err := virVolumeConfig.Marshal()
	if err != nil {
		return nil, util.NewError(err, "cannot marshal configdrive volume to xml")
	}
	virVolume, err := virPool.StorageVolCreateXML(virVolumeXml, 0)
	if err != nil {
		return nil, util.NewError(err, "cannot create configdrive storage volume")
	}
	stream, err := conn.NewStream(0)
	if err != nil {
		return nil, util.NewError(err, "cannot initialize configdrive upload stream")
	}
	if err := virVolume.Upload(stream, 0, uint64(len(configdriveContent)), 0); err != nil {
		return nil, util.NewError(err, "cannot start configdrive upload")
	}
	if _, err := stream.Send(configdriveContent); err != nil {
		return nil, util.NewError(err, "configdrive upload failed")
	}
	if err := stream.Finish(); err != nil {
		return nil, util.NewError(err, "cannot finalize configdrive upload")
	}

	virVolumeXml, err = virVolume.GetXMLDesc(0)
	if err != nil {
		return nil, util.NewError(err, "cannot get volume info")
	}
	virVolumeConfig = &libvirtxml.StorageVolume{}
	if err := virVolumeConfig.Unmarshal(virVolumeXml); err != nil {
		return nil, util.NewError(err, "cannot unmarshal volume xml")
	}
	if virVolumeConfig.Target == nil {
		return nil, fmt.Errorf("configdrive volume target element is blank")
	}
	attachedVolume := &compute.VirtualMachineAttachedVolume{
		Type:   compute.VolumeTypeFile,
		Format: compute.FormatIso,
		Path:   virVolumeConfig.Target.Path,
		Device: compute.DeviceTypeCdrom,
	}
	repo.configCacheMu.Lock()
	repo.configCache[attachedVolume.Path] = nil
	repo.configCacheMu.Unlock()
	return attachedVolume, nil
}

type virStreamReader struct {
	*libvirt.Stream
}

func (r *virStreamReader) Read(b []byte) (int, error) {
	return r.Recv(b)
}

func (repo *VirtualMachineRepository) parseConfigDrive(conn *libvirt.Connect, volumePath string) (*compute.VirtualMachineConfig, error) {
	virVolume, err := conn.LookupStorageVolByPath(volumePath)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup volume")
	}
	stream, err := conn.NewStream(0)
	if err != nil {
		return nil, util.NewError(err, "cannot initialize configdrive download stream")
	}
	if err := virVolume.Download(stream, 0, ConfigDriveMaxSize, 0); err != nil {
		return nil, util.NewError(err, "cannot start configdrive download")
	}
	data, err := configdrive.ParseIso(configdrive.AllFormats, &virStreamReader{stream})
	if err != nil {
		return nil, util.NewError(err, "cannot parse configdrive iso")
	}
	config := &compute.VirtualMachineConfig{
		Hostname: data.Hostname(),
	}
	for _, rawKey := range data.PublicKeys() {
		pubkey, comment, options, _, err := ssh.ParseAuthorizedKey([]byte(rawKey))
		if err != nil {
			repo.logger.Warn().Msg("ignoring invalid ssh key")
			continue
		}
		key := &compute.Key{
			Type:        pubkey.Type(),
			Value:       []byte(rawKey),
			Comment:     comment,
			Options:     options,
			Fingerprint: ssh.FingerprintLegacyMD5(pubkey),
		}
		config.Keys = append(config.Keys, key)
	}
	return config, nil
}

func (repo *VirtualMachineRepository) domainToVm(conn *libvirt.Connect, domain *libvirt.Domain) (*compute.VirtualMachine, error) {
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
	if vm.IsRunning() {
		for _, attachedInterface := range vm.Interfaces {
			virDomainIfaces, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_AGENT)
			if err != nil {
				repo.logger.Debug().Str("vm", vm.Id).Err(err).Msg("cannot get interfaces addresses with qemu guest agent")
				virDomainIfacesLease, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)
				if err != nil {
					repo.logger.Debug().Str("vm", vm.Id).Err(err).Msg("cannot get interfaces addresses with dhcp leases")
					continue
				}
				virDomainIfaces = virDomainIfacesLease
			}
			for _, virDomainIface := range virDomainIfaces {
				if virDomainIface.Hwaddr == attachedInterface.Mac {
					for _, addr := range virDomainIface.Addrs {
						attachedInterface.IpAddressList = append(attachedInterface.IpAddressList, addr.Addr)
					}
				}
			}
		}
	}

	noConfigDriveVolumes := []*compute.VirtualMachineAttachedVolume{}
	for _, volume := range vm.Volumes {
		if volume.Device != compute.DeviceTypeCdrom {
			noConfigDriveVolumes = append(noConfigDriveVolumes, volume)
			continue
		}
		if !strings.HasSuffix(volume.Path, repo.configDriveSuffix) {
			noConfigDriveVolumes = append(noConfigDriveVolumes, volume)
			continue
		}
		if config := repo.configCache[volume.Path]; config != nil {
			vm.Config = config
		} else {
			config, err := repo.parseConfigDrive(conn, volume.Path)
			if err != nil {
				repo.logger.Warn().Err(err).Str("volume_path", volume.Path).Msg("cannot parse configdrive")
				continue
			}
			vm.Config = config
			repo.configCacheMu.Lock()
			repo.configCache[volume.Path] = config
			repo.configCacheMu.Unlock()
		}
	}
	vm.Volumes = noConfigDriveVolumes

	return vm, nil
}

func (repo *VirtualMachineRepository) attachVolume(virDomainConfig *libvirtxml.Domain, volume *compute.VirtualMachineAttachedVolume) error {
	disk := libvirtxml.DomainDisk{
		Driver: &libvirtxml.DomainDiskDriver{Name: "qemu"},
		Target: &libvirtxml.DomainDiskTarget{},
	}
	switch volume.Format {
	default:
		return fmt.Errorf("unsupported volume format '%s'", volume.Format)
	case compute.FormatQcow2:
		disk.Driver.Type = "qcow2"
	case compute.FormatRaw:
		disk.Driver.Type = "raw"
	case compute.FormatIso:
		disk.Driver.Type = "raw"
	}
	hdLetter := 'a'
	vdLetter := 'c'
	for _, disk := range virDomainConfig.Devices.Disks {
		if disk.Target != nil {
			if disk.Target.Dev == "" {
				continue
			}
			if strings.HasPrefix(disk.Target.Dev, "hd") {
				hdLetter++
			} else {
				vdLetter++
			}
		}
	}
	switch volume.Device {
	default:
		return fmt.Errorf("unsupported volume device type '%s'", volume.Device)
	case compute.DeviceTypeCdrom:
		disk.Device = "cdrom"
		disk.ReadOnly = &libvirtxml.DomainDiskReadOnly{}
		disk.Target.Bus = "ide"
		disk.Target.Dev = "hd" + string(hdLetter)
	case compute.DeviceTypeDisk:
		disk.Device = "disk"
		disk.Target.Bus = "virtio"
		disk.Target.Dev = "vd" + string(vdLetter)
	}
	switch volume.Type {
	default:
		return fmt.Errorf("unknown volume type '%s'", volume.Type)
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
	return nil
}

func (repo *VirtualMachineRepository) attachInterface(virDomainConfig *libvirtxml.Domain, attachedIface *compute.VirtualMachineAttachedInterface) error {
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
		return fmt.Errorf("unsupported interface type %s", attachedIface.Type)
	case compute.NetworkTypeLibvirt:
		domainIface.Source.Network = &libvirtxml.DomainInterfaceSourceNetwork{
			Network: attachedIface.Network,
		}
	case compute.NetworkTypeBridge:
		domainIface.Source.Bridge = &libvirtxml.DomainInterfaceSourceBridge{
			Bridge: attachedIface.Network,
		}
	}
	if attachedIface.AccessVlan > 0 {
		domainIface.VLan = &libvirtxml.DomainInterfaceVLan{
			Tags: []libvirtxml.DomainInterfaceVLanTag{libvirtxml.DomainInterfaceVLanTag{ID: attachedIface.AccessVlan}},
		}
	}
	virDomainConfig.Devices.Interfaces = append(virDomainConfig.Devices.Interfaces, domainIface)
	return nil
}

type virStreamReadWriteCloser struct {
	*libvirt.Stream
}

func (r *virStreamReadWriteCloser) Read(b []byte) (int, error) {
	return r.Recv(b)
}

func (r *virStreamReadWriteCloser) Write(b []byte) (int, error) {
	return r.Send(b)
}

func (r *virStreamReadWriteCloser) Close() error {
	return r.Stream.Finish()
}

func (repo *VirtualMachineRepository) GetConsoleStream(id string) (compute.VirtualMachineConsoleStream, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(conn)

	virDomain, err := conn.LookupDomainByName(id)
	if err != nil {
		return nil, util.NewError(err, "cannot get vm")
	}
	stream, err := conn.NewStream(0)
	if err != nil {
		return nil, util.NewError(err, "cannot create stream")
	}
	if err := virDomain.OpenConsole("", stream, libvirt.DOMAIN_CONSOLE_FORCE); err != nil {
		return nil, util.NewError(err, "cannot open domain console")
	}
	return &virStreamReadWriteCloser{stream}, nil
}

func (repo *VirtualMachineRepository) Create(id string, arch compute.Arch, vcpus int, memoryKb uint, volumes []*compute.VirtualMachineAttachedVolume, interfaces []*compute.VirtualMachineAttachedInterface, config *compute.VirtualMachineConfig) (*compute.VirtualMachine, error) {
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
	virDomainConfig.UUID = uuid.New().String()
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
	virDomainConfig.Devices.Channels = append(virDomainConfig.Devices.Channels, libvirtxml.DomainChannel{
		Protocol: &libvirtxml.DomainChardevProtocol{Type: "unix"},
		Target:   &libvirtxml.DomainChannelTarget{VirtIO: &libvirtxml.DomainChannelTargetVirtIO{Name: "org.qemu.guest_agent.0"}},
	})
	configVolume, err := repo.generateConfigDrive(conn, virDomainConfig, config)
	if err != nil {
		return nil, util.NewError(err, "cannot generate config drive")
	}
	volumes = append(volumes, configVolume)

	for _, volume := range volumes {
		if err := repo.attachVolume(virDomainConfig, volume); err != nil {
			return nil, util.NewError(err, "cannot attach volume")
		}
	}
	for _, attachedIface := range interfaces {
		if err := repo.attachInterface(virDomainConfig, attachedIface); err != nil {
			return nil, util.NewError(err, "cannot attach interface")
		}
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
	vm, err := repo.domainToVm(conn, virDomain)
	if err != nil {
		return nil, util.NewError(err, "cannot parse domain to vm")
	}
	return vm, nil
}

func (repo *VirtualMachineRepository) Update(id string, vcpus int, memoryKb uint) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(conn)

	virDomain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "lookup domain failed")
	}
	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_MIGRATABLE)
	if err != nil {
		return util.NewError(err, "cannot fetch domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return util.NewError(err, "cannot unmarshal domain xml")
	}
	if vcpus > 0 {
		virDomainConfig.VCPU.Value = vcpus
	}
	if memoryKb > 0 {
		virDomainConfig.Memory.Value = memoryKb
		virDomainConfig.Memory.Unit = "kib"
	}
	virDomainXmlUpdated, err := virDomainConfig.Marshal()
	if err != nil {
		return util.NewError(err, "cannot marshal updated domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXmlUpdated); err != nil {
		return util.NewError(err, "cannot update domain xml")
	}
	return nil
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
	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_MIGRATABLE)
	if err != nil {
		return util.NewError(err, "cannot fetch domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return util.NewError(err, "cannot parse domain xml")
	}
	if virDomainRunning {
		if err := virDomain.Destroy(); err != nil {
			return util.NewError(err, "cannot destroy domain")
		}
	}
	if err := virDomain.Undefine(); err != nil {
		return util.NewError(err, "cannot undefine domain")
	}
	for _, diskConfig := range virDomainConfig.Devices.Disks {
		volume := VirtualMachineAttachedVolumeFromDomainDiskConfig(diskConfig)
		if volume.Device != compute.DeviceTypeCdrom {
			continue
		}
		if !strings.HasSuffix(volume.Path, repo.configDriveSuffix) {
			continue
		}
		virVolume, err := conn.LookupStorageVolByPath(volume.Path)
		if err != nil {
			return util.NewError(err, "cannot lookup config volume")
		}
		if err := virVolume.Delete(libvirt.STORAGE_VOL_DELETE_NORMAL); err != nil {
			return util.NewError(err, "cannot delete config volume")
		}
		return nil
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
		vm, err := repo.domainToVm(conn, &domain)
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
	vm, err := repo.domainToVm(conn, domain)
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

func (repo *VirtualMachineRepository) EnableGuestAgent(id string) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virDomain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return util.NewError(err, "cannot check if domain is running")
	}
	if running {
		return fmt.Errorf("domain must be stopped")
	}
	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_MIGRATABLE)
	if err != nil {
		return util.NewError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return util.NewError(err, "cannot parse domain xml")
	}
	virDomainConfig.Devices.Channels = append(virDomainConfig.Devices.Channels, libvirtxml.DomainChannel{
		Protocol: &libvirtxml.DomainChardevProtocol{Type: "unix"},
		Target:   &libvirtxml.DomainChannelTarget{VirtIO: &libvirtxml.DomainChannelTargetVirtIO{Name: "org.qemu.guest_agent.0"}},
		Source:   &libvirtxml.DomainChardevSource{UNIX: &libvirtxml.DomainChardevSourceUNIX{}},
	})
	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return util.NewError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return util.NewError(err, "cannot update domain xml")
	}
	return nil
}

func (repo *VirtualMachineRepository) DisableGuestAgent(id string) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virDomain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return util.NewError(err, "cannot check if domain is running")
	}
	if running {
		return fmt.Errorf("domain must be stopped")
	}
	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_MIGRATABLE)
	if err != nil {
		return util.NewError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return util.NewError(err, "cannot parse domain xml")
	}
	newChannels := []libvirtxml.DomainChannel{}
	for _, channel := range virDomainConfig.Devices.Channels {
		if channel.Target != nil && channel.Target.VirtIO != nil && channel.Target.VirtIO.Name == "org.qemu.guest_agent.0" {
			continue
		}
		newChannels = append(newChannels, channel)
	}
	virDomainConfig.Devices.Channels = newChannels

	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return util.NewError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return util.NewError(err, "cannot update domain xml")
	}
	return nil
}

func (repo *VirtualMachineRepository) AttachVolume(id, path string, typ compute.VolumeType, format compute.VolumeFormat, device compute.DeviceType) (*compute.VirtualMachineAttachedVolume, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virDomain, err := conn.LookupDomainByName(id)
	if err != nil {
		return nil, util.NewError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return nil, util.NewError(err, "cannot check if domain is running")
	}
	if running {
		return nil, fmt.Errorf("domain must be stopped")
	}

	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_MIGRATABLE)
	if err != nil {
		return nil, util.NewError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return nil, util.NewError(err, "cannot parse domain xml")
	}
	attachedVolume := &compute.VirtualMachineAttachedVolume{
		Type:   typ,
		Path:   path,
		Format: format,
		Device: compute.DeviceTypeDisk,
	}
	if err := repo.attachVolume(virDomainConfig, attachedVolume); err != nil {
		return nil, err
	}
	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return nil, util.NewError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return nil, util.NewError(err, "cannot update domain xml")
	}
	return attachedVolume, nil
}

func (repo *VirtualMachineRepository) DetachVolume(id, needlePath string) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virDomain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return util.NewError(err, "cannot check if domain is running")
	}
	if running {
		return fmt.Errorf("domain must be stopped")
	}

	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_MIGRATABLE)
	if err != nil {
		return util.NewError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return util.NewError(err, "cannot parse domain xml")
	}
	if virDomainConfig.Devices.Disks == nil {
		return fmt.Errorf("no disk found")
	}
	newDisks := []libvirtxml.DomainDisk{}
	needleFound := false
	for _, disk := range virDomainConfig.Devices.Disks {
		volume := VirtualMachineAttachedVolumeFromDomainDiskConfig(disk)
		if volume.Path == needlePath {
			needleFound = true
			continue
		}
		newDisks = append(newDisks, disk)
	}
	if !needleFound {
		return fmt.Errorf("no disk found")
	}
	virDomainConfig.Devices.Disks = newDisks
	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return util.NewError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return util.NewError(err, "cannot update domain")
	}
	return nil
}

func (repo *VirtualMachineRepository) AttachInterface(id, network, mac, model string, accessVlan uint, ifaceType compute.NetworkType) (*compute.VirtualMachineAttachedInterface, error) {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virDomain, err := conn.LookupDomainByName(id)
	if err != nil {
		return nil, util.NewError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return nil, util.NewError(err, "cannot check if domain is running")
	}
	if running {
		return nil, fmt.Errorf("domain must be stopped")
	}

	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_MIGRATABLE)
	if err != nil {
		return nil, util.NewError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return nil, util.NewError(err, "cannot parse domain xml")
	}
	attachedIface := &compute.VirtualMachineAttachedInterface{
		Type:       ifaceType,
		Network:    network,
		Mac:        mac,
		Model:      model,
		AccessVlan: accessVlan,
	}
	if err := repo.attachInterface(virDomainConfig, attachedIface); err != nil {
		return nil, err
	}
	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return nil, util.NewError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return nil, util.NewError(err, "cannot update domain xml")
	}
	return attachedIface, nil
}

func (repo *VirtualMachineRepository) DetachInterface(id, needleMac string) error {
	conn, err := repo.pool.Acquire()
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(conn)

	virDomain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return util.NewError(err, "cannot check if domain is running")
	}
	if running {
		return fmt.Errorf("domain must be stopped")
	}

	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_MIGRATABLE)
	if err != nil {
		return util.NewError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return util.NewError(err, "cannot parse domain xml")
	}
	if virDomainConfig.Devices.Interfaces == nil {
		return fmt.Errorf("no interface found")
	}
	newInterfaces := []libvirtxml.DomainInterface{}
	needleFound := false
	for _, ifaceConfig := range virDomainConfig.Devices.Interfaces {
		iface := VirtualMachineAttachedInterfaceFromInterfaceConfig(ifaceConfig)
		if iface.Mac == needleMac {
			needleFound = true
			continue
		}
		newInterfaces = append(newInterfaces, ifaceConfig)
	}
	if !needleFound {
		return fmt.Errorf("no interface found")
	}
	virDomainConfig.Devices.Interfaces = newInterfaces
	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return util.NewError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return util.NewError(err, "cannot update domain")
	}
	return nil
}
