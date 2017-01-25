package testool

import (
	"github.com/libvirt/libvirt-go"
	"log"
	"os"
	"text/template"
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

type LibvirtTestPoolFixture struct {
	Name    string
	Volumes []string
}

type LibvirtTest struct {
	VMTpl      *template.Template
	VolTpl     *template.Template
	VirConnect *libvirt.Connect

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
	virConn, err := libvirt.NewConnect(os.Getenv(TEST_URI_ENV_KEY))
	if err != nil {
		panic(err)
	}
	suite.VirConnect = virConn
}

func (suite *LibvirtTest) SetupTest() {
	suite.VMTpl = template.Must(template.New("vm.xml.in").Parse(VM_TEMPLATE))
	suite.VolTpl = template.Must(template.New("volume.xml.in").Parse(VOLUME_TEMPLATE))

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

	for _, name := range suite.Fixtures.Domains {
		domain, err := CreateDomain(suite.VirConnect, name)
		if err != nil {
			suite.TearDownTest()
			log.Panicf("cannot create test domain %s: %s", name, err)
		}
		suite.AddCleanup(domain)
	}
	for _, name := range suite.Fixtures.Networks {
		network, err := CreateNetwork(suite.VirConnect, name)
		if err != nil {
			suite.TearDownTest()
			log.Panicf("cannot create test network %s: %s", name, err)
		}
		suite.AddCleanup(network)
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
		log.Panicf("unkown object added for cleanup", tObj)
	}
}
