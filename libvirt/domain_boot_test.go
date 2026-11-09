package libvirt

import (
	"encoding/xml"
	"reflect"
	"subuk/vmango/compute"
	"testing"

	libvirtxml "libvirt.org/go/libvirtxml"
)

func bootTestDomain(t *testing.T, body string) *libvirtxml.Domain {
	t.Helper()
	config := &libvirtxml.Domain{}
	if err := config.Unmarshal("<domain type='kvm'>" + body + "</domain>"); err != nil {
		t.Fatal(err)
	}
	return config
}

func assertDiskBootOrders(t *testing.T, config *libvirtxml.Domain, want map[string]uint) {
	t.Helper()
	data, err := config.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	var actual struct {
		OS struct {
			Boots []struct{} `xml:"boot"`
		} `xml:"os"`
		Devices struct {
			Disks []struct {
				Target struct {
					Dev string `xml:"dev,attr"`
				} `xml:"target"`
				Boot *struct {
					Order uint `xml:"order,attr"`
				} `xml:"boot"`
			} `xml:"disk"`
		} `xml:"devices"`
	}
	if err := xml.Unmarshal([]byte(data), &actual); err != nil {
		t.Fatal(err)
	}
	got := map[string]uint{}
	for _, disk := range actual.Devices.Disks {
		if disk.Boot != nil {
			got[disk.Target.Dev] = disk.Boot.Order
		}
	}
	if len(actual.OS.Boots) != 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("boot orders = %v, want %v, legacy boot entries = %d\n%s", got, want, len(actual.OS.Boots), data)
	}
}

func TestBootDiskReattachAfterConfigDrive(t *testing.T) {
	for _, optical := range []bool{false, true} {
		t.Run(map[bool]string{false: "disk only", true: "with installation CD"}[optical], func(t *testing.T) {
			config := newDomainConfig(&compute.VirtualMachine{}, &libvirtxml.DomainCaps{Arch: "x86_64"})
			root := &compute.VirtualMachineAttachedVolume{Path: "/root.qcow2", Alias: "root", DeviceType: compute.DeviceTypeDisk, DeviceBus: compute.DeviceBusVirtio}
			configDrive := &compute.VirtualMachineAttachedVolume{Path: "/config.iso", Alias: "configdrive", DeviceType: compute.DeviceTypeDisk, DeviceBus: compute.DeviceBusVirtio}
			namer := NewDeviceNamer()
			config.Devices.Disks = append(config.Devices.Disks, *DomainDiskConfigFromVirtualMachineAttachedVolume(root, "qcow2", "file", namer))
			if optical {
				config.Devices.Disks = append(config.Devices.Disks, libvirtxml.DomainDisk{Device: "cdrom", Target: &libvirtxml.DomainDiskTarget{Dev: "sda", Bus: "sata"}})
			}
			ensureDomainBootDisk(config)
			want := map[string]uint{"vda": 1}
			if optical {
				want = map[string]uint{"sda": 1, "vda": 2}
			}
			assertDiskBootOrders(t, config, want)
			config.Devices.Disks = append(config.Devices.Disks, *DomainDiskConfigFromVirtualMachineAttachedVolume(configDrive, "raw", "file", namer))
			ensureDomainBootDisk(config)
			assertDiskBootOrders(t, config, want)
			// Detach the root, retaining the config drive and its vdb address.
			config.Devices.Disks = config.Devices.Disks[1:]
			for cycle := 0; cycle < 2; cycle++ {
				namer = NewDeviceNamerFromDisks(config.Devices.Disks)
				config.Devices.Disks = append(config.Devices.Disks, *DomainDiskConfigFromVirtualMachineAttachedVolume(root, "qcow2", "file", namer))
				ensureDomainBootDisk(config)
				delete(want, "vda")
				want["vdc"] = 1
				if optical {
					want["vdc"] = 2
				}
				assertDiskBootOrders(t, config, want)
				// Exercise persisted XML, not only the in-memory representation.
				data, err := config.Marshal()
				if err != nil {
					t.Fatal(err)
				}
				config = &libvirtxml.Domain{}
				if err := config.Unmarshal(data); err != nil {
					t.Fatal(err)
				}
				config.Devices.Disks = config.Devices.Disks[:len(config.Devices.Disks)-1]
			}
		})
	}
}

func TestLegacyBootDiskMigration(t *testing.T) {
	config := bootTestDomain(t, `<os><type>hvm</type><boot dev='cdrom'/><boot dev='hd'/><bootmenu enable='yes'/></os><devices>
<disk device='disk'><source file='/config.iso'/><target dev='vdb' bus='virtio'/><alias name='ua-configdrive'/></disk>
<disk device='disk'><source file='/data.qcow2'/><target dev='vdc' bus='virtio'/><alias name='ua-data'/></disk>
<disk device='disk'><source file='/root.qcow2'/><target dev='vdd' bus='virtio'/><alias name='ua-root'/><address type='pci' slot='0x07'/></disk>
</devices>`)
	before := config.Devices.Disks[2]
	ensureDomainBootDisk(config)
	assertDiskBootOrders(t, config, map[string]uint{"vdd": 1})
	if !reflect.DeepEqual(before.Source, config.Devices.Disks[2].Source) || !reflect.DeepEqual(before.Target, config.Devices.Disks[2].Target) || !reflect.DeepEqual(before.Address, config.Devices.Disks[2].Address) || config.OS.BootMenu.Enable != "yes" {
		t.Fatal("boot migration changed disk identity or boot menu")
	}
	ensureDomainBootDisk(config)
	assertDiskBootOrders(t, config, map[string]uint{"vdd": 1})
}

func TestBootDiskPreservesCustomChoices(t *testing.T) {
	for _, body := range []string{
		`<os><boot dev='network'/><boot dev='hd'/></os>`,
		`<os><boot dev='cdrom'/></os>`,
		`<os><kernel>/vmlinuz</kernel></os>`,
		`<bootloader>/pygrub</bootloader><os/>`,
	} {
		config := bootTestDomain(t, body+`<devices><disk device='disk'><target dev='vdc' bus='virtio'/><alias name='ua-root'/></disk></devices>`)
		before, _ := config.Marshal()
		ensureDomainBootDisk(config)
		after, _ := config.Marshal()
		if before != after {
			t.Fatalf("custom boot changed:\n%s", after)
		}
	}
	config := bootTestDomain(t, `<os/><devices>
<disk device='disk'><target dev='vda' bus='virtio'/><alias name='ua-data'/><boot order='3'/></disk>
<disk device='disk'><target dev='vdc' bus='virtio'/><alias name='ua-root'/></disk>
<interface type='network'><source network='default'/><boot order='1'/></interface></devices>`)
	before, _ := config.Marshal()
	ensureDomainBootDisk(config)
	after, _ := config.Marshal()
	if before != after {
		t.Fatal("explicit boot configuration changed")
	}
}

func TestReattachedBootDiskFollowsExistingDevices(t *testing.T) {
	config := bootTestDomain(t, `<os/><devices>
<disk device='disk'><target dev='vdb' bus='virtio'/><alias name='ua-configdrive'/></disk>
<disk device='disk'><target dev='vdc' bus='virtio'/></disk>
<interface type='network'><source network='default'/><boot order='2'/></interface>
<hostdev mode='subsystem' type='pci'><source><address domain='0x0000' bus='0x01' slot='0x00' function='0x0'/></source><boot order='4'/></hostdev>
<redirdev bus='usb' type='spicevmc'><boot order='6'/></redirdev>
<filesystem type='mount'><source dir='/shared'/><target dir='shared'/><boot order='8'/></filesystem></devices>`)
	if err := writeSchedulerMetadata(config, &compute.MachineMetadata{Cloud: "c", Zone: "z", CPURequest: 1, MemoryRequestBytes: 1024, Attachments: &compute.AttachmentMetadata{Version: compute.AttachmentVersion, ConfigDrive: "vdb"}}); err != nil {
		t.Fatal(err)
	}
	ensureDomainBootDisk(config)
	assertDiskBootOrders(t, config, map[string]uint{"vdc": 9})
}
