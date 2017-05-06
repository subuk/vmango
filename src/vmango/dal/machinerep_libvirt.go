package dal

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/libvirt/libvirt-go"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"vmango/models"
)

var METADATA_TEMPLATE = template.Must(template.New("metadata").Parse(strings.TrimSpace(`
<vmango:md xmlns:vmango="http://vmango.org/schema/md">
  <vmango:imageId>{{ .Machine.ImageId }}</vmango:imageId>
  <vmango:os>{{ .Machine.OS }}</vmango:os>
  <vmango:sshkeys>
    {{ range .Machine.SSHKeys }}
    <vmango:key name="{{ .Name }}">{{ .Public }}</vmango:key>
    {{ end }}
  </vmango:sshkeys>
  <vmango:userdata>
    <![CDATA[
      {{ .Machine.Userdata }}
    ]]>
  </vmango:userdata>
</vmango:md>
`)))

type LibvirtMachinerep struct {
	conn        *libvirt.Connect
	vmtpl       *template.Template
	voltpl      *template.Template
	network     string
	storagePool string
	hypervisor  string
	ignoreVms   []string
}

func NewLibvirtMachinerep(conn *libvirt.Connect, vmtpl, voltpl *template.Template, network, pool, hypervisor string, ignoreVms []string) (*LibvirtMachinerep, error) {
	return &LibvirtMachinerep{
		conn:        conn,
		vmtpl:       vmtpl,
		voltpl:      voltpl,
		network:     network,
		hypervisor:  hypervisor,
		storagePool: pool,
		ignoreVms:   ignoreVms,
	}, nil
}

func (store *LibvirtMachinerep) assignIP(vm *models.VirtualMachine) error {
	network, err := store.conn.LookupNetworkByName(store.network)
	if err != nil {
		return err
	}
	xmlString, err := network.GetXMLDesc(0)
	if err != nil {
		return err
	}
	networkConfig := netXMLConfig{}
	if err := xml.Unmarshal([]byte(xmlString), &networkConfig); err != nil {
		return fmt.Errorf("failed to parse network xml: %s", err)
	}
	addrs, err := listIPRange(
		networkConfig.IP.DHCPRange.Start,
		networkConfig.IP.DHCPRange.End,
		networkConfig.IP.Address,
		networkConfig.IP.Netmask,
	)
	if err != nil {
		return err
	}
	var ip *models.IP
	for _, addr := range addrs {
		if has := networkConfig.HasHost(addr); !has {
			ip = &models.IP{Address: addr}
			break
		}
	}
	if ip == nil {
		return fmt.Errorf("failed to find free IP address")
	}

	return network.Update(
		libvirt.NETWORK_UPDATE_COMMAND_ADD_LAST,
		libvirt.NETWORK_SECTION_IP_DHCP_HOST,
		-1,
		fmt.Sprintf(
			`<host mac="%s" name="%s" ip="%s" />`,
			vm.HWAddr, vm.Name, ip.Address,
		),
		libvirt.NETWORK_UPDATE_AFFECT_LIVE|libvirt.NETWORK_UPDATE_AFFECT_CONFIG,
	)
}

func (store *LibvirtMachinerep) releaseIP(vm *models.VirtualMachine) error {
	network, err := store.conn.LookupNetworkByName(store.network)
	if err != nil {
		return err
	}
	if vm.Ip == nil {
		log.WithField("machine", vm.Name).Warn("no ip to release")
		return nil
	}
	return network.Update(
		libvirt.NETWORK_UPDATE_COMMAND_DELETE,
		libvirt.NETWORK_SECTION_IP_DHCP_HOST,
		-1,
		fmt.Sprintf(
			`<host mac="%s" name="%s" ip="%s" />`,
			vm.HWAddr, vm.Name, vm.Ip.Address,
		),
		libvirt.NETWORK_UPDATE_AFFECT_LIVE|libvirt.NETWORK_UPDATE_AFFECT_CONFIG,
	)
}

func (store *LibvirtMachinerep) fillVm(vm *models.VirtualMachine, domain *libvirt.Domain, network *libvirt.Network) error {
	name, err := domain.GetName()
	if err != nil {
		return err
	}
	uuid, err := domain.GetUUID()
	if err != nil {
		return err
	}
	info, err := domain.GetInfo()
	if err != nil {
		return err
	}

	domainXMLString, err := domain.GetXMLDesc(0)
	if err != nil {
		return err
	}

	domainConfig := domainXMLConfig{}
	if err := xml.Unmarshal([]byte(domainXMLString), &domainConfig); err != nil {
		return fmt.Errorf("failed to parse domain xml: %s", err)
	}

	log.WithField("name", name).WithField("domain", domainConfig).Debug("domain xml fetched")

	switch info.State {
	default:
		vm.State = models.STATE_UNKNOWN
	case libvirt.DOMAIN_RUNNING:
		vm.State = models.STATE_RUNNING
	case libvirt.DOMAIN_SHUTDOWN:
		vm.State = models.STATE_STOPPED
	case libvirt.DOMAIN_SHUTOFF:
		vm.State = models.STATE_STOPPED

	}

	if len(domainConfig.Disks) > 0 {
		rootVolumeConfig := domainConfig.RootDisk()
		if rootVolumeConfig == nil {
			return fmt.Errorf("failed to find root disk")
		}
		rootVolume, err := store.conn.LookupStorageVolByPath(rootVolumeConfig.Source.Path())
		if err != nil {
			return err
		}
		rootVolumeInfo, err := rootVolume.GetInfo()
		if err != nil {
			return err
		}
		vm.RootDisk = &models.VirtualMachineDisk{}
		vm.RootDisk.Driver = rootVolumeConfig.Driver.Name
		vm.RootDisk.Type = rootVolumeConfig.Driver.Type
		vm.RootDisk.Size = rootVolumeInfo.Capacity
	}

	vm.Name = name
	vm.Id = fmt.Sprintf("%x", uuid)
	vm.Memory = int(info.Memory * 1024)
	vm.Cpus = int(info.NrVirtCpu)
	vm.HWAddr = domainConfig.Interfaces[0].Mac.Address
	vm.VNCAddr = domainConfig.VNCAddr()
	vm.Arch = models.ParseHWArch(domainConfig.Os.Type.Arch)
	vm.OS = domainConfig.OSName
	vm.Userdata = strings.TrimSpace(domainConfig.Userdata) + "\n"
	vm.ImageId = domainConfig.ImageId

	for _, key := range domainConfig.SSHKeys {
		vm.SSHKeys = append(vm.SSHKeys, &models.SSHKey{Name: key.Name, Public: key.Public})
	}

	networkXMLString, err := network.GetXMLDesc(0)
	if err != nil {
		return err
	}
	networkConfig := netXMLConfig{}
	if err := xml.Unmarshal([]byte(networkXMLString), &networkConfig); err != nil {
		return fmt.Errorf("failed to parse network xml: %s", err)
	}
	for _, host := range networkConfig.IP.Hosts {
		if host.HWAddr == vm.HWAddr {
			vm.Ip = &models.IP{Address: host.IPAddr}
		}
	}

	return nil
}

func (store *LibvirtMachinerep) isIgnored(name string) bool {
	for _, ignored := range store.ignoreVms {
		if name == ignored {
			return true
		}
	}
	return false
}

func (store *LibvirtMachinerep) List(machines *models.VirtualMachineList) error {
	domains, err := store.conn.ListAllDomains(0)
	if err != nil {
		return err
	}
	network, err := store.conn.LookupNetworkByName(store.network)
	if err != nil {
		return err
	}

	for _, domain := range domains {
		domainName, err := domain.GetName()
		if err != nil {
			panic(err)
		}
		if store.isIgnored(domainName) {
			continue
		}
		vm := &models.VirtualMachine{}
		if err := store.fillVm(vm, &domain, network); err != nil {
			return err
		}
		machines.Add(vm)
	}
	return nil
}

func (store *LibvirtMachinerep) Get(machine *models.VirtualMachine) (bool, error) {
	if machine.Id == "" {
		panic("no id specified for LibvirtMachinerep.Get()")
	}
	network, err := store.conn.LookupNetworkByName(store.network)
	if err != nil {
		return false, fmt.Errorf("failed to find network '%s'", err)
	}
	domain, err := store.conn.LookupDomainByUUIDString(machine.Id)
	if err != nil {
		virErr := err.(libvirt.Error)
		if virErr.Code == libvirt.ERR_NO_DOMAIN {
			return false, nil
		}
		return false, virErr
	}
	if err := store.fillVm(machine, domain, network); err != nil {
		return true, fmt.Errorf("failed to fetch machine info: %s", err)
	}
	return true, nil
}

type openstackMetadataFile struct {
}

type openstackMetadata struct {
	AZ          string                  `json:"availability_zone"`
	Files       []openstackMetadataFile `json:"files"`
	Hostname    string                  `json:"hostname"`
	LaunchIndex uint                    `json:"launch_index"`
	Name        string                  `json:"name"`
	Meta        map[string]string       `json:"meta"`
	PublicKeys  map[string]string       `json:"public_keys"`
	UUID        string                  `json:"uuid"`
}

func (store *LibvirtMachinerep) createConfigDrive(machine *models.VirtualMachine, pool *libvirt.StoragePool) (*libvirt.StorageVol, error) {
	tmpdir, err := ioutil.TempDir("", "")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpdir)
	metadataRoot := filepath.Join(tmpdir, "openstack", "latest")
	if err := os.MkdirAll(metadataRoot, 0755); err != nil {
		return nil, err
	}
	ud, err := os.Create(filepath.Join(metadataRoot, "user_data"))
	if err != nil {
		return nil, err
	}
	md, err := os.Create(filepath.Join(metadataRoot, "meta_data.json"))
	if err != nil {
		return nil, err
	}
	metadataPubkeys := map[string]string{}
	for _, key := range machine.SSHKeys {
		metadataPubkeys[key.Name] = key.Public
	}
	metadata := &openstackMetadata{
		AZ:          "none",
		Files:       []openstackMetadataFile{},
		Hostname:    machine.Name,
		LaunchIndex: 0,
		Name:        machine.Name,
		Meta:        map[string]string{},
		PublicKeys:  metadataPubkeys,
		UUID:        machine.Id,
	}
	mdContent, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	_, err = md.Write(mdContent)
	if err != nil {
		return nil, err
	}
	if err := md.Close(); err != nil {
		return nil, err
	}
	_, err = ud.Write([]byte(machine.Userdata))
	if err != nil {
		return nil, err
	}
	if err := ud.Close(); err != nil {
		return nil, err
	}

	cmd := exec.Command(
		"mkisofs", "-R", "-V", "config-2", "-o",
		filepath.Join(tmpdir, "drive.iso"), tmpdir,
	)
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	volumeXML := fmt.Sprintf(`
	<volume>
	  <target>
	  	<format type="raw" />
	  </target>
	  <name>%s_config.iso</name>
	  <capacity unit="b">1</capacity>
	</volume>`, machine.Name)
	volume, err := pool.StorageVolCreateXML(volumeXML, 0)
	if err != nil {
		return nil, err
	}
	content, err := os.Open(filepath.Join(tmpdir, "drive.iso"))
	if err != nil {
		return nil, err
	}
	contentSize, err := content.Seek(0, os.SEEK_END)
	if err != nil {
		return nil, err
	}
	_, err = content.Seek(0, os.SEEK_SET)
	if err != nil {
		return nil, err
	}

	stream, err := store.conn.NewStream(0)
	if err != nil {
		return nil, err
	}
	if err := volume.Upload(stream, 0, uint64(contentSize), 0); err != nil {
		return nil, err
	}
	stream.SendAll(func(stream *libvirt.Stream, n int) ([]byte, error) {
		buf := make([]byte, n)
		_, err := content.Read(buf)
		return buf, err
	})
	return volume, nil
}

func (store *LibvirtMachinerep) renderMetadata(machine *models.VirtualMachine) string {
	var buf bytes.Buffer
	context := struct {
		Machine *models.VirtualMachine
	}{machine}
	if err := METADATA_TEMPLATE.Execute(&buf, context); err != nil {
		panic(fmt.Errorf("failed to render metadata xml: %s", err))
	}
	return buf.String()
}

func (store *LibvirtMachinerep) Create(machine *models.VirtualMachine, image *models.Image, plan *models.Plan) error {
	storagePool, err := store.conn.LookupStoragePoolByName(store.storagePool)
	if err != nil {
		return fmt.Errorf("failed to lookup vm storage pool: %s", err.(libvirt.Error).Message)
	}

	imagePool, err := store.conn.LookupStoragePoolByName(image.PoolName)
	if err != nil {
		return fmt.Errorf("failed to lookup image storage pool: %s", err.(libvirt.Error).Message)
	}

	network, err := store.conn.LookupNetworkByName(store.network)
	if err != nil {
		return err
	}

	if _, err := store.conn.LookupDomainByName(machine.Name); err == nil {
		return fmt.Errorf("domain with name '%s' already exists", machine.Name)
	}

	var volumeXML bytes.Buffer
	voltplContext := struct {
		Machine *models.VirtualMachine
		Image   *models.Image
		Plan    *models.Plan
	}{machine, image, plan}
	if err := store.voltpl.Execute(&volumeXML, voltplContext); err != nil {
		return fmt.Errorf("failed to create volume xml from template: %s", err)
	}
	imageVolume, err := imagePool.LookupStorageVolByName(image.Id)
	if err != nil {
		return fmt.Errorf("failed to lookup image volume: %s", err)
	}

	log.WithField("xml", volumeXML.String()).Debug("defining volume from xml")
	rootVolume, err := storagePool.StorageVolCreateXMLFrom(volumeXML.String(), imageVolume, 0)
	if err != nil {
		return fmt.Errorf("failed to clone image: %s", err)
	}
	rootVolumePath, err := rootVolume.GetPath()
	if err != nil {
		return fmt.Errorf("failed to get machine volume path: %s", err)
	}

	machine.OS = image.OS
	machine.Arch = image.Arch
	machine.Memory = plan.Memory
	machine.Cpus = plan.Cpus
	machine.ImageId = image.Id

	var domainCreationXml bytes.Buffer
	vmtplContext := struct {
		Machine    *models.VirtualMachine
		Image      *models.Image
		Plan       *models.Plan
		VolumePath string
		Network    string
		Metadata   string
	}{machine, image, plan, rootVolumePath, store.network, store.renderMetadata(machine)}
	if err := store.vmtpl.Execute(&domainCreationXml, vmtplContext); err != nil {
		return err
	}
	log.WithField("xml", domainCreationXml.String()).Debug("defining domain from xml")
	domain, err := store.conn.DomainDefineXML(domainCreationXml.String())
	if err != nil {
		return err
	}
	if err := store.fillVm(machine, domain, network); err != nil {
		return err
	}
	if err := store.assignIP(machine); err != nil {
		return err
	}

	log.Debug("creating config drive")
	configDriveVolume, err := store.createConfigDrive(machine, storagePool)
	if err != nil {
		return fmt.Errorf("failed to create config drive: %s", err)
	}
	configDrivePath, err := configDriveVolume.GetPath()
	if err != nil {
		return err
	}

	atttachConfigDriveXML := fmt.Sprintf(`
    <disk type='file' device='cdrom'>
      <source file="%s" />
      <target dev='hdc' bus='ide'/>
      <readonly />
    </disk>
	`, configDrivePath)
	if err := domain.UpdateDeviceFlags(atttachConfigDriveXML, libvirt.DOMAIN_DEVICE_MODIFY_CONFIG); err != nil {
		return fmt.Errorf("failed to attach config drive: %s", err)
	}

	if machine.RootDisk.Type == "qcow2" {
		if err := rootVolume.Resize(uint64(plan.DiskSize), 0); err != nil {
			configDriveVolume.Delete(0)
			return fmt.Errorf("failed to resize root volume: %s", err)
		}
	}
	return store.fillVm(machine, domain, network)
}

func (store *LibvirtMachinerep) Remove(machine *models.VirtualMachine) error {
	if machine.Id == "" {
		panic("no id specified for machine remove")
	}
	storagePool, err := store.conn.LookupStoragePoolByName(store.storagePool)
	if err != nil {
		return fmt.Errorf("failed to lookup vm storage pool: %s", err)
	}
	if err := storagePool.Refresh(0); err != nil {
		return err
	}
	domain, err := store.conn.LookupDomainByUUIDString(machine.Id)
	if err != nil {
		return fmt.Errorf("failed to lookup domain: %s", err.(libvirt.Error).Message)
	}
	running, err := domain.IsActive()
	if err != nil {
		return err
	}
	if running {
		if err := domain.Destroy(); err != nil {
			return err
		}
	}
	domainXMLString, err := domain.GetXMLDesc(0)
	if err != nil {
		return err
	}

	domainXML := domainXMLConfig{}
	if err := xml.Unmarshal([]byte(domainXMLString), &domainXML); err != nil {
		return fmt.Errorf("failed to parse domain xml: %s", err)
	}
	for _, disk := range domainXML.Disks {
		lookupKey := disk.Source.Path()
		if lookupKey == "" {
			return fmt.Errorf("cannot find lookup key for volume '%s'", lookupKey)
		}
		volume, err := store.conn.LookupStorageVolByPath(lookupKey)
		if err != nil {
			return fmt.Errorf("failed to lookup domain disk by key '%s': %s", lookupKey, err)
		}
		if err := volume.Delete(libvirt.STORAGE_VOL_DELETE_NORMAL); err != nil {
			return err
		}
	}
	if err := store.releaseIP(machine); err != nil {
		return fmt.Errorf("failed to release machine ip: %s", err)
	}
	if err := domain.Undefine(); err != nil {
		return fmt.Errorf("failed to undefine domain: %s", err.(libvirt.Error).Message)
	}
	return nil
}

func (store *LibvirtMachinerep) Start(machine *models.VirtualMachine) error {
	domain, err := store.conn.LookupDomainByName(machine.Name)
	if err != nil {
		return err
	}
	return domain.Create()
}

func (store *LibvirtMachinerep) Stop(machine *models.VirtualMachine) error {
	domain, err := store.conn.LookupDomainByName(machine.Name)
	if err != nil {
		return err
	}
	return domain.Destroy()
}

func (store *LibvirtMachinerep) Reboot(machine *models.VirtualMachine) error {
	domain, err := store.conn.LookupDomainByName(machine.Name)
	if err != nil {
		return err
	}
	return domain.Reboot(libvirt.DOMAIN_REBOOT_DEFAULT)
}
