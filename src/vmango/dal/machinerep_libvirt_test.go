// +build integration

package dal_test

import (
	"encoding/xml"
	"github.com/libvirt/libvirt-go"
	"github.com/stretchr/testify/suite"
	"regexp"
	"strings"
	"testing"
	"vmango/dal"
	"vmango/models"
	"vmango/testool"
)

type MachinerepLibvirtSuite struct {
	suite.Suite
	testool.LibvirtTest
}

func (suite *MachinerepLibvirtSuite) SetupSuite() {
	suite.LibvirtTest.SetupSuite()
	suite.LibvirtTest.Fixtures.Domains = []string{"one", "two"}
	suite.LibvirtTest.Fixtures.Networks = []string{"vmango-test"}
	suite.LibvirtTest.Fixtures.Pools = []testool.LibvirtTestPoolFixture{
		{
			Name:    "vmango-vms-test",
			Volumes: []string{"one_disk", "one_config.iso", "two_disk", "two_config.iso"},
		},
		{
			Name:    "vmango-images-test",
			Volumes: []string{},
		},
	}
}

func MustRepo(repo *dal.LibvirtMachinerep, err error) *dal.LibvirtMachinerep {
	if err != nil {
		panic(err)
	}
	return repo
}

func (suite *MachinerepLibvirtSuite) CreateRep() *dal.LibvirtMachinerep {
	return MustRepo(dal.NewLibvirtMachinerep(
		suite.VirConnect, suite.VMTpl, suite.VolTpl,
		suite.Fixtures.Networks[0], suite.Fixtures.Pools[0].Name, []string{},
	))
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
	suite.Equal(536870912, oneVm.Memory)
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
	suite.Equal(536870912, twoVm.Memory)
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
	repo := MustRepo(dal.NewLibvirtMachinerep(
		suite.VirConnect, suite.VMTpl, suite.VolTpl,
		suite.Fixtures.Networks[0], suite.Fixtures.Pools[0].Name,
		[]string{"one"},
	))
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
	suite.Equal(536870912, machine.Memory)
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
	expected := "Storage volume not found: no storage vol with matching path '?/var/lib/libvirt/images/one_disk'?"
	actual := err.(libvirt.Error).Message
	suite.Require().Regexp(regexp.MustCompile(expected), actual)

	_, err = suite.VirConnect.LookupStorageVolByPath("/var/lib/libvirt/images/one_config.iso")
	expected = "Storage volume not found: no storage vol with matching path '?/var/lib/libvirt/images/one_config.iso'?"
	actual = err.(libvirt.Error).Message
	suite.Require().Regexp(regexp.MustCompile(expected), actual)
	// suite.Require().Equal(expected, actual)
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
	repo := MustRepo(dal.NewLibvirtMachinerep(
		suite.VirConnect, suite.VMTpl, suite.VolTpl,
		suite.Fixtures.Networks[0], "doesntexist",
		[]string{"one"},
	))
	machine := &models.VirtualMachine{}
	image := &models.Image{PoolName: suite.Fixtures.Pools[1].Name}
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
		PoolName: suite.Fixtures.Pools[1].Name,
		FullName: "test-image",
	}
	if err := testool.CreateVolume(suite.VirConnect, suite.Fixtures.Pools[1].Name, image.FullName); err != nil {
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
	suite.AddCleanup(domain)
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
	suite.Run(t, new(MachinerepLibvirtSuite))
}
