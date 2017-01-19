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
	"text/template"
	"vmango/models"
)

type diskSourceXMLConfig struct {
	File string `xml:"file,attr"`
}

type diskTargetXMLConfig struct {
	Device string `xml:"dev,attr"`
	Bus    string `xml:"bus,attr"`
}

type diskDriverXMLConfig struct {
	Name  string `xml:"name,attr"`
	Type  string `xml:"type,attr"`
	Cache string `xml:"cache,attr"`
}

type diskXMLConfig struct {
	Driver diskDriverXMLConfig `xml:"driver"`
	Target diskTargetXMLConfig `xml:"target"`
	Source diskSourceXMLConfig `xml:"source"`
}

type interfaceMacXMLConfig struct {
	Address string `xml:"address,attr"`
}

type interfaceXMLConfig struct {
	Type string                `xml:"type,attr"`
	Mac  interfaceMacXMLConfig `xml:"mac"`
}

type sshKeyConfig struct {
	Name   string `xml:"name,attr"`
	Public string `xml:",chardata"`
}

type domainXMLConfig struct {
	XMLName    xml.Name             `xml:"domain"`
	Name       string               `xml:"name"`
	Disks      []diskXMLConfig      `xml:"devices>disk"`
	Interfaces []interfaceXMLConfig `xml:"devices>interface"`
	SSHKeys    []sshKeyConfig       `xml:"metadata>md>sshkeys>key"`
}

type LibvirtMachinerep struct {
	conn        *libvirt.Connect
	vmtpl       *template.Template
	network     string
	storagePool string
}

func NewLibvirtMachinerep(conn *libvirt.Connect, tpl *template.Template, network, pool string) (*LibvirtMachinerep, error) {
	return &LibvirtMachinerep{conn: conn, vmtpl: tpl, network: network, storagePool: pool}, nil
}

func (store *LibvirtMachinerep) fillVm(vm *models.VirtualMachine, domain *libvirt.Domain) error {
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

	xmlString, err := domain.GetXMLDesc(0)
	if err != nil {
		return err
	}

	domainConfig := domainXMLConfig{}
	if err := xml.Unmarshal([]byte(xmlString), &domainConfig); err != nil {
		return fmt.Errorf("failed to parse domain xml:", err)
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
		volume, err := store.conn.LookupStorageVolByPath(domainConfig.Disks[0].Source.File)
		if err != nil {
			return err
		}
		volumeInfo, err := volume.GetInfo()
		if err != nil {
			return err
		}
		vm.Disk = &models.VirtualMachineDisk{}
		vm.Disk.Driver = domainConfig.Disks[0].Driver.Name
		vm.Disk.Size = volumeInfo.Capacity
	} else {
		vm.Disk = nil
	}

	vm.Name = name
	vm.Uuid = fmt.Sprintf("%x", uuid)
	vm.Memory = int(info.Memory)
	vm.Cpus = int(info.NrVirtCpu)
	vm.HWAddr = domainConfig.Interfaces[0].Mac.Address
	for _, key := range domainConfig.SSHKeys {
		vm.SSHKeys = append(vm.SSHKeys, &models.SSHKey{key.Name, key.Public})
	}
	return nil
}

func (store *LibvirtMachinerep) List(machines *models.VirtualMachineList) error {
	domains, err := store.conn.ListAllDomains(0)
	if err != nil {
		return err
	}
	for _, domain := range domains {
		vm := &models.VirtualMachine{}
		if err := store.fillVm(vm, &domain); err != nil {
			return err
		}
		machines.Add(vm)
	}
	return nil
}

func (store *LibvirtMachinerep) Get(machine *models.VirtualMachine) (bool, error) {
	if machine.Name == "" {
		panic("no name specified for LibvirtMachinerep.Get()")
	}

	domain, err := store.conn.LookupDomainByName(machine.Name)
	if err != nil {
		virErr := err.(libvirt.Error)
		if virErr.Code == libvirt.ERR_NO_DOMAIN {
			return false, nil
		}
		return false, virErr
	}
	store.fillVm(machine, domain)
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
		UUID:        machine.Uuid,
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

func (store *LibvirtMachinerep) Create(machine *models.VirtualMachine, image *models.Image, plan *models.Plan) error {
	storagePool, err := store.conn.LookupStoragePoolByName(store.storagePool)
	if err != nil {
		return fmt.Errorf("failed to lookup vm storage pool: %s", err)
	}

	imagePool, err := store.conn.LookupStoragePoolByName(image.PoolName)
	if err != nil {
		return fmt.Errorf("failed to lookup image storage pool: %s", err)
	}

	configDriveVolume, err := store.createConfigDrive(machine, storagePool)
	if err != nil {
		return fmt.Errorf("failed to create config drive: %s", err)
	}
	log.WithField("configdrive", configDriveVolume).Debug("config drive created successfully")
	configDrivePath, err := configDriveVolume.GetPath()
	if err != nil {
		return err
	}

	volumeXML := fmt.Sprintf(`
	<volume>
	  <target>
	  	<format type="%s" />
	  </target>
	  <name>%s_disk.%s</name>
	  <capacity unit="G">%d</capacity>
	  <allocation unit="G">%d</allocation>
	</volume>
	`, image.TypeString(), machine.Name, image.TypeString(), plan.DiskSizeGigabytes(), plan.DiskSizeGigabytes())
	imageVolume, err := imagePool.LookupStorageVolByName(image.FullName)
	if err != nil {
		return fmt.Errorf("failed to lookup image volume: %s", err)
	}

	rootVolume, err := storagePool.StorageVolCreateXMLFrom(volumeXML, imageVolume, 0)
	if err != nil {
		configDriveVolume.Delete(0)
		return fmt.Errorf("failed to clone image: %s", err)
	}
	if err := rootVolume.Resize(uint64(plan.DiskSize), 0); err != nil {
		configDriveVolume.Delete(0)
		return fmt.Errorf("failed to resize root volume: %s", err)
	}
	rootVolumePath, err := rootVolume.GetPath()
	if err != nil {
		return fmt.Errorf("failed to get machine volume path: %s", err)
	}
	var machineXml bytes.Buffer
	vmtplContext := struct {
		Machine     *models.VirtualMachine
		Image       *models.Image
		Plan        *models.Plan
		VolumePath  string
		ConfigDrive string
		Network     string
	}{machine, image, plan, rootVolumePath, configDrivePath, store.network}
	if err := store.vmtpl.Execute(&machineXml, vmtplContext); err != nil {
		configDriveVolume.Delete(0)
		return err
	}
	log.WithField("xml", machineXml.String()).Debug("defining domain from xml")
	domain, err := store.conn.DomainDefineXML(machineXml.String())
	if err != nil {
		return err
	}
	store.fillVm(machine, domain)
	return nil
}

func (store *LibvirtMachinerep) Remove(machine *models.VirtualMachine) error {
	storagePool, err := store.conn.LookupStoragePoolByName(store.storagePool)
	if err != nil {
		return fmt.Errorf("failed to lookup vm storage pool: %s", err)
	}
	if err := storagePool.Refresh(0); err != nil {
		return err
	}
	domain, err := store.conn.LookupDomainByName(machine.Name)
	if err != nil {
		return err
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
		return fmt.Errorf("failed to parse domain xml:", err)
	}
	for _, disk := range domainXML.Disks {
		volume, err := store.conn.LookupStorageVolByPath(disk.Source.File)
		if err != nil {
			return err
		}
		if err := volume.Delete(libvirt.STORAGE_VOL_DELETE_NORMAL); err != nil {
			return err
		}
	}
	return domain.Undefine()
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
