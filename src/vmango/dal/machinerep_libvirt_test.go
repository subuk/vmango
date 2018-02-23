// +build integration

package dal_test

import (
	"encoding/xml"
	"strings"
	"testing"
	"vmango/dal"
	"vmango/domain"
	"vmango/testool"

	"github.com/libvirt/libvirt-go"
	"github.com/stretchr/testify/suite"
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

func (suite *MachinerepLibvirtSuite) CreateRep(params ...map[string]interface{}) *dal.LibvirtMachinerep {
	ignoreVms := ""
	networkScript := ""
	vmPool := suite.Fixtures.Pools[0].Name

	if len(params) > 0 {
		if v, ok := params[0]["ignore_vms"].(string); ok {
			ignoreVms = v
		}
		if v, ok := params[0]["vm_pool"].(string); ok {
			vmPool = v
		}
		if v, ok := params[0]["network_script"].(string); ok {
			networkScript = v
		}
	}
	provider, err := dal.ProviderFactory(&domain.ProviderConfig{
		Name: "test-libvirt",
		Type: dal.LibvirtProvider,
		Params: map[string]string{
			"url":                suite.LibvirtTest.VirURI,
			"machine_template":   suite.LibvirtTest.VMTplContent,
			"volume_template":    suite.LibvirtTest.VolTplContent,
			"network":            suite.Fixtures.Networks[0],
			"network_script":     networkScript,
			"root_storage_pool":  vmPool,
			"image_storage_pool": suite.Fixtures.Pools[1].Name,
			"ignore_vms":         ignoreVms,
		},
	})
	if err != nil {
		panic(err)
	}
	return provider.Machines.(*dal.LibvirtMachinerep)
}

func (suite *MachinerepLibvirtSuite) TestListOk() {
	machines := &domain.VirtualMachineList{}
	err := suite.CreateRep().List(machines)
	suite.Require().NoError(err)
	suite.Require().Equal(machines.Count(), 2)
	oneVm := machines.Find("one")
	suite.Require().NotNil(oneVm)
	suite.Equal("one", oneVm.Name)
	suite.Equal(domain.STATE_RUNNING, oneVm.State)
	suite.Equal("fb6c4f622cf346239aee23f0005eb5fb", oneVm.Id)
	suite.Equal(536870912, oneVm.Memory)
	suite.Equal(1, oneVm.Cpus)
	suite.Equal("image-stub-000", oneVm.ImageId)
	suite.Equal("52:54:00:2e:54:28", oneVm.HWAddr)
	suite.Equal("127.0.0.1:5900", oneVm.VNCAddr)
	suite.Equal(oneVm.RootDisk.Type, "raw")
	suite.NotEqual(0, oneVm.RootDisk.Size)
	suite.Equal("qemu", oneVm.RootDisk.Driver)
	suite.Len(oneVm.SSHKeys, 1)
	suite.Equal("test", oneVm.SSHKeys[0].Name)
	expectedKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDXJjuFhloSumFjJRrrZfSinBE0q4e/o0nKzt4QfkD3VR56rrrrCtjHh+/wcZcIdm9I9QODxoFoSSvrPNOzLj0lfF0f64Ic7fUnC4hhRBEeyo/03KVpUQcHWHjeex+5OHQXa8s5Xy/dytZkhdvDYOCgEpMgC2tU6tk/mVuk84Q03QEnSYJQuIgj8VwvxC+22aGSpLzXtenpdXr+O8s7dkuhHQjl1w6WbiLADv0I06bFwW8iB6p7aHZCqJUYAUYa4XaCjXdVwoKAE/J23s17XCZzY10YmBIikRQQIjpvRIbHArzO0om4++2KMnY8m6xoMp2imyceD/0fIVlAqhLTEaBP test@vmango"
	suite.Equal(expectedKey, oneVm.SSHKeys[0].Public)
	suite.Equal("192.168.128.130", oneVm.Ip.Address)
	suite.Equal("x86_64", oneVm.Arch.String())
	suite.Equal("#!/bin/sh\n", oneVm.Userdata)
	suite.Equal("StubOs-1.0", oneVm.OS)
	suite.Equal("stubuser", oneVm.Creator)
	suite.Equal("medium", oneVm.Plan)

	twoVm := machines.Find("two")
	suite.Require().NotNil(twoVm)
	suite.Equal("two", twoVm.Name)
	suite.Equal(domain.STATE_RUNNING, twoVm.State)
	suite.Equal("c72cb377301a4f2aa34c547f70872b55", twoVm.Id)
	suite.Equal(536870912, twoVm.Memory)
	suite.Equal(1, twoVm.Cpus)
	suite.Equal("image-stub-000", twoVm.ImageId)
	suite.Equal("52:54:00:2e:54:29", twoVm.HWAddr)
	suite.Equal("127.0.0.1:5901", twoVm.VNCAddr)
	suite.Equal("raw", twoVm.RootDisk.Type)
	suite.NotEqual(0, twoVm.RootDisk.Size)
	suite.Equal("qemu", twoVm.RootDisk.Driver)
	suite.Len(twoVm.SSHKeys, 1)
	suite.Equal("test", twoVm.SSHKeys[0].Name)
	expectedKey = "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDXJjuFhloSumFjJRrrZfSinBE0q4e/o0nKzt4QfkD3VR56rrrrCtjHh+/wcZcIdm9I9QODxoFoSSvrPNOzLj0lfF0f64Ic7fUnC4hhRBEeyo/03KVpUQcHWHjeex+5OHQXa8s5Xy/dytZkhdvDYOCgEpMgC2tU6tk/mVuk84Q03QEnSYJQuIgj8VwvxC+22aGSpLzXtenpdXr+O8s7dkuhHQjl1w6WbiLADv0I06bFwW8iB6p7aHZCqJUYAUYa4XaCjXdVwoKAE/J23s17XCZzY10YmBIikRQQIjpvRIbHArzO0om4++2KMnY8m6xoMp2imyceD/0fIVlAqhLTEaBP test@vmango"
	suite.Equal(expectedKey, twoVm.SSHKeys[0].Public)
	suite.Equal("StubOs-1.0", twoVm.OS)
	suite.Equal("x86_64", twoVm.Arch.String())
	suite.Equal("stubuser", twoVm.Creator)
	suite.Equal("large", twoVm.Plan)
}

func (suite *MachinerepLibvirtSuite) TestIgnoredOk() {
	repo := suite.CreateRep(map[string]interface{}{"ignore_vms": "one"})
	machines := &domain.VirtualMachineList{}
	err := repo.List(machines)
	suite.Require().NoError(err)
	suite.Equal(machines.Count(), 1)
	suite.Equal("two", machines.All()[0].Name)
}

func (suite *MachinerepLibvirtSuite) TestGetOk() {
	repo := suite.CreateRep()
	machine := &domain.VirtualMachine{Id: "c72cb377301a4f2aa34c547f70872b55"}
	exists, err := repo.Get(machine)
	suite.Require().True(exists)
	suite.Require().Nil(err)

	suite.Equal("two", machine.Name)
	suite.Equal(domain.STATE_RUNNING, machine.State)
	suite.Equal("c72cb377301a4f2aa34c547f70872b55", machine.Id)
	suite.Equal(536870912, machine.Memory)
	suite.Equal(1, machine.Cpus)
	suite.Equal("image-stub-000", machine.ImageId)
	suite.Equal("#!/bin/sh\n", machine.Userdata)
	suite.Equal("stubuser", machine.Creator)
	suite.Equal("52:54:00:2e:54:29", machine.HWAddr)
	suite.Equal("127.0.0.1:5901", machine.VNCAddr)
	suite.Equal("raw", machine.RootDisk.Type)
	suite.NotEqual(0, machine.RootDisk.Size)
	suite.Equal("qemu", machine.RootDisk.Driver)
	suite.Len(machine.SSHKeys, 1)
	suite.Equal("test", machine.SSHKeys[0].Name)
	expectedKey := "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDXJjuFhloSumFjJRrrZfSinBE0q4e/o0nKzt4QfkD3VR56rrrrCtjHh+/wcZcIdm9I9QODxoFoSSvrPNOzLj0lfF0f64Ic7fUnC4hhRBEeyo/03KVpUQcHWHjeex+5OHQXa8s5Xy/dytZkhdvDYOCgEpMgC2tU6tk/mVuk84Q03QEnSYJQuIgj8VwvxC+22aGSpLzXtenpdXr+O8s7dkuhHQjl1w6WbiLADv0I06bFwW8iB6p7aHZCqJUYAUYa4XaCjXdVwoKAE/J23s17XCZzY10YmBIikRQQIjpvRIbHArzO0om4++2KMnY8m6xoMp2imyceD/0fIVlAqhLTEaBP test@vmango"
	suite.Equal(expectedKey, machine.SSHKeys[0].Public)
	suite.Equal("StubOs-1.0", machine.OS)
	suite.Equal("x86_64", machine.Arch.String())
	suite.Equal("large", machine.Plan)
}

func (suite *MachinerepLibvirtSuite) TestGetScriptedNetworkIPOk() {
	repo := suite.CreateRep(map[string]interface{}{"network_script": suite.LibvirtTest.NetworkScript})
	machine := &domain.VirtualMachine{Id: "c72cb377301a4f2aa34c547f70872b55"}
	exists, err := repo.Get(machine)
	suite.Require().True(exists)
	suite.Require().Nil(err)

	suite.Equal("44.43.42.41", machine.Ip.Address)
}

func (suite *MachinerepLibvirtSuite) TestGetNotFoundFail() {
	repo := suite.CreateRep()
	machine := &domain.VirtualMachine{Id: "deadbeefdeadbeefdeadbeefdeadbeef"}
	exists, err := repo.Get(machine)
	suite.Require().False(exists)
	suite.Require().Nil(err)
}

func (suite *MachinerepLibvirtSuite) TestGetNoNameFail() {
	repo := suite.CreateRep()
	machine := &domain.VirtualMachine{}
	suite.Require().Panics(func() {
		repo.Get(machine)
	})
}

func (suite *MachinerepLibvirtSuite) TestRemoveWithIPOk() {
	repo := suite.CreateRep()
	machine := &domain.VirtualMachine{Id: "fb6c4f622cf346239aee23f0005eb5fb"}
	suite.T().Log("Waiting for domain")
	err := repo.Remove(machine)
	suite.Require().NoError(err)

	domain, err := suite.VirConnect.LookupDomainByUUIDString("fb6c4f622cf346239aee23f0005eb5fb")
	suite.Require().NotNil(err)
	suite.Require().Nil(domain)
	suite.Require().Contains(err.(libvirt.Error).Message, "Domain not found")

	_, err = suite.VirConnect.LookupStorageVolByPath("/var/lib/libvirt/images/one_disk")
	suite.Require().Equal(libvirt.ERR_NO_STORAGE_VOL, err.(libvirt.Error).Code)

	_, err = suite.VirConnect.LookupStorageVolByPath("/var/lib/libvirt/images/one_config.iso")
	suite.Require().Equal(libvirt.ERR_NO_STORAGE_VOL, err.(libvirt.Error).Code)
}

func (suite *MachinerepLibvirtSuite) TestRemoveNotFoundFail() {
	repo := suite.CreateRep()
	machine := &domain.VirtualMachine{Id: "deadbeefdeadbeefdeadbeefdeadbeef"}
	err := repo.Remove(machine)
	suite.Require().NotNil(err)
	suite.T().Log(err.Error())
	suite.Require().Contains(err.Error(), "Domain not found")
}

func (suite *MachinerepLibvirtSuite) TestRemoveNoIdFail() {
	repo := suite.CreateRep()
	machine := &domain.VirtualMachine{}
	suite.Require().Panics(func() {
		repo.Remove(machine)
	})
}

func (suite *MachinerepLibvirtSuite) TestCreateNoImagePoolFail() {
	repo := suite.CreateRep()
	machine := &domain.VirtualMachine{}
	image := &domain.Image{PoolName: "doesntexist"}
	plan := &domain.Plan{}
	err := repo.Create(machine, image, plan)
	suite.Contains(err.Error(), "failed to lookup image storage pool: Storage pool not found: ")
}

func (suite *MachinerepLibvirtSuite) TestCreateNoVMPoolFail() {
	repo := suite.CreateRep(map[string]interface{}{"vm_pool": "doesntexist"})
	machine := &domain.VirtualMachine{}
	image := &domain.Image{PoolName: suite.Fixtures.Pools[1].Name}
	plan := &domain.Plan{}
	err := repo.Create(machine, image, plan)
	suite.Contains(err.Error(), "failed to lookup vm storage pool: Storage pool not found: ")
}

func (suite *MachinerepLibvirtSuite) TestCreateSameNameFail() {
	repo := suite.CreateRep()
	machine := &domain.VirtualMachine{Name: "two"}
	image := &domain.Image{PoolName: suite.Fixtures.Pools[1].Name}
	plan := &domain.Plan{}
	err := repo.Create(machine, image, plan)
	suite.EqualError(err, "domain with name 'two' already exists")
}

func (suite *MachinerepLibvirtSuite) TestCreateOk() {
	repo := suite.CreateRep()
	image := &domain.Image{
		OS:       "Ubuntu-12.04",
		Arch:     domain.ARCH_X86_64,
		Type:     domain.IMAGE_FMT_QCOW2,
		PoolName: suite.Fixtures.Pools[1].Name,
		Id:       "test-image",
	}
	if err := testool.CreateVolume(suite.VirConnect, suite.Fixtures.Pools[1].Name, image.Id); err != nil {
		suite.FailNow("failed to create image volume", err.Error())
	}

	plan := &domain.Plan{
		Name:     "small",
		Memory:   512 * 1024 * 1024,
		Cpus:     2,
		DiskSize: 5 * 1024 * 1024 * 1024,
	}
	machine := &domain.VirtualMachine{
		Name:     "test-create",
		Userdata: "#!/bin/sh",
		Creator:  "someuser",
		SSHKeys: []*domain.SSHKey{
			{Name: "home", Public: "asdf"},
			{Name: "work", Public: "hello"},
		},
	}
	err := repo.Create(machine, image, plan)
	suite.Require().NoError(err)
	virDomain, err := suite.VirConnect.LookupDomainByName("test-create")
	suite.Require().NoError(err)
	suite.AddCleanup(virDomain)
	domainXMLString, err := virDomain.GetXMLDesc(0)
	suite.Require().NoError(err)
	domainConfig := struct {
		Memory   string `xml:"memory"`
		Cpus     string `xml:"vcpu"`
		Id       string `xml:"uuid"`
		Name     string `xml:"name"`
		OS       string `xml:"metadata>md>os"`
		Creator  string `xml:"metadata>md>creator"`
		ImageId  string `xml:"metadata>md>imageId"`
		Userdata string `xml:"metadata>md>userdata"`
		Plan     string `xml:"metadata>md>plan"`
		SSHKeys  []struct {
			Name   string `xml:"name,attr"`
			Public string `xml:",chardata"`
		} `xml:"metadata>md>sshkeys>key"`
		Disks []struct {
			Type   string `xml:"type,attr"`
			Device string `xml:"device,attr"`
			Source struct {
				File string `xml:"file,attr"`
				Dev  string `xml:"dev,attr"`
			} `xml:"source"`
		} `xml:"devices>disk"`
	}{}
	if err := xml.Unmarshal([]byte(domainXMLString), &domainConfig); err != nil {
		suite.Require().NoError(err)
	}
	suite.Equal("524288", domainConfig.Memory)
	suite.Equal("2", domainConfig.Cpus)
	suite.Equal("test-create", domainConfig.Name)
	suite.Equal("someuser", domainConfig.Creator)
	suite.NotEmpty(domainConfig.Id)
	suite.Equal("#!/bin/sh", strings.TrimSpace(domainConfig.Userdata))
	suite.Equal("Ubuntu-12.04", domainConfig.OS)
	suite.Len(domainConfig.SSHKeys, 2)
	suite.Equal(domainConfig.ImageId, "test-image")
	suite.Equal(domainConfig.SSHKeys[0].Name, "home")
	suite.Equal(domainConfig.SSHKeys[0].Public, "asdf")
	suite.Equal(domainConfig.SSHKeys[1].Name, "work")
	suite.Equal(domainConfig.SSHKeys[1].Public, "hello")
	suite.Len(domainConfig.Disks, 2)
	suite.Equal(domainConfig.Disks[0].Device, "disk")

	switch domainConfig.Disks[0].Type {
	case "file":
		suite.True(strings.HasSuffix(domainConfig.Disks[0].Source.File, "_disk"))
	case "block":
		suite.True(strings.HasSuffix(domainConfig.Disks[0].Source.Dev, "_disk"))
	default:
		suite.Require().FailNow("unknown domain disk type:", domainConfig.Disks[0].Type)
	}

	suite.Equal(domainConfig.Disks[1].Device, "cdrom")
	suite.Equal(domainConfig.Disks[1].Type, "file")
	suite.True(strings.HasSuffix(domainConfig.Disks[1].Source.File, "_config.iso"))

	suite.NotEmpty(machine.Id)
	suite.Equal("test-create", machine.Name)
	suite.Equal("someuser", machine.Creator)
	suite.Equal("#!/bin/sh\n", machine.Userdata)
	suite.Equal("Ubuntu-12.04", machine.OS)
	suite.Equal("small", machine.Plan)
	suite.Equal(domain.HWArch(domain.ARCH_X86_64), machine.Arch)
	suite.Equal(domain.STATE_STOPPED, machine.State)
	suite.Equal(536870912, machine.Memory)
	suite.Equal(2, machine.Cpus)
	suite.Equal("test-image", machine.ImageId)
	suite.NotEmpty(machine.Ip)
	suite.NotEmpty(machine.HWAddr)
	suite.NotEmpty(machine.VNCAddr)
	suite.NotEmpty(machine.RootDisk.Driver)
	suite.Equal(uint64(0x140000000), machine.RootDisk.Size)
	suite.NotEmpty(machine.RootDisk.Type)
	suite.Equal(2, len(machine.SSHKeys))
	suite.Equal(machine.SSHKeys[0].Name, "home")
	suite.Equal(machine.SSHKeys[0].Public, "asdf")
	suite.Equal(machine.SSHKeys[1].Name, "work")
	suite.Equal(machine.SSHKeys[1].Public, "hello")
}

func (suite *MachinerepLibvirtSuite) TestCreateScriptedNetworkOk() {
	repo := suite.CreateRep(map[string]interface{}{"network_script": suite.LibvirtTest.NetworkScript})
	image := &domain.Image{
		OS:       "Ubuntu-12.04",
		Arch:     domain.ARCH_X86_64,
		Type:     domain.IMAGE_FMT_QCOW2,
		PoolName: suite.Fixtures.Pools[1].Name,
		Id:       "test-image",
	}
	if err := testool.CreateVolume(suite.VirConnect, suite.Fixtures.Pools[1].Name, image.Id); err != nil {
		suite.FailNow("failed to create image volume", err.Error())
	}
	plan := &domain.Plan{
		Name:     "small",
		Memory:   512 * 1024 * 1024,
		Cpus:     2,
		DiskSize: 5 * 1024 * 1024 * 1024,
	}
	machine := &domain.VirtualMachine{
		Name:     "test-create-scripted-network",
		Userdata: "#!/bin/sh",
		Creator:  "someuser",
		SSHKeys: []*domain.SSHKey{
			{Name: "home", Public: "asdf"},
			{Name: "work", Public: "hello"},
		},
	}
	err := repo.Create(machine, image, plan)
	suite.Require().NoError(err)
	virDomain, err := suite.VirConnect.LookupDomainByName("test-create-scripted-network")
	suite.Require().NoError(err)
	suite.AddCleanup(virDomain)

	suite.Require().Equal("44.43.42.41", machine.Ip.Address)
}

func TestMachinerepLibvirtSuite(t *testing.T) {
	suite.Run(t, new(MachinerepLibvirtSuite))
}
