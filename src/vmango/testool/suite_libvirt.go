package testool

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/libvirt/libvirt-go"
)

const TEST_URI_ENV_KEY = "VMANGO_TEST_LIBVIRT_URI"
const TEST_TYPE_ENV_KEY = "VMANGO_TEST_TYPE"

type LibvirtTestPoolFixture struct {
	Name    string
	Volumes []string
}

type LibvirtTest struct {
	VMTplContent  string
	VMTplPath     string
	VolTplContent string
	VolTplPath    string
	VirConnect    *libvirt.Connect
	VirURI        string

	Fixtures struct {
		Domains  []string
		Networks []string
		Pools    []LibvirtTestPoolFixture
	}

	CleanupAfterTest struct {
		Domains  []*libvirt.Domain
		Networks []*libvirt.Network
		Pools    []*libvirt.StoragePool
	}
}

func (suite *LibvirtTest) SetupSuite() {
	if os.Getenv(TEST_URI_ENV_KEY) == "" {
		log.Panicf("%s must be set", TEST_URI_ENV_KEY)
	}
	if os.Getenv(TEST_TYPE_ENV_KEY) == "" {
		log.Panicf("%s must be set", TEST_TYPE_ENV_KEY)
	}
	suite.VirURI = os.Getenv(TEST_URI_ENV_KEY)
	virConn, err := libvirt.NewConnect(suite.VirURI)
	if err != nil {
		panic(err)
	}
	suite.VirConnect = virConn
}

func (suite *LibvirtTest) SetupTest() {
	suite.VMTplPath = filepath.Join(SourceDir(), "fixtures/libvirt", os.Getenv(TEST_TYPE_ENV_KEY), "vm.xml.in")
	suite.VolTplPath = filepath.Join(SourceDir(), "fixtures/libvirt", os.Getenv(TEST_TYPE_ENV_KEY), "volume.xml.in")
	vmTplContent, err := ioutil.ReadFile(suite.VMTplPath)
	if err != nil {
		panic(err)
	}
	volTplContent, err := ioutil.ReadFile(suite.VolTplPath)
	if err != nil {
		panic(err)
	}
	suite.VMTplContent = string(vmTplContent)
	suite.VolTplContent = string(volTplContent)

	for _, poolFixture := range suite.Fixtures.Pools {
		pool, err := CreatePool(suite.VirConnect, poolFixture.Name)
		if err != nil {
			suite.TearDownTest()
			log.Panicf("failed to load pool fixture %s: %s", poolFixture.Name, err)
		}
		suite.AddCleanup(pool)
		for _, volumeName := range poolFixture.Volumes {
			if err := CreateVolume(suite.VirConnect, poolFixture.Name, volumeName); err != nil {
				suite.TearDownTest()
				log.Panicf("failed to create test volume %s in pool %s: %s", volumeName, poolFixture.Name, err)
			}
		}
	}

	for _, name := range suite.Fixtures.Networks {
		network, err := CreateNetwork(suite.VirConnect, name)
		if err != nil {
			suite.TearDownTest()
			log.Panicf("cannot create test network %s: %s", name, err)
		}
		suite.AddCleanup(network)
	}

	for _, name := range suite.Fixtures.Domains {
		domain, err := CreateDomain(suite.VirConnect, name)
		if err != nil {
			suite.TearDownTest()
			log.Panicf("cannot create test domain %s: %s", name, err)
		}
		suite.AddCleanup(domain)
	}
}

func (suite *LibvirtTest) TearDownTest() {
	for _, domain := range suite.CleanupAfterTest.Domains {
		log.Println("Destroying domain:", domain)
		domain.Destroy()
		domain.Undefine()
	}
	for _, network := range suite.CleanupAfterTest.Networks {
		log.Println("Destroying network:", network)
		network.Destroy()
		network.Undefine()
	}
	for _, pool := range suite.CleanupAfterTest.Pools {
		poolName, _ := pool.GetName()
		log.Println("Destroying pool", poolName)
		vols, err := pool.ListStorageVolumes()
		if err != nil {
			panic(err)
		}
		for _, volName := range vols {
			vol, err := pool.LookupStorageVolByName(volName)
			if err != nil {
				panic(err)
			}
			log.Println("Destroying volume", volName)
			vol.Delete(0)
		}
		pool.Destroy()
		pool.Undefine()
	}
	suite.CleanupAfterTest.Pools = []*libvirt.StoragePool{}
	suite.CleanupAfterTest.Networks = []*libvirt.Network{}
	suite.CleanupAfterTest.Domains = []*libvirt.Domain{}
	suite.VirConnect.Close()
}

func (suite *LibvirtTest) AddCleanup(obj interface{}) {
	switch tObj := obj.(type) {
	case *libvirt.Domain:
		suite.CleanupAfterTest.Domains = append(suite.CleanupAfterTest.Domains, tObj)
	case *libvirt.Network:
		suite.CleanupAfterTest.Networks = append(suite.CleanupAfterTest.Networks, tObj)
	case *libvirt.StoragePool:
		suite.CleanupAfterTest.Pools = append(suite.CleanupAfterTest.Pools, tObj)
	default:
		log.Panicf("unkown object added for cleanup: %s", tObj)
	}
}
