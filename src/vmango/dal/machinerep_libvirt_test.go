package dal_test

import (
	"encoding/xml"
	"fmt"
	"github.com/libvirt/libvirt-go"
	"github.com/stretchr/testify/suite"
	"os"
	"strings"
	"testing"
	"text/template"
	"vmango/dal"
	"vmango/models"
	"vmango/testool"
)

const TEST_URI_ENV_KEY = "VMANGO_TEST_LIBVIRT_URI"

const VOLUME_TEMPLATE = `
<volume>
  <target>
    <format type="{{ .Image.TypeString }}" />
  </target>
  <name>{{ .Machine.Name }}_disk</name>
  <key>{{ .Machine.Name }}_disk.{{ .Image.TypeString }}</key>
  <capacity unit="G">{{ .Plan.DiskSizeGigabytes }}</capacity>
  <allocation unit="G">{{ .Plan.DiskSizeGigabytes }}</allocation>
</volume>
`

const VM_TEMPLATE = `
<domain type='kvm'>
  <name>{{ .Machine.Name }}</name>
  <currentMemory unit="b">{{ .Plan.Memory }}</currentMemory>
  <metadata>
    <vmango:md xmlns:vmango="http://vmango.org/schema/md">
      <vmango:sshkeys>
        {{ range .Machine.SSHKeys }}
        <vmango:key name="{{ .Name }}">{{ .Public }}</vmango:key>
        {{ end }}
      </vmango:sshkeys>
    </vmango:md>
  </metadata>
  <memory unit="b">{{ .Plan.Memory }}</memory>
  <os>
    <type arch='{{ .Image.ArchString2 }}'>hvm</type>
    <boot dev='hd'/>
  </os>
  <features>
    <acpi/>
    <apic/>
    <pae/>
  </features>
  <clock offset="utc"/>

  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>restart</on_crash>

  <vcpu>{{ .Plan.Cpus }}</vcpu>

  <devices>
    <emulator>/usr/bin/kvm-spice</emulator>
    <disk type='file' device='disk'>
      <driver name='qemu' type='{{ .Image.TypeString }}' cache='none'/>
      <source file='{{ .VolumePath }}'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <target dev='hdc' bus='ide'/>
      <readonly/>
    </disk>
    <interface type='network'>
      <source network='{{ .Network }}'/>
      <model type='virtio'/>
    </interface>
    <input type='tablet' bus='usb'/>
    <graphics type='vnc' port='-1'/>
    <console type='pty'/>
    <sound model='ac97'/>
    <video>
      <model type='cirrus'/>
    </video>
  </devices>
</domain>
`

type MachinerepLibvirtSuite struct {
	suite.Suite
	VMTpl      *template.Template
	VolTpl     *template.Template
	Network    string
	VmPool     string
	ImagePool  string
	VirConnect *libvirt.Connect

	domains  []*libvirt.Domain
	networks []*libvirt.Network
	pools    []*libvirt.StoragePool
}

func (suite *MachinerepLibvirtSuite) SetupTest() {
	suite.VMTpl = template.Must(template.New("vm.xml.in").Parse(VM_TEMPLATE))
	suite.VolTpl = template.Must(template.New("volume.xml.in").Parse(VOLUME_TEMPLATE))
	suite.VmPool = "vmango-vms-test"
	suite.ImagePool = "vmango-images-test"
	suite.Network = "vmango"

	uri := os.Getenv(TEST_URI_ENV_KEY)
	if uri == "" {
		suite.FailNow(fmt.Sprintf("no %s specified", TEST_URI_ENV_KEY))
		uri = fmt.Sprintf("test:///%s/libvirt_testxml/node.xml", testool.SourceDir())
	}
	virConn, err := libvirt.NewConnect(uri)
	if err != nil {
		panic(err)
	}

	suite.VirConnect = virConn
	for _, name := range []string{"vms", "images"} {
		pool, err := testool.CreatePool(virConn, name)
		if err != nil {
			pool, _ := virConn.LookupStoragePoolByName("vmango-" + name + "-test")
			suite.pools = append(suite.pools, pool)
			suite.TearDownTest()
		}
		suite.Require().NoError(err)
		suite.pools = append(suite.pools, pool)
	}
	for _, name := range []string{"one_disk", "one_config.iso", "two_disk", "two_config.iso"} {
		suite.Require().NoError(testool.CreateVolume(virConn, suite.VmPool, name))
	}
	for _, name := range []string{"one", "two"} {
		domain, err := testool.CreateDomain(virConn, name)
		suite.Require().NoError(err)
		suite.domains = append(suite.domains, domain)
	}
	for _, name := range []string{"vmango"} {
		network, err := testool.CreateNetwork(virConn, name)
		suite.Require().NoError(err)
		suite.networks = append(suite.networks, network)
	}
}

func (suite *MachinerepLibvirtSuite) TearDownTest() {
	suite.T().Log("teardown called")
	for _, domain := range suite.domains {
		suite.T().Log("Destroying domain:", domain)
		domain.Destroy()
		domain.Undefine()
	}
	for _, network := range suite.networks {
		suite.T().Log("Destroying network:", network)
		network.Destroy()
		network.Undefine()
	}

	for _, pool := range suite.pools {
		suite.T().Log("Destroying pool:", pool)
		vols, err := pool.ListStorageVolumes()
		suite.Require().NoError(err)
		for _, volName := range vols {
			vol, _ := pool.LookupStorageVolByName(volName)
			vol.Delete(0)
		}
		pool.Destroy()
		pool.Undefine()
	}
	suite.pools = []*libvirt.StoragePool{}
	suite.networks = []*libvirt.Network{}
	suite.domains = []*libvirt.Domain{}
	suite.VirConnect.Close()
}

func (suite *MachinerepLibvirtSuite) CreateRep(ignoreVms ...string) *dal.LibvirtMachinerep {
	repo, err := dal.NewLibvirtMachinerep(
		suite.VirConnect, suite.VMTpl, suite.VolTpl,
		suite.Network, suite.VmPool, ignoreVms,
	)
	if err != nil {
		panic(err)
	}
	return repo
}

func (suite *MachinerepLibvirtSuite) TestListOk() {
	machines := &models.VirtualMachineList{}
	err := suite.CreateRep().List(machines)
	suite.Require().NoError(err)
	suite.Require().Equal(machines.Count(), 2)
	oneVm := machines.Find("one")
	suite.NotNil(oneVm)
	suite.Equal("one", oneVm.Name)
	suite.Equal(models.STATE_RUNNING, oneVm.State)
	suite.Equal("fb6c4f622cf346239aee23f0005eb5fb", oneVm.Uuid)
	suite.Equal(524288, oneVm.Memory)
	suite.Equal(1, oneVm.Cpus)
	suite.Equal("", oneVm.ImageName)
	suite.Equal("52:54:00:2e:54:28", oneVm.HWAddr)
	suite.Equal("127.0.0.1:5900", oneVm.VNCAddr)
	suite.Equal(oneVm.Disk.Type, "raw")
	suite.Equal(uint64(10485760), oneVm.Disk.Size)
	suite.Equal("qemu", oneVm.Disk.Driver)
	suite.Len(oneVm.SSHKeys, 1)
	suite.Equal("test", oneVm.SSHKeys[0].Name)
	expectedKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDXJjuFhloSumFjJRrrZfSinBE0q4e/o0nKzt4QfkD3VR56rrrrCtjHh+/wcZcIdm9I9QODxoFoSSvrPNOzLj0lfF0f64Ic7fUnC4hhRBEeyo/03KVpUQcHWHjeex+5OHQXa8s5Xy/dytZkhdvDYOCgEpMgC2tU6tk/mVuk84Q03QEnSYJQuIgj8VwvxC+22aGSpLzXtenpdXr+O8s7dkuhHQjl1w6WbiLADv0I06bFwW8iB6p7aHZCqJUYAUYa4XaCjXdVwoKAE/J23s17XCZzY10YmBIikRQQIjpvRIbHArzO0om4++2KMnY8m6xoMp2imyceD/0fIVlAqhLTEaBP test@vmango"
	suite.Equal(expectedKey, oneVm.SSHKeys[0].Public)
	suite.Nil(oneVm.Ip)

	twoVm := machines.Find("two")
	suite.NotNil(twoVm)
	suite.Equal("two", twoVm.Name)
	suite.Equal(models.STATE_RUNNING, twoVm.State)
	suite.Equal("c72cb377301a4f2aa34c547f70872b55", twoVm.Uuid)
	suite.Equal(524288, twoVm.Memory)
	suite.Equal(1, twoVm.Cpus)
	suite.Equal("", twoVm.ImageName)
	suite.Equal("52:54:00:2e:54:29", twoVm.HWAddr)
	suite.Equal("127.0.0.1:5901", twoVm.VNCAddr)
	suite.Equal("raw", twoVm.Disk.Type)
	suite.Equal(uint64(10485760), twoVm.Disk.Size)
	suite.Equal("qemu", twoVm.Disk.Driver)
	suite.Len(twoVm.SSHKeys, 1)
	suite.Equal("test", twoVm.SSHKeys[0].Name)
	expectedKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDXJjuFhloSumFjJRrrZfSinBE0q4e/o0nKzt4QfkD3VR56rrrrCtjHh+/wcZcIdm9I9QODxoFoSSvrPNOzLj0lfF0f64Ic7fUnC4hhRBEeyo/03KVpUQcHWHjeex+5OHQXa8s5Xy/dytZkhdvDYOCgEpMgC2tU6tk/mVuk84Q03QEnSYJQuIgj8VwvxC+22aGSpLzXtenpdXr+O8s7dkuhHQjl1w6WbiLADv0I06bFwW8iB6p7aHZCqJUYAUYa4XaCjXdVwoKAE/J23s17XCZzY10YmBIikRQQIjpvRIbHArzO0om4++2KMnY8m6xoMp2imyceD/0fIVlAqhLTEaBP test@vmango"
	suite.Equal(expectedKey, twoVm.SSHKeys[0].Public)
	suite.Nil(twoVm.Ip)
}

func (suite *MachinerepLibvirtSuite) TestIgnoredOk() {
	repo := suite.CreateRep("one")
	machines := &models.VirtualMachineList{}
	err := repo.List(machines)
	suite.Require().NoError(err)
	suite.Equal(machines.Count(), 1)
	suite.Equal("two", machines.All()[0].Name)
}

func (suite *MachinerepLibvirtSuite) TestGetOk() {
	repo := suite.CreateRep()
	machine := &models.VirtualMachine{Name: "two"}
	exists, err := repo.Get(machine)
	suite.Require().True(exists)
	suite.Require().Nil(err)

	suite.Equal("two", machine.Name)
	suite.Equal(models.STATE_RUNNING, machine.State)
	suite.Equal("c72cb377301a4f2aa34c547f70872b55", machine.Uuid)
	suite.Equal(524288, machine.Memory)
	suite.Equal(1, machine.Cpus)
	suite.Equal("", machine.ImageName)
	suite.Equal("52:54:00:2e:54:29", machine.HWAddr)
	suite.Equal("127.0.0.1:5901", machine.VNCAddr)
	suite.Equal("raw", machine.Disk.Type)
	suite.Equal(uint64(10485760), machine.Disk.Size)
	suite.Equal("qemu", machine.Disk.Driver)
	suite.Len(machine.SSHKeys, 1)
	suite.Equal("test", machine.SSHKeys[0].Name)
	expectedKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDXJjuFhloSumFjJRrrZfSinBE0q4e/o0nKzt4QfkD3VR56rrrrCtjHh+/wcZcIdm9I9QODxoFoSSvrPNOzLj0lfF0f64Ic7fUnC4hhRBEeyo/03KVpUQcHWHjeex+5OHQXa8s5Xy/dytZkhdvDYOCgEpMgC2tU6tk/mVuk84Q03QEnSYJQuIgj8VwvxC+22aGSpLzXtenpdXr+O8s7dkuhHQjl1w6WbiLADv0I06bFwW8iB6p7aHZCqJUYAUYa4XaCjXdVwoKAE/J23s17XCZzY10YmBIikRQQIjpvRIbHArzO0om4++2KMnY8m6xoMp2imyceD/0fIVlAqhLTEaBP test@vmango"
	suite.Equal(expectedKey, machine.SSHKeys[0].Public)
	suite.Nil(machine.Ip)
}

func (suite *MachinerepLibvirtSuite) TestGetNotFoundFail() {
	repo := suite.CreateRep()
	machine := &models.VirtualMachine{Name: "doesntexist"}
	exists, err := repo.Get(machine)
	suite.Require().False(exists)
	suite.Require().Nil(err)
}

func (suite *MachinerepLibvirtSuite) TestGetNoNameFail() {
	repo := suite.CreateRep()
	machine := &models.VirtualMachine{}
	suite.Require().Panics(func() {
		repo.Get(machine)
	})
}

func (suite *MachinerepLibvirtSuite) TestRemoveOk() {
	repo := suite.CreateRep()
	machine := &models.VirtualMachine{Name: "one"}
	suite.T().Log("Waiting for domain")
	err := repo.Remove(machine)
	suite.Require().NoError(err)

	domain, err := suite.VirConnect.LookupDomainByName("one")
	suite.Require().NotNil(err)
	suite.Require().Nil(domain)
	suite.Require().Contains(err.(libvirt.Error).Message, "Domain not found")

	_, err = suite.VirConnect.LookupStorageVolByPath("/var/lib/libvirt/images/one_disk")
	suite.Require().Equal(err.(libvirt.Error).Message, "Storage volume not found: no storage vol with matching path '/var/lib/libvirt/images/one_disk'")
	_, err = suite.VirConnect.LookupStorageVolByPath("/var/lib/libvirt/images/one_config.iso")
	suite.Require().Equal(err.(libvirt.Error).Message, "Storage volume not found: no storage vol with matching path '/var/lib/libvirt/images/one_config.iso'")
}

func (suite *MachinerepLibvirtSuite) TestRemoveNotFoundFail() {
	repo := suite.CreateRep()
	machine := &models.VirtualMachine{Name: "doesntexist"}
	err := repo.Remove(machine)
	suite.Require().NotNil(err)
	suite.T().Log(err.Error())
	suite.Require().Contains(err.Error(), "Domain not found")
}

func (suite *MachinerepLibvirtSuite) TestRemoveNoNameFail() {
	repo := suite.CreateRep()
	machine := &models.VirtualMachine{}
	suite.Require().Panics(func() {
		repo.Remove(machine)
	})
}

func (suite *MachinerepLibvirtSuite) TestCreateNoImagePoolFail() {
	repo := suite.CreateRep()
	machine := &models.VirtualMachine{}
	image := &models.Image{PoolName: "doesntexist"}
	plan := &models.Plan{}
	err := repo.Create(machine, image, plan)
	suite.Require().EqualError(err, "failed to lookup image storage pool: Storage pool not found: no storage pool with matching name 'doesntexist'")
}

func (suite *MachinerepLibvirtSuite) TestCreateNoVMPoolFail() {
	suite.VmPool = "doesntexist"
	repo := suite.CreateRep()
	machine := &models.VirtualMachine{}
	image := &models.Image{PoolName: suite.ImagePool}
	plan := &models.Plan{}
	err := repo.Create(machine, image, plan)
	suite.Require().EqualError(err, "failed to lookup vm storage pool: Storage pool not found: no storage pool with matching name 'doesntexist'")
}

func (suite *MachinerepLibvirtSuite) TestCreateOk() {
	repo := suite.CreateRep()
	image := &models.Image{
		OS:       "Ubuntu-12.04",
		Arch:     models.IMAGE_ARCH_X86_64,
		Type:     models.IMAGE_FMT_QCOW2,
		PoolName: suite.ImagePool,
		FullName: "test-image",
	}
	if err := testool.CreateVolume(suite.VirConnect, suite.ImagePool, image.FullName); err != nil {
		suite.FailNow("failed to create image volume", err.Error())
	}

	plan := &models.Plan{
		Name:     "small",
		Memory:   512 * 1024 * 1024,
		Cpus:     2,
		DiskSize: 5 * 1024 * 1024 * 1024,
	}
	machine := &models.VirtualMachine{
		Name: "test-create",
		SSHKeys: []*models.SSHKey{
			{Name: "home", Public: "asdf"},
			{Name: "work", Public: "hello"},
		},
	}
	err := repo.Create(machine, image, plan)
	suite.Require().NoError(err)
	domain, err := suite.VirConnect.LookupDomainByName("test-create")
	suite.Require().NoError(err)
	suite.domains = append(suite.domains, domain)
	domainXMLString, err := domain.GetXMLDesc(0)
	suite.Require().NoError(err)
	domainConfig := struct {
		Memory  string `xml:"memory"`
		Cpus    string `xml:"vcpu"`
		Name    string `xml:"name"`
		SSHKeys []struct {
			Name   string `xml:"name,attr"`
			Public string `xml:",chardata"`
		} `xml:"metadata>md>sshkeys>key"`
		Disks []struct {
			Type   string `xml:"type,attr"`
			Device string `xml:"device,attr"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
		} `xml:"devices>disk"`
	}{}
	if err := xml.Unmarshal([]byte(domainXMLString), &domainConfig); err != nil {
		suite.Require().NoError(err)
	}
	suite.Equal("524288", domainConfig.Memory)
	suite.Equal("2", domainConfig.Cpus)
	suite.Equal("test-create", domainConfig.Name)
	suite.Len(domainConfig.SSHKeys, 2)
	suite.Equal(domainConfig.SSHKeys[0].Name, "home")
	suite.Equal(domainConfig.SSHKeys[0].Public, "asdf")
	suite.Equal(domainConfig.SSHKeys[1].Name, "work")
	suite.Equal(domainConfig.SSHKeys[1].Public, "hello")
	suite.Len(domainConfig.Disks, 2)
	suite.Equal(domainConfig.Disks[0].Device, "disk")
	suite.Equal(domainConfig.Disks[0].Type, "file")
	suite.True(strings.HasSuffix(domainConfig.Disks[0].Source.File, "_disk"))
	suite.Equal(domainConfig.Disks[1].Device, "cdrom")
	suite.Equal(domainConfig.Disks[1].Type, "file")
	suite.True(strings.HasSuffix(domainConfig.Disks[1].Source.File, "_config.iso"))
}

func TestMachinerepLibvirtSuite(t *testing.T) {
	if os.Getenv(TEST_URI_ENV_KEY) == "" {
		t.Skip(fmt.Sprintf("Skipping libvirt machinerep tests due to env var %s not set", TEST_URI_ENV_KEY))
	}
	suite.Run(t, new(MachinerepLibvirtSuite))
}
