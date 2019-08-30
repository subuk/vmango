package libvirt

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/util"

	"golang.org/x/crypto/ssh"

	"github.com/google/uuid"
	"github.com/libvirt/libvirt-go"
	"github.com/libvirt/libvirt-go-xml"
	"github.com/rs/zerolog"
)

const ConfigDriveMaxSize = 5 * 1024 * 1024

type VirtualMachineRepository struct {
	pool                  *ConnectionPool
	logger                zerolog.Logger
	configDriveVolumePool string
	configDriveSuffix     string
}

func NewVirtualMachineRepository(pool *ConnectionPool, configDriveVolumePool, configDriveSuffix string, logger zerolog.Logger) *VirtualMachineRepository {
	if configDriveSuffix == "" {
		panic("No config drive suffix specified")
	}
	return &VirtualMachineRepository{pool: pool, logger: logger, configDriveVolumePool: configDriveVolumePool, configDriveSuffix: configDriveSuffix}
}

func (repo *VirtualMachineRepository) generateConfigDrive(conn *libvirt.Connect, domainConfig *libvirtxml.Domain, config *compute.VirtualMachineConfig) (*compute.VirtualMachineAttachedVolume, error) {
	md := &CloudInitMetadata{
		InstanceId:    domainConfig.UUID,
		Hostname:      config.Hostname,
		LocalHostname: config.Hostname,
	}
	for _, key := range config.Keys {
		md.PublicKeys = append(md.PublicKeys, string(key.Value))
	}
	mdBytes, err := md.Marshal()
	if err != nil {
		return nil, util.NewError(err, "cannot marshal config metadata")
	}

	virPool, err := conn.LookupStoragePoolByName(repo.configDriveVolumePool)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup configdrive storage pool")
	}

	configVolumeName := strings.ReplaceAll(domainConfig.Name, "-", "_") + repo.configDriveSuffix

	if existingVolume, err := virPool.LookupStorageVolByName(configVolumeName); err == nil && existingVolume != nil {
		existingVolumeInfo, err := existingVolume.GetInfo()
		if err == nil && existingVolumeInfo.Capacity <= 2*1024*1024 {
			if err := existingVolume.Delete(libvirt.STORAGE_VOL_DELETE_NORMAL); err != nil {
				return nil, util.NewError(err, "cannot delete existing configdrive volume")
			}
		}
	}

	tmpdir, err := ioutil.TempDir("", "vmango-configdrive-content")
	if err != nil {
		return nil, util.NewError(err, "cannot create tmp directory")
	}
	defer os.RemoveAll(tmpdir)

	if err := ioutil.WriteFile(filepath.Join(tmpdir, "meta-data"), mdBytes, 0644); err != nil {
		return nil, util.NewError(err, "cannot write metadata file to config drive")
	}
	if err := ioutil.WriteFile(filepath.Join(tmpdir, "user-data"), config.Userdata, 0644); err != nil {
		return nil, util.NewError(err, "cannot write userdata file to config drive")
	}
	localConfigdriveFilename := filepath.Join(tmpdir, "drive.iso")
	cmd := exec.Command("mkisofs", "-o", localConfigdriveFilename, "-V", "cidata", "-r", "-J", "--quiet", tmpdir)
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	configdriveSize, err := util.GetFileSize(localConfigdriveFilename)
	if err != nil {
		return nil, util.NewError(err, "cannot get local configdrive size")
	}

	virVolumeConfig := &libvirtxml.StorageVolume{}
	virVolumeConfig.Name = configVolumeName
	virVolumeConfig.Target = &libvirtxml.StorageVolumeTarget{Format: &libvirtxml.StorageVolumeTargetFormat{Type: "raw"}}
	virVolumeConfig.Capacity = &libvirtxml.StorageVolumeSize{Unit: "MiB", Value: 2}
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
	if err := virVolume.Upload(stream, 0, configdriveSize, 0); err != nil {
		return nil, util.NewError(err, "cannot start configdrive upload")
	}
	configdriveContent, err := ioutil.ReadFile(localConfigdriveFilename)
	if err != nil {
		return nil, util.NewError(err, "cannot read local configdrive file")
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
	tmpfile, err := ioutil.TempFile("", "vmango-read-configdrive")
	if err != nil {
		return nil, util.NewError(err, "cannot create tmp file")
	}
	defer tmpfile.Close()
	defer os.Remove(tmpfile.Name())
	if _, err := io.Copy(tmpfile, &virStreamReader{stream}); err != nil {
		return nil, util.NewError(err, "cannot download configdrive")
	}
	mdCmd := exec.Command("isoinfo", "-J", "-x", "/meta-data", "-i", tmpfile.Name())
	mdBytes, err := mdCmd.Output()
	if err != nil {
		return nil, util.NewError(err, "cannot extract metadata from file")
	}

	md := &CloudInitMetadata{}
	if err := md.Unmarshal(mdBytes); err != nil {
		return nil, util.NewError(err, "cannot parse metadata")
	}
	config := &compute.VirtualMachineConfig{
		Hostname: md.Hostname,
	}

	for _, rawKey := range md.PublicKeys {
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
		config, err := repo.parseConfigDrive(conn, volume.Path)
		if err != nil {
			repo.logger.Warn().Err(err).Str("volume_path", volume.Path).Msg("cannot parse configdrive")
			continue
		}
		vm.Config = config
	}
	vm.Volumes = noConfigDriveVolumes

	return vm, nil
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

	configVolume, err := repo.generateConfigDrive(conn, virDomainConfig, config)
	if err != nil {
		return nil, util.NewError(err, "cannot generate config drive")
	}
	volumes = append(volumes, configVolume)

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
	vm, err := repo.domainToVm(conn, virDomain)
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
