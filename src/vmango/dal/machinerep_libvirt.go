package dal

import (
	"bytes"
	"encoding/xml"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"gopkg.in/alexzorin/libvirt-go.v2"
	"io"
	"os"
	"text/template"
	"vmango/models"
)

type LibvirtMachinerep struct {
	conn  libvirt.VirConnection
	vmtpl *template.Template
}

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

type domainXMLConfig struct {
	XMLName xml.Name        `xml:"domain"`
	Name    string          `xml:"name"`
	Disks   []diskXMLConfig `xml:"devices>disk"`
}

func NewLibvirtMachinerep(conn libvirt.VirConnection, tpl *template.Template) (*LibvirtMachinerep, error) {
	return &LibvirtMachinerep{conn: conn, vmtpl: tpl}, nil
}

func (store *LibvirtMachinerep) fillVm(vm *models.VirtualMachine, domain libvirt.VirDomain) error {
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

	switch info.GetState() {
	default:
		vm.State = models.STATE_UNKNOWN
	case 1:
		vm.State = models.STATE_RUNNING
	case 5:
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
		vm.Disk.Size = volumeInfo.GetCapacityInBytes()
	} else {
		vm.Disk = nil
	}

	vm.Name = name
	vm.Uuid = fmt.Sprintf("%x", uuid)
	vm.Memory = int(info.GetMaxMem())
	vm.Cpus = int(info.GetNrVirtCpu())
	return nil
}

func (store *LibvirtMachinerep) List(machines *models.VirtualMachineList) error {
	domains, err := store.conn.ListAllDomains(0)
	if err != nil {
		return err
	}
	for _, domain := range domains {
		vm := &models.VirtualMachine{}
		if err := store.fillVm(vm, domain); err != nil {
			return err
		}
		machines.Add(vm)
	}
	return nil
}

func (store *LibvirtMachinerep) Get(machine *models.VirtualMachine) (bool, error) {
	if machine.Name == "" {
		return false, nil
	}

	domain, err := store.conn.LookupDomainByName(machine.Name)
	if err != nil {
		virErr := err.(libvirt.VirError)
		if virErr.Code == libvirt.VIR_ERR_NO_DOMAIN {
			return false, nil
		}
		return false, virErr
	}
	store.fillVm(machine, domain)
	return true, nil
}

func (store *LibvirtMachinerep) Create(machine *models.VirtualMachine, image *models.Image, plan *models.Plan) error {

	storagePool, err := store.conn.LookupStoragePoolByName("default")
	if err != nil {
		return fmt.Errorf("failed to lookup storage pool: %s", err)
	}

	volumeXML := fmt.Sprintf(`
	<volume>
	  <name>%s_disk.%s</name>
	  <capacity unit="b">1</capacity>
	</volume>
	`, machine.Name, image.TypeString())

	volume, err := storagePool.StorageVolCreateXML(volumeXML, 0)
	if err != nil {
		return fmt.Errorf("failed to create storage volume: %s", err)
	}
	volumePath, err := volume.GetPath()
	if err != nil {
		return fmt.Errorf("failed to get volume path: %s", err)
	}

	imageStream, err := image.Stream()
	if err != nil {
		return fmt.Errorf("failed to open image file: %s", err)
	}
	defer imageStream.Close()

	vmdriveStream, err := os.Create(volumePath)
	if err != nil {
		return fmt.Errorf("failed to open vm drive: %s", err)
	}
	defer vmdriveStream.Close()

	if _, err := io.Copy(vmdriveStream, imageStream); err != nil {
		return fmt.Errorf("failed to copy image content to vm drive: %s", err)
	}

	var machineXml bytes.Buffer
	vmtplContext := struct {
		Machine    *models.VirtualMachine
		Image      *models.Image
		Plan       *models.Plan
		VolumePath string
	}{machine, image, plan, volumePath}
	if err := store.vmtpl.Execute(&machineXml, vmtplContext); err != nil {
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

func (store *LibvirtMachinerep) Start(machine *models.VirtualMachine) error {
	domain, err := store.conn.LookupDomainByName(machine.Name)
	if err != nil {
		return err
	}
	return domain.Create()
}
