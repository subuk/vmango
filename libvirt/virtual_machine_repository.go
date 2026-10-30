package libvirt

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/configdrive"
	"subuk/vmango/tasks"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/rs/zerolog"
	"libvirt.org/go/libvirt"
	libvirtxml "libvirt.org/go/libvirtxml"
)

const ConfigDriveMaxSize = 5 * 1024 * 1024
const VirtualMachineSockets = 1

type VirtualMachineRepository struct {
	pool          *ConnectionPool
	logger        zerolog.Logger
	settings      map[string]NodeSettings
	configCache   map[string]*compute.VirtualMachineConfig
	configCacheMu *sync.Mutex
}

func NewVirtualMachineRepository(pool *ConnectionPool, settings map[string]NodeSettings, logger zerolog.Logger) *VirtualMachineRepository {
	return &VirtualMachineRepository{
		pool:          pool,
		settings:      settings,
		logger:        logger,
		configCache:   map[string]*compute.VirtualMachineConfig{},
		configCacheMu: &sync.Mutex{},
	}
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
		return nil, repositoryError(err, "cannot lookup volume")
	}
	stream, err := conn.NewStream(0)
	if err != nil {
		return nil, repositoryError(err, "cannot initialize configdrive download stream")
	}
	if err := virVolume.Download(stream, 0, ConfigDriveMaxSize, 0); err != nil {
		return nil, repositoryError(err, "cannot start configdrive download")
	}
	data, err := configdrive.ParseIso(configdrive.AllFormats, &virStreamReader{stream})
	if err != nil {
		return nil, repositoryError(err, "cannot parse configdrive iso")
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

func (repo *VirtualMachineRepository) domainToVm(conn *libvirt.Connect, nodeId string, domain *libvirt.Domain, settings NodeSettings) (*compute.VirtualMachine, error) {
	domainXml, err := domain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return nil, repositoryError(err, "cannot get domain xml")
	}
	domainConfig := &libvirtxml.Domain{}
	if err := domainConfig.Unmarshal(domainXml); err != nil {
		return nil, repositoryError(err, "cannot unmarshal domain xml")
	}
	domainInfo, err := domain.GetInfo()
	if err != nil {
		return nil, repositoryError(err, "cannot get domain info")
	}
	vm, err := VirtualMachineFromDomainConfig(domainConfig, domainInfo)
	if err != nil {
		return nil, repositoryError(err, "cannot create virtual machine from domain config")
	}

	autostart, err := domain.GetAutostart()
	if err != nil {
		return nil, repositoryError(err, "cannot get domain autostart value")
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
				if strings.EqualFold(virDomainIface.Hwaddr, attachedInterface.Mac) {
					for _, addr := range virDomainIface.Addrs {
						attachedInterface.IpAddressList = append(attachedInterface.IpAddressList, addr.Addr)
					}
				}
			}
		}
	}

	for _, volume := range vm.Volumes {
		if !strings.HasSuffix(volume.Path, settings.CdSuffix) {
			continue
		}
		repo.configCacheMu.Lock()
		cachedConfig := repo.configCache[nodeId+"\x00"+volume.Path]
		repo.configCacheMu.Unlock()
		if config := cachedConfig; config != nil {
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
		repo.configCache[nodeId+"\x00"+volume.Path] = config
		repo.configCacheMu.Unlock()
	}

	return vm, nil
}

func (repo *VirtualMachineRepository) attachInterface(virDomainConfig *libvirtxml.Domain, attachedIface *compute.VirtualMachineAttachedInterface) error {
	if !attachedIface.Bridge && (attachedIface.NetworkName == "macos-socket-vmnet" || attachedIface.NetworkName == "qemu-usernet") {
		if virDomainConfig.QEMUCommandline == nil {
			virDomainConfig.QEMUCommandline = &libvirtxml.DomainQEMUCommandline{}
		}
		if attachedIface.Mac == "" {
			attachedIface.Mac = generateMacAddress("52:54:00")
		}

		ifaceNum := 0
		for idx, arg := range virDomainConfig.QEMUCommandline.Args {
			if arg.Value != "-netdev" {
				continue
			}
			argv := virDomainConfig.QEMUCommandline.Args[idx+1].Value
			params := parseQemuDeviceArg(argv)
			if strings.HasPrefix(params["id"], "vmngmcos") {
				thisIfaceNum, err := strconv.ParseInt(string(params["id"][len("vmngmcos"):]), 10, 64)
				if err != nil {
					repo.logger.Debug().Err(err).Str("full_argv", argv).Msg("failed to extract number from vmngmcos interface")
					continue
				}
				if thisIfaceNum > int64(ifaceNum) {
					ifaceNum = int(thisIfaceNum)
				}
			}
		}
		ifaceNum++
		ifaceName := fmt.Sprintf("vmngmcos%d", ifaceNum)
		deviceID := ""
		if alias := logicalNetworkAlias(attachedIface.LogicalNetwork); alias != "" {
			deviceID = ",id=" + alias
		}
		deviceModel := "virtio-net-device"
		if virDomainConfig.OS.Type.Arch == "x86_64" {
			deviceModel = "virtio-net-pci"
		}
		switch attachedIface.NetworkName {
		default:
			panic(fmt.Errorf("unhandled network type %s", attachedIface.NetworkName))
		case "macos-socket-vmnet":
			virDomainConfig.QEMUCommandline.Args = append(virDomainConfig.QEMUCommandline.Args, libvirtxml.DomainQEMUCommandlineArg{Value: "-netdev"})
			virDomainConfig.QEMUCommandline.Args = append(virDomainConfig.QEMUCommandline.Args, libvirtxml.DomainQEMUCommandlineArg{Value: "socket,id=" + ifaceName + ",fd=3"})
			virDomainConfig.QEMUCommandline.Args = append(virDomainConfig.QEMUCommandline.Args, libvirtxml.DomainQEMUCommandlineArg{Value: "-device"})
			virDomainConfig.QEMUCommandline.Args = append(virDomainConfig.QEMUCommandline.Args, libvirtxml.DomainQEMUCommandlineArg{Value: deviceModel + ",netdev=" + ifaceName + ",mac=" + attachedIface.Mac + deviceID})
		case "qemu-usernet":
			virDomainConfig.QEMUCommandline.Args = append(virDomainConfig.QEMUCommandline.Args, libvirtxml.DomainQEMUCommandlineArg{Value: "-netdev"})
			virDomainConfig.QEMUCommandline.Args = append(virDomainConfig.QEMUCommandline.Args, libvirtxml.DomainQEMUCommandlineArg{Value: "user,id=" + ifaceName})
			virDomainConfig.QEMUCommandline.Args = append(virDomainConfig.QEMUCommandline.Args, libvirtxml.DomainQEMUCommandlineArg{Value: "-device"})
			virDomainConfig.QEMUCommandline.Args = append(virDomainConfig.QEMUCommandline.Args, libvirtxml.DomainQEMUCommandlineArg{Value: deviceModel + ",netdev=" + ifaceName + ",mac=" + attachedIface.Mac + deviceID})
		}

		return nil
	}

	if attachedIface.Model == "" {
		attachedIface.Model = "virtio"
	}
	domainIface := libvirtxml.DomainInterface{}
	if alias := logicalNetworkAlias(attachedIface.LogicalNetwork); alias != "" {
		domainIface.Alias = &libvirtxml.DomainAlias{Name: alias}
	}
	if attachedIface.Mac != "" {
		domainIface.MAC = &libvirtxml.DomainInterfaceMAC{Address: attachedIface.Mac}
	}
	domainIface.Source = &libvirtxml.DomainInterfaceSource{}
	domainIface.Model = &libvirtxml.DomainInterfaceModel{Type: attachedIface.Model}
	if attachedIface.Bridge {
		domainIface.Source.Bridge = &libvirtxml.DomainInterfaceSourceBridge{Bridge: attachedIface.NetworkName}
	} else {
		domainIface.Source.Network = &libvirtxml.DomainInterfaceSourceNetwork{Network: attachedIface.NetworkName}
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
	err := r.Stream.Abort()
	r.Stream.Free()
	return err
}

func (repo *VirtualMachineRepository) GetConsoleStream(id, nodeId string, force bool) (compute.VirtualMachineConsoleStream, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, repositoryError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)

	virDomain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return nil, repositoryError(err, "cannot get vm")
	}
	defer virDomain.Free()
	stream, err := conn.NewStream(0)
	if err != nil {
		return nil, repositoryError(err, "cannot create stream")
	}
	flags := libvirt.DOMAIN_CONSOLE_SAFE
	if force {
		flags |= libvirt.DOMAIN_CONSOLE_FORCE
	}
	if err := virDomain.OpenConsole("", stream, flags); err != nil {
		stream.Free()
		return nil, fmt.Errorf("%w: console unavailable or occupied", compute.ErrConflict)
	}
	return &virStreamReadWriteCloser{stream}, nil
}

type cmdIoWrapper struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cmd    *exec.Cmd
}

func (w *cmdIoWrapper) Read(p []byte) (int, error) {
	return w.stdout.Read(p)
}

func (w *cmdIoWrapper) Write(p []byte) (int, error) {
	return w.stdin.Write(p)
}

func (w *cmdIoWrapper) Close() error {
	w.stdin.Close()
	if w.cmd != nil {
		w.cmd.Process.Kill()
	}
	return w.stdout.Close()

}

func (repo *VirtualMachineRepository) GetGraphicStream(id, nodeId string) (compute.VirtualMachineGraphicStream, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, repositoryError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)

	connUriRaw, err := conn.GetURI()
	if err != nil {
		return nil, repositoryError(err, "cannot get connection hostname")
	}
	connUri, err := url.Parse(connUriRaw)
	if err != nil {
		return nil, repositoryError(err, "cannot parse connection uri")
	}
	virDomain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return nil, repositoryError(err, "cannot get vm")
	}
	virDomainRunning, err := virDomain.IsActive()
	if err != nil {
		return nil, repositoryError(err, "cannot check if domain is running")
	}
	if !virDomainRunning {
		return nil, fmt.Errorf("%w: domain is not running", compute.ErrConflict)
	}
	virDomainXml, err := virDomain.GetXMLDesc(0)
	if err != nil {
		return nil, repositoryError(err, "cannot fetch domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return nil, repositoryError(err, "cannot parse domain xml")
	}
	graphicPort := 0
	graphicListen := ""
	for _, graphic := range virDomainConfig.Devices.Graphics {
		if graphic.VNC != nil {
			graphicPort = graphic.VNC.Port
			graphicListen = graphicsListenerAddress(graphic.VNC.Listen, graphic.VNC.Listeners)
		}
	}
	if graphicPort <= 0 {
		return nil, fmt.Errorf("%w: no graphic port found", compute.ErrConflict)
	}

	consoleHost, err := graphicsDialHost(graphicListen, connUri)
	if err != nil {
		return nil, err
	}
	if strings.Contains(connUri.Scheme, "ssh") {
		portStr := fmt.Sprintf("%d", graphicPort)
		args := []string{
			"ssh", "-l", connUri.User.Username(), connUri.Hostname(),
			graphicsForwardCommand(consoleHost, portStr),
		}
		cmd := exec.Command(args[0], args[1:]...)
		repo.logger.Debug().Strs("args", cmd.Args).Msg("executing ssh forwarding command")

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, repositoryError(err, "cannot initialize cmd stdout")
		}
		cmd.Stderr = io.Discard
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, repositoryError(err, "cannot initialize cmd stdin")
		}
		if err := cmd.Start(); err != nil {
			return nil, repositoryError(err, "cannot start ssh forwarding command")
		}
		repo.logger.Debug().Msg("ssh started successfully")
		go func(cmd *exec.Cmd) {
			if err := cmd.Wait(); err != nil {
				repo.logger.Warn().Msg("graphics forwarding command failed")
				return
			}
			repo.logger.Debug().Msg("graphics command finished successfully")
		}(cmd)
		return &cmdIoWrapper{stdin: stdin, stdout: stdout, cmd: cmd}, nil
	}
	addr := net.JoinHostPort(consoleHost, strconv.Itoa(graphicPort))
	repo.logger.Debug().Str("addr", addr).Msg("connecting directly to console")
	return net.DialTimeout("tcp", addr, 10*time.Second)
}

func (repo *VirtualMachineRepository) generateNewDomainConfig(conn *libvirt.Connect, vm *compute.VirtualMachine) (*libvirtxml.Domain, error) {
	settings := repo.settings[vm.NodeId]
	domCapsXml, err := conn.GetDomainCapabilities(settings.Emulator, vm.Arch.String(), "", "", 0)
	if err != nil {
		return nil, repositoryError(err, "cannot fetch domain capabilities")
	}
	domCapsConfig := &libvirtxml.DomainCaps{}
	if err := domCapsConfig.Unmarshal(domCapsXml); err != nil {
		return nil, repositoryError(err, "cannot parse domain capabilities")
	}

	return newDomainConfig(vm, domCapsConfig), nil
}

// newDomainConfig builds domain XML from capabilities without contacting a hypervisor.
func newDomainConfig(vm *compute.VirtualMachine, domCapsConfig *libvirtxml.DomainCaps) *libvirtxml.Domain {
	virDomainConfig := &libvirtxml.Domain{}
	virDomainConfig.UUID = vm.Id
	virDomainConfig.Type = domCapsConfig.Domain
	virDomainConfig.Name = vm.Name
	virDomainConfig.OS = &libvirtxml.DomainOS{
		Type: &libvirtxml.DomainOSType{Type: "hvm", Machine: domCapsConfig.Machine, Arch: domCapsConfig.Arch},
		BootDevices: []libvirtxml.DomainBootDevice{
			{Dev: "cdrom"},
			{Dev: "hd"},
		},
	}
	if vm.Firmware != "" {
		virDomainConfig.OS.Firmware = vm.Firmware
	}
	// PC guests need ACPI/APIC for CPU discovery and power management.
	// Select by guest architecture, not the host (which may also run ARM VMs).
	if domCapsConfig.Arch == "x86_64" || domCapsConfig.Arch == "i686" {
		virDomainConfig.Features = &libvirtxml.DomainFeatureList{
			ACPI: &libvirtxml.DomainFeature{},
			APIC: &libvirtxml.DomainFeatureAPIC{},
		}
	}
	virDomainConfig.CPU = &libvirtxml.DomainCPU{}
	virDomainConfig.CPU.Mode = "maximum"
	virDomainConfig.Clock = &libvirtxml.DomainClock{Offset: "utc"}
	virDomainConfig.OnPoweroff = "destroy"
	virDomainConfig.OnReboot = "restart"
	virDomainConfig.OnCrash = "destroy"
	virDomainConfig.Devices = &libvirtxml.DomainDeviceList{Emulator: domCapsConfig.Path}
	if vm.DiskController != "" {
		virDomainConfig.Devices.Controllers = append(virDomainConfig.Devices.Controllers, libvirtxml.DomainController{Type: "scsi", Model: vm.DiskController})
	}
	virDomainConfig.Devices.Consoles = append(virDomainConfig.Devices.Consoles, libvirtxml.DomainConsole{})

	return virDomainConfig
}

func (repo *VirtualMachineRepository) Save(vm *compute.VirtualMachine) error {
	return repo.save(vm, false)
}

func (repo *VirtualMachineRepository) SaveResources(vm *compute.VirtualMachine) error {
	return repo.save(vm, true)
}

func (repo *VirtualMachineRepository) save(vm *compute.VirtualMachine, resourcesOnly bool) (err error) {
	ref := compute.VMRef(vm.NodeId, vm.Id)
	var effects []tasks.Effect
	defer func() { err = mutationError(err, "domain.save", ref, effects) }()

	conn, err := repo.pool.Acquire(vm.NodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(vm.NodeId)

	capsXml, err := conn.GetCapabilities()
	if err != nil {
		return repositoryError(err, "cannot fetch domain capabilities")
	}
	capsConfig := &libvirtxml.Caps{}
	if err := capsConfig.Unmarshal(capsXml); err != nil {
		return repositoryError(err, "cannot parse capabilities")
	}

	var isNewVm bool
	var virDomainConfig *libvirtxml.Domain

	existingVirDomain, err := conn.LookupDomainByUUIDString(vm.Id)
	if err != nil {
		if !errors.Is(err, libvirt.ERR_NO_DOMAIN) {
			return repositoryError(err, "cannot lookup domain")
		}
		isNewVm = true
	}

	if isNewVm && resourcesOnly {
		return compute.ErrNotFound
	}
	if isNewVm {
		newVirDomainConfig, err := repo.generateNewDomainConfig(conn, vm)
		if err != nil {
			return repositoryError(err, "cannot generate new domain config")
		}
		virDomainConfig = newVirDomainConfig
	} else {
		flags := libvirt.DOMAIN_XML_INACTIVE
		if resourcesOnly {
			flags |= libvirt.DOMAIN_XML_SECURE
		}
		virDomainXml, err := existingVirDomain.GetXMLDesc(flags)
		if err != nil {
			return repositoryError(err, "cannot fetch xml")
		}
		virDomainConfig = &libvirtxml.Domain{}
		if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
			return repositoryError(err, "cannot parse domain xml")
		}
	}

	applyDomainPolicy(virDomainConfig, vm, capsConfig, resourcesOnly)

	if isNewVm {
		namer := NewDeviceNamer()
		for _, attachedVolume := range vm.Volumes {
			if err := repo.attachVolume(conn, virDomainConfig, attachedVolume, namer); err != nil {
				return repositoryError(err, "cannot attach volume")
			}
		}
		for _, attachedIface := range vm.Interfaces {
			if err := repo.attachInterface(virDomainConfig, attachedIface); err != nil {
				return repositoryError(err, "cannot attach interface")
			}
		}
	}
	if err := writeSchedulerMetadata(virDomainConfig, vm.Metadata); err != nil {
		return err
	}
	virDomainXml, err := virDomainConfig.Marshal()
	if err != nil {
		return repositoryError(err, "cannot marshal domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return repositoryError(err, "cannot define new domain")
	}
	effects = append(effects, confirmed(ref, "configuration-defined"))
	virDomain, err := conn.LookupDomainByUUIDString(vm.Id)
	if err != nil {
		return repositoryError(err, "cannot lookup domain after define")
	}
	virDomainAutostart, err := virDomain.GetAutostart()
	if err != nil {
		return repositoryError(err, "cannot get domain autostart state")
	}
	if vm.Autostart != virDomainAutostart {
		if err := virDomain.SetAutostart(vm.Autostart); err != nil {
			return repositoryError(err, "cannot set domain autostart state")
		}
	}
	return nil
}

func (repo *VirtualMachineRepository) Delete(id, nodeId string) (err error) {
	ref := compute.VMRef(nodeId, id)
	var effects []tasks.Effect
	defer func() { err = mutationError(err, "domain.delete", ref, effects) }()

	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)

	virDomain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return repositoryError(err, "lookup domain failed")
	}
	virDomainRunning, err := virDomain.IsActive()
	if err != nil {
		return repositoryError(err, "cannot check if domain is running")
	}
	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return repositoryError(err, "cannot fetch domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return repositoryError(err, "cannot parse domain xml")
	}
	if virDomainRunning {
		if err := virDomain.Destroy(); err != nil {
			return repositoryError(err, "cannot destroy domain")
		}
		effects = append(effects, confirmed(ref, "powered-off"))
	}
	if err := virDomain.UndefineFlags(libvirt.DOMAIN_UNDEFINE_NVRAM); err != nil {
		return repositoryError(err, "cannot undefine domain")
	}
	return nil
}

func (repo *VirtualMachineRepository) nodeList(nodeId string) ([]*compute.VirtualMachine, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, repositoryError(err, "cannot acquire libvirt connection")
	}
	defer repo.pool.Release(nodeId)
	vms := []*compute.VirtualMachine{}
	settings := repo.settings[nodeId]
	domains, err := conn.ListAllDomains(0)
	if err != nil {
		return nil, repositoryError(err, "cannot list all domains")
	}
	for _, domain := range domains {
		vm, err := repo.domainToVm(conn, nodeId, &domain, settings)
		domain.Free()
		if err != nil {
			return nil, repositoryError(err, "cannot convert libvirt domain to vm")
		}
		vms = append(vms, vm)
	}
	return vms, nil
}

func (repo *VirtualMachineRepository) List(options compute.VirtualMachineListOptions) ([]*compute.VirtualMachine, error) {
	result := []*compute.VirtualMachine{}
	var inventoryError error
	nodes := repo.pool.Nodes(options.NodeIds)
	wg := &sync.WaitGroup{}
	mu := &sync.Mutex{}
	wg.Add(len(nodes))
	start := time.Now()
	for _, nodeId := range nodes {
		go func(nodeId string) {
			defer wg.Done()
			nodeStart := time.Now()
			vms, err := repo.nodeList(nodeId)
			if err != nil {
				repo.logger.Warn().Err(err).Str("node", nodeId).Msg("cannot list vms")
				mu.Lock()
				inventoryError = fmt.Errorf("inventory for host %s: %w", nodeId, err)
				mu.Unlock()
				return
			}
			repo.logger.Debug().Str("node", nodeId).TimeDiff("took", time.Now(), nodeStart).Msg("node vm list done")
			mu.Lock()
			result = append(result, vms...)
			mu.Unlock()
		}(nodeId)
	}
	wg.Wait()
	repo.logger.Debug().TimeDiff("took", time.Now(), start).Msg("full vm list done")
	if options.Strict && inventoryError != nil {
		return nil, inventoryError
	}
	return result, nil
}

func (repo *VirtualMachineRepository) Get(id, nodeId string) (*compute.VirtualMachine, error) {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return nil, repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	settings := repo.settings[nodeId]
	domain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return nil, repositoryError(err, "failed to lookup vm")
	}
	vm, err := repo.domainToVm(conn, nodeId, domain, settings)
	if err != nil {
		return nil, repositoryError(err, "cannot convert libvirt domain to vm")
	}
	return vm, nil
}

func (repo *VirtualMachineRepository) Poweroff(id, nodeId string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	domain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return repositoryError(err, "domain lookup failed")
	}
	return repositoryError(domain.Destroy(), "virDomainDestroy")
}

func (repo *VirtualMachineRepository) Reboot(id, nodeId string) error {
	if err := ensureEventLoop(); err != nil {
		return repositoryError(err, "initialize reboot observation")
	}
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	domain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		repo.pool.Release(nodeId)
		return repositoryError(err, "domain lookup failed")
	}
	defer domain.Free()
	observed := make(chan struct{}, 1)
	callback, err := conn.DomainEventRebootRegister(domain, func(_ *libvirt.Connect, _ *libvirt.Domain) {
		select {
		case observed <- struct{}{}:
		default:
		}
	})
	if err != nil {
		repo.pool.Release(nodeId)
		return repositoryError(err, "register reboot confirmation")
	}
	defer conn.DomainEventDeregister(callback)
	err = domain.Reboot(libvirt.DOMAIN_REBOOT_DEFAULT)
	repo.pool.Release(nodeId)
	if err != nil {
		return repositoryError(err, "virDomainReboot")
	}
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	select {
	case <-observed:
		return nil
	case <-timer.C:
		return fmt.Errorf("reboot was requested but no libvirt reboot event was observed within two minutes; the backend outcome is uncertain")
	}
}

func (repo *VirtualMachineRepository) Start(id, nodeId string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	domain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return repositoryError(err, "domain lookup failed")
	}
	return repositoryError(domain.Create(), "virDomainCreate")
}

func (repo *VirtualMachineRepository) attachVolume(conn *libvirt.Connect, virDomainConfig *libvirtxml.Domain, attachedVolume *compute.VirtualMachineAttachedVolume, namer *DeviceNamer) error {
	virVolumeConfig, err := getVolumeConfigByPath(conn, attachedVolume.Path)
	if err != nil {
		return repositoryError(err, "cannot get volume config")
	}
	diskConfig := DomainDiskConfigFromVirtualMachineAttachedVolume(
		attachedVolume,
		getVolTargetFormatType(virVolumeConfig),
		virVolumeConfig.Type,
		namer,
	)
	virDomainConfig.Devices.Disks = append(virDomainConfig.Devices.Disks, *diskConfig)
	return nil
}

func (repo *VirtualMachineRepository) AttachVolume(id, nodeId string, attachedVolume *compute.VirtualMachineAttachedVolume) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	virDomain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return repositoryError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return repositoryError(err, "cannot check if domain is running")
	}
	if running {
		return fmt.Errorf("%w: domain must be stopped", compute.ErrConflict)
	}

	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return repositoryError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return repositoryError(err, "cannot parse domain xml")
	}
	namer := NewDeviceNamerFromDisks(virDomainConfig.Devices.Disks)
	if err := repo.attachVolume(conn, virDomainConfig, attachedVolume, namer); err != nil {
		return repositoryError(err, "cannot add volume xml config")
	}

	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return repositoryError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return repositoryError(err, "cannot update domain")
	}
	return nil
}

func (repo *VirtualMachineRepository) DetachVolume(id, nodeId, needlePath string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	virDomain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return repositoryError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return repositoryError(err, "cannot check if domain is running")
	}
	if running {
		return fmt.Errorf("%w: domain must be stopped", compute.ErrConflict)
	}

	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return repositoryError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return repositoryError(err, "cannot parse domain xml")
	}
	if virDomainConfig.Devices.Disks == nil {
		return compute.ErrNotFound
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
		return compute.ErrNotFound
	}
	virDomainConfig.Devices.Disks = newDisks
	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return repositoryError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return repositoryError(err, "cannot update domain")
	}
	return nil
}

func (repo *VirtualMachineRepository) AttachInterface(id, nodeId string, attachedIface *compute.VirtualMachineAttachedInterface) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	virDomain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return repositoryError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return repositoryError(err, "cannot check if domain is running")
	}
	if running {
		return fmt.Errorf("%w: domain must be stopped", compute.ErrConflict)
	}
	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return repositoryError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return repositoryError(err, "cannot parse domain xml")
	}
	if err := repo.attachInterface(virDomainConfig, attachedIface); err != nil {
		return err
	}
	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return repositoryError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return repositoryError(err, "cannot update domain xml")
	}
	return nil
}

func (repo *VirtualMachineRepository) detachInterfaceLibvirt(id, nodeId, needleMac string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	virDomain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return repositoryError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return repositoryError(err, "cannot check if domain is running")
	}
	if running {
		return fmt.Errorf("%w: domain must be stopped", compute.ErrConflict)
	}

	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return repositoryError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return repositoryError(err, "cannot parse domain xml")
	}
	if virDomainConfig.Devices.Interfaces == nil {
		return compute.ErrInterfaceNotFound
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
		return compute.ErrInterfaceNotFound
	}
	virDomainConfig.Devices.Interfaces = newInterfaces
	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return repositoryError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return repositoryError(err, "cannot update domain")
	}
	return nil
}

func (repo *VirtualMachineRepository) detachInterfaceQemuArgs(id, nodeId, needleMac string) error {
	conn, err := repo.pool.Acquire(nodeId)
	if err != nil {
		return repositoryError(err, "cannot acquire connection")
	}
	defer repo.pool.Release(nodeId)

	virDomain, err := conn.LookupDomainByUUIDString(id)
	if err != nil {
		return repositoryError(err, "domain lookup failed")
	}
	running, err := virDomain.IsActive()
	if err != nil {
		return repositoryError(err, "cannot check if domain is running")
	}
	if running {
		return fmt.Errorf("%w: domain must be stopped", compute.ErrConflict)
	}
	virDomainXml, err := virDomain.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return repositoryError(err, "cannot get domain xml")
	}
	virDomainConfig := &libvirtxml.Domain{}
	if err := virDomainConfig.Unmarshal(virDomainXml); err != nil {
		return repositoryError(err, "cannot parse domain xml")
	}
	if virDomainConfig.QEMUCommandline == nil {
		return compute.ErrInterfaceNotFound
	}
	if len(virDomainConfig.QEMUCommandline.Args) <= 0 {
		return compute.ErrInterfaceNotFound
	}

	needleNetdevId := ""
	for idx, qarg := range virDomainConfig.QEMUCommandline.Args {
		switch qarg.Value {
		case "-device":
			argv := virDomainConfig.QEMUCommandline.Args[idx+1].Value
			if strings.Contains(strings.ToLower(argv), "="+strings.ToLower(needleMac)) {
				params := parseQemuDeviceArg(argv)
				needleNetdevId = params["netdev"]
			}
		}
	}

	newArgs := []libvirtxml.DomainQEMUCommandlineArg{}
	for idx := 0; idx < len(virDomainConfig.QEMUCommandline.Args); idx++ {
		arg := virDomainConfig.QEMUCommandline.Args[idx]
		switch arg.Value {
		default:
			newArgs = append(newArgs, arg)
		case "-netdev":
			idx++
			nextarg := virDomainConfig.QEMUCommandline.Args[idx]
			params := parseQemuDeviceArg(nextarg.Value)
			if params["id"] == needleNetdevId {
				continue
			}
			newArgs = append(newArgs, arg, nextarg)
		case "-device":
			idx++
			nextarg := virDomainConfig.QEMUCommandline.Args[idx]
			params := parseQemuDeviceArg(nextarg.Value)
			if params["netdev"] == needleNetdevId {
				continue
			}
			newArgs = append(newArgs, arg, nextarg)
		}
	}
	if len(newArgs) == len(virDomainConfig.QEMUCommandline.Args) {
		return compute.ErrInterfaceNotFound
	}
	virDomainConfig.QEMUCommandline.Args = newArgs
	virDomainXml, err = virDomainConfig.Marshal()
	if err != nil {
		return repositoryError(err, "cannot create domain xml")
	}
	if _, err := conn.DomainDefineXML(virDomainXml); err != nil {
		return repositoryError(err, "cannot update domain")
	}
	return nil
}

func (repo *VirtualMachineRepository) DetachInterface(id, nodeId, needleMac string) error {
	if err := repo.detachInterfaceLibvirt(id, nodeId, needleMac); err != nil {
		if err == compute.ErrInterfaceNotFound {
			return repo.detachInterfaceQemuArgs(id, nodeId, needleMac)
		}
	}
	return nil
}
