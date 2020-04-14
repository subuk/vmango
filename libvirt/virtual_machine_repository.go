package libvirt

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"os/exec"
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

type VirtualMachineRepositoryNodeSettings struct {
	ConfigDriveVolumePool  string
	ConfigDriveSuffix      string
	ConfigDriveWriteFormat configdrive.Format
}

type VirtualMachineRepository struct {
	pool          *ConnectionPool
	logger        zerolog.Logger
	settings      map[string]*VirtualMachineRepositoryNodeSettings
	configCache   map[string]*compute.VirtualMachineConfig
	configCacheMu *sync.Mutex
}

func NewVirtualMachineRepository(pool *ConnectionPool, settings map[string]*VirtualMachineRepositoryNodeSettings, logger zerolog.Logger) *VirtualMachineRepository {
	return &VirtualMachineRepository{
		pool:          pool,
		settings:      settings,
		logger:        logger,
		configCache:   map[string]*compute.VirtualMachineConfig{},
		configCacheMu: &sync.Mutex{},
	}
}

func (repo *VirtualMachineRepository) generateConfigDrive(conn *libvirt.Connect, poolName string, domainConfig *libvirtxml.Domain, config *compute.VirtualMachineConfig, suffix string, format configdrive.Format) (*compute.VirtualMachineAttachedVolume, error) {
	virPool, err := conn.LookupStoragePoolByName(poolName)
	if err != nil {
		return nil, util.NewError(err, "cannot lookup configdrive storage pool")
	}

	configVolumeName := strings.Replace(domainConfig.Name, "-", "_", -1) + suffix
	if existingVolume, err := virPool.LookupStorageVolByName(configVolumeName); err == nil && existingVolume != nil {
		existingVolumeInfo, err := existingVolume.GetInfo()
		if err == nil && existingVolumeInfo.Capacity <= 2*1024*1024 {
			if err := existingVolume.Delete(libvirt.STORAGE_VOL_DELETE_NORMAL); err != nil {
				return nil, util.NewError(err, "cannot delete existing configdrive volume")
			}
		}
	}

	var data configdrive.Data
	switch format {
	default:
		panic(fmt.Errorf("unknown configdrive write format '%s'", format))
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
		DeviceName: "hda",
		DeviceBus:  compute.DeviceBusIde,
		Type:       compute.VolumeTypeFile,
		Format:     compute.FormatIso,
		Path:       virVolumeConfig.Target.Path,
		DeviceType: compute.DeviceTypeCdrom,
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

func (repo *VirtualMachineRepository) domainToVm(conn *libvirt.Connect, nodeId string, domain *libvirt.Domain, cdSuffix string) (*compute.VirtualMachine, error) {
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

	autostart, err := domain.GetAutostart()
	if err != nil {
		return nil, util.NewError(err, "cannot get domain autostart value")
	}
	vm.Autostart = autostart

	vm.NodeId = nodeId

	if vm.IsRunning() && len(vm.Interfaces) > 0 {
		virDomainIfaces := []libvirt.DomainInterface{}
		if vm.GuestAgent {
			ifaces, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_AGENT)
			if err != nil {
				repo.logger.Debug().Str("vm", vm.Id).Err(err).Msg("cannot get interfaces addresses from guest agent")
			}
			virDomainIfaces = ifaces
		}
		if len(virDomainIfaces) <= 0 {
			ifaces, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)
			if err != nil {
				repo.logger.Debug().Str("vm", vm.Id).Err(err).Msg("cannot get interfaces addresses from dhcp leases")
			}
			virDomainIfaces = ifaces
		}
		if len(virDomainIfaces) <= 0 {
			ifaces, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_ARP)
			if err != nil {
				repo.logger.Debug().Str("vm", vm.Id).Err(err).Msg("cannot get interfaces addresses from arp tables")
			}
			virDomainIfaces = ifaces
		}
		for _, virDomainIface := range virDomainIfaces {
			for _, attachedInterface := range vm.Interfaces {
				if virDomainIface.Hwaddr == attachedInterface.Mac {
					for _, addr := range virDomainIface.Addrs {
						attachedInterface.IpAddressList = append(attachedInterface.IpAddressList, addr.Addr)
					}
				}
			}
		}
	}

	for _, volume := range vm.Volumes {
		if volume.DeviceType != compute.DeviceTypeCdrom {
			continue
		}
		if !strings.HasSuffix(volume.Path, cdSuffix) {
			continue
		}
		if config := repo.configCache[volume.Path]; config != nil {
			vm.Config = config
			continue
		}

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
	// vm.Volumes = noConfigDriveVolumes

	return vm, nil
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
	switch attachedIface.NetworkType {
	default:
		return fmt.Errorf("unsupported interface type %s", attachedIface.NetworkType)
	case compute.NetworkTypeLibvirt:
		domainIface.Source.Network = &libvirtxml.DomainInterfaceSourceNetwork{
			Network: attachedIface.NetworkName,
		}
	case compute.NetworkTypeBridge:
		domainIface.Source.Bridge = &libvirtxml.DomainInterfaceSourceBridge{
			Bridge: attachedIface.NetworkName,
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

func (repo *VirtualMachineRepository) GetConsoleStream(id, nodeId string) (compute.VirtualMachineConsoleStream, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)

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

type cmdIoWrapper struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (w *cmdIoWrapper) Read(p []byte) (int, error) {
	return w.stdout.Read(p)
}

func (w *cmdIoWrapper) Write(p []byte) (int, error) {
	return w.stdin.Write(p)
}

func (w *cmdIoWrapper) Close() error {
	w.stdin.Close()
	return w.stdout.Close()

}

func (repo *VirtualMachineRepository) GetGraphicStream(id, nodeId string) (compute.VirtualMachineGraphicStream, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)

	connUriRaw, err := conn.GetURI()
	if err != nil {
		return nil, util.NewError(err, "cannot get connection hostname")
	}
	connUri, err := url.Parse(connUriRaw)
	if err != nil {
		return nil, util.NewError(err, "cannot parse connection uri")
	}
	virDomain, err := conn.LookupDomainByName(id)
	if err != nil {
		return nil, util.NewError(err, "cannot get vm")
	}
	virDomainRunning, err := virDomain.IsActive()
	if err != nil {
		return nil, util.NewError(err, "cannot check if domain is running")
	}
	if !virDomainRunning {
		return nil, fmt.Errorf("domain is not running")
	}
	virDomainXml, err := virDomain.GetXMLDesc(0)
	if err != nil {
		return nil, util.NewError(err, "cannot fetch domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return nil, util.NewError(err, "cannot parse domain xml")
	}
	graphicPort := 0
	for _, graphic := range virDomainConfig.Devices.Graphics {
		if graphic.VNC != nil {
			graphicPort = graphic.VNC.Port
		}
	}
	if graphicPort <= 0 {
		return nil, fmt.Errorf("no graphic port found")
	}

	if strings.Contains(connUri.Scheme, "ssh") {
		portStr := fmt.Sprintf("%d", graphicPort)
		args := []string{
			"ssh", "-l", connUri.User.Username(), connUri.Hostname(),
			"if (command -v socat) >/dev/null 2>&1; then socat - TCP:localhost:" + portStr + "; else nc localhost " + portStr + "; fi",
		}
		cmd := exec.Command(args[0], args[1:]...)
		repo.logger.Debug().Strs("args", cmd.Args).Msg("executing ssh forwarding command")

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, util.NewError(err, "cannot initialize cmd stdout")
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return nil, util.NewError(err, "cannot initialize cmd stdin")
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, util.NewError(err, "cannot initialize cmd stdin")
		}
		if err := cmd.Start(); err != nil {
			return nil, util.NewError(err, "cannot start ssh forwarding command")
		}
		repo.logger.Debug().Msg("ssh started successfully")
		go func(cmd *exec.Cmd) {
			if err := cmd.Wait(); err != nil {
				errText, _ := ioutil.ReadAll(stderr)
				repo.logger.Warn().Err(err).Str("text", string(errText)).Msg("graphics forwarding command failed")
				return
			}
			repo.logger.Debug().Msg("graphics command finished successfully")
		}(cmd)
		return &cmdIoWrapper{stdin: stdin, stdout: stdout}, nil
	}
	addr := fmt.Sprintf("%s:%d", connUri.Hostname(), graphicPort)
	repo.logger.Debug().Str("addr", addr).Msg("connecting directly to console")
	return net.Dial("tcp", addr)
}

func (repo *VirtualMachineRepository) Create(id, nodeId string, arch compute.Arch, vcpus int, memory compute.Size, volumes []*compute.VirtualMachineAttachedVolume, interfaces []*compute.VirtualMachineAttachedInterface, config *compute.VirtualMachineConfig) (*compute.VirtualMachine, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)

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
	virDomainConfig.Memory = &libvirtxml.DomainMemory{Unit: "bytes", Value: uint(memory.Bytes())}
	virDomainConfig.OS = &libvirtxml.DomainOS{
		Type: &libvirtxml.DomainOSType{Type: "hvm", Machine: domCapsConfig.Machine, Arch: libvirtArch},
		BootDevices: []libvirtxml.DomainBootDevice{
			{Dev: "cdrom"},
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
	settings := repo.settings[nodeId]
	configVolume, err := repo.generateConfigDrive(conn, settings.ConfigDriveVolumePool, virDomainConfig, config, settings.ConfigDriveSuffix, settings.ConfigDriveWriteFormat)
	if err != nil {
		return nil, util.NewError(err, "cannot generate config drive")
	}
	volumes = append(volumes, configVolume)

	for _, volume := range volumes {
		diskConfig := DomainDiskConfigFromVirtualMachineAttachedVolume(volume)
		virDomainConfig.Devices.Disks = append(virDomainConfig.Devices.Disks, *diskConfig)
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
	vm, err := repo.domainToVm(conn, nodeId, virDomain, settings.ConfigDriveSuffix)
	if err != nil {
		return nil, util.NewError(err, "cannot parse domain to vm")
	}
	return vm, nil
}

func (repo *VirtualMachineRepository) Update(id, nodeId string, params compute.VirtualMachineUpdateParams) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)

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
		return util.NewError(err, "cannot unmarshal domain xml")
	}
	if params.Autostart != nil {
		if err := virDomain.SetAutostart(*params.Autostart); err != nil {
			return util.NewError(err, "cannot set autostart to %t", *params.Autostart)
		}
	}
	if params.Vcpus != nil {
		virDomainConfig.VCPU.Value = *params.Vcpus
	}
	if params.Memory != nil {
		virDomainConfig.Memory.Value = uint((*params.Memory).Bytes())
		virDomainConfig.Memory.Unit = "bytes"
		virDomainConfig.CurrentMemory = nil
	}
	if params.GuestAgent != nil {
		if virDomainRunning {
			return util.NewError(err, "domain must be stopped to change guest agent integration state")
		}
		if *params.GuestAgent {
			hasGuestAgent := false
			for _, channel := range virDomainConfig.Devices.Channels {
				if channel.Target != nil && channel.Target.VirtIO != nil && channel.Target.VirtIO.Name == "org.qemu.guest_agent.0" {
					hasGuestAgent = true
					break
				}
			}
			if !hasGuestAgent {
				virDomainConfig.Devices.Channels = append(virDomainConfig.Devices.Channels, libvirtxml.DomainChannel{
					Protocol: &libvirtxml.DomainChardevProtocol{Type: "unix"},
					Target:   &libvirtxml.DomainChannelTarget{VirtIO: &libvirtxml.DomainChannelTargetVirtIO{Name: "org.qemu.guest_agent.0"}},
					Source:   &libvirtxml.DomainChardevSource{UNIX: &libvirtxml.DomainChardevSourceUNIX{}},
				})
			}
		} else {
			newChannels := []libvirtxml.DomainChannel{}
			for _, channel := range virDomainConfig.Devices.Channels {
				if channel.Target != nil && channel.Target.VirtIO != nil && channel.Target.VirtIO.Name == "org.qemu.guest_agent.0" {
					continue
				}
				newChannels = append(newChannels, channel)
			}
			virDomainConfig.Devices.Channels = newChannels
		}
	}

	if params.GraphicType != nil {
		if virDomainRunning {
			return util.NewError(err, "domain must be stopped to change graphic")
		}
		switch *params.GraphicType {
		default:
			panic("unknown graphic type")
		case compute.GraphicTypeNone:
			virDomainConfig.Devices.Graphics = nil
			virDomainConfig.Devices.Videos = nil
		case compute.GraphicTypeVnc:
			vncGraphic := &libvirtxml.DomainGraphicVNC{Port: -1, AutoPort: "yes"}
			virDomainConfig.Devices.Graphics = []libvirtxml.DomainGraphic{
				libvirtxml.DomainGraphic{
					VNC: vncGraphic,
				},
			}
			if params.GraphicListen != nil {
				vncGraphic.Listen = *params.GraphicListen
			}
		case compute.GraphicTypeSpice:
			spiceGraphic := &libvirtxml.DomainGraphicSpice{Port: -1, AutoPort: "yes"}
			virDomainConfig.Devices.Graphics = []libvirtxml.DomainGraphic{
				libvirtxml.DomainGraphic{
					Spice: spiceGraphic,
				},
			}
			if params.GraphicListen != nil {
				spiceGraphic.Listen = *params.GraphicListen
			}

		}

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

func (repo *VirtualMachineRepository) Delete(id, nodeId string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return util.NewError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)

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
	return nil
}

func (repo *VirtualMachineRepository) List() ([]*compute.VirtualMachine, error) {
	vms := []*compute.VirtualMachine{}
	for _, nodeId := range repo.pool.Nodes() {
		conn, err := repo.pool.Acquire(nodeId)
		if err != nil {
			return nil, util.NewError(err, "cannot acquire libvirt connection")
		}
		defer repo.pool.Release(nodeId)
		settings := repo.settings[nodeId]
		domains, err := conn.ListAllDomains(0)
		for _, domain := range domains {
			vm, err := repo.domainToVm(conn, nodeId, &domain, settings.ConfigDriveSuffix)
			if err != nil {
				return nil, util.NewError(err, "cannot convert libvirt domain to vm")
			}
			vms = append(vms, vm)
		}
	}
	return vms, nil
}

func (repo *VirtualMachineRepository) Get(id, nodeId string) (*compute.VirtualMachine, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	settings := repo.settings[nodeId]
	domain, err := conn.LookupDomainByName(id)
	if err != nil {
		return nil, util.NewError(err, "failed to lookup vm")
	}
	vm, err := repo.domainToVm(conn, nodeId, domain, settings.ConfigDriveSuffix)
	if err != nil {
		return nil, util.NewError(err, "cannot convert libvirt domain to vm")
	}
	return vm, nil
}

func (repo *VirtualMachineRepository) Poweroff(id, nodeId string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	domain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "domain lookup failed")
	}
	return domain.Destroy()
}

func (repo *VirtualMachineRepository) Reboot(id, nodeId string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	domain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "domain lookup failed")
	}
	return domain.Reboot(libvirt.DOMAIN_REBOOT_DEFAULT)
}

func (repo *VirtualMachineRepository) Start(id, nodeId string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	domain, err := conn.LookupDomainByName(id)
	if err != nil {
		return util.NewError(err, "domain lookup failed")
	}
	return domain.Create()
}

func (repo *VirtualMachineRepository) AttachVolume(id, nodeId string, attachedVolume *compute.VirtualMachineAttachedVolume) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

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

	discConfig := DomainDiskConfigFromVirtualMachineAttachedVolume(attachedVolume)
	virDomainConfig.Devices.Disks = append(virDomainConfig.Devices.Disks, *discConfig)

	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return util.NewError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return util.NewError(err, "cannot update domain xml")
	}
	return nil
}

func (repo *VirtualMachineRepository) DetachVolume(id, nodeId, needlePath string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

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

func (repo *VirtualMachineRepository) AttachInterface(id, nodeId string, attachedIface *compute.VirtualMachineAttachedInterface) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

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
	if err := repo.attachInterface(virDomainConfig, attachedIface); err != nil {
		return err
	}
	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return util.NewError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return util.NewError(err, "cannot update domain xml")
	}
	return nil
}

func (repo *VirtualMachineRepository) DetachInterface(id, nodeId, needleMac string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return util.NewError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

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
