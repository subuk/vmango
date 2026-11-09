package libvirt

import (
	vir "libvirt.org/go/libvirt"
	libvirtxml "libvirt.org/go/libvirtxml"
	"slices"
	"strings"
	"subuk/vmango/compute"
	"testing"
)

func TestImageCapabilitiesAndController(t *testing.T) {
	caps := &libvirtxml.DomainCaps{Arch: "aarch64", Domain: "hvf", Machine: "virt", VCPU: &libvirtxml.DomainCapsVCPU{Max: 8}, OS: &libvirtxml.DomainCapsOS{Enums: []libvirtxml.DomainCapsEnum{{Name: "firmware", Values: []string{"efi"}}}}, Devices: &libvirtxml.DomainCapsDevices{Disk: &libvirtxml.DomainCapsDevice{Supported: "yes", Enums: []libvirtxml.DomainCapsEnum{{Name: "bus", Values: []string{"scsi", "virtio", "usb"}}}}}}
	got := imageCapabilities(caps)
	if got.Arch != compute.ArchAarch64 || got.MaxVCPUs != 8 || !slices.Contains(got.Firmwares, "efi") || slices.Contains(got.Firmwares, "bios") || slices.Contains(got.DiskBuses, compute.DeviceBusUnknown) || !slices.Contains(got.DiskControllers, "virtio-scsi") {
		t.Fatalf("capabilities: %+v", got)
	}
	vm := &compute.VirtualMachine{Id: "test", Name: "test", Arch: compute.ArchAarch64, Firmware: "efi", DiskController: "virtio-scsi"}
	config := newDomainConfig(vm, caps)
	data, err := config.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data, `type="scsi"`) || !strings.Contains(data, `model="virtio-scsi"`) {
		t.Fatalf("controller missing: %s", data)
	}
}

func TestUserNetworkUsesNativeInterface(t *testing.T) {
	for _, arch := range []string{"aarch64", "x86_64"} {
		c := newDomainConfig(&compute.VirtualMachine{}, &libvirtxml.DomainCaps{Arch: arch})
		nic := (compute.NetworkBinding{Type: compute.NetworkUser}).Interface("internet")
		nic.Model = "virtio"
		if err := (&VirtualMachineRepository{}).attachInterface(c, nic); err != nil {
			t.Fatal(err)
		}
		if c.QEMUCommandline != nil || len(c.Devices.Interfaces) != 1 || c.Devices.Interfaces[0].Source.User == nil {
			t.Fatal("not a native user interface")
		}
		observed := VirtualMachineAttachedInterfaceFromInterfaceConfig(c.Devices.Interfaces[0])
		if observed.Binding.Kind() != compute.NetworkUser || observed.Model != "virtio" || observed.LogicalNetwork != "" || observed.Managed {
			t.Fatal("observation inferred ownership", observed)
		}
	}
}

func TestVirtioVideoCapabilitiesAndDomainRoundTrip(t *testing.T) {
	for _, arch := range []string{"aarch64", "x86_64"} {
		t.Run(arch, func(t *testing.T) {
			caps := &libvirtxml.DomainCaps{Arch: arch, Devices: &libvirtxml.DomainCapsDevices{
				Video: &libvirtxml.DomainCapsDevice{Supported: "yes", Enums: []libvirtxml.DomainCapsEnum{{Name: "modelType", Values: []string{"virtio", "none"}}}},
			}}
			if !slices.Contains(imageCapabilities(caps).VideoModels, compute.VideoModelVirtio) {
				t.Fatal("backend virtio video capability was dropped")
			}
			vm := &compute.VirtualMachine{Id: "desktop", Name: "desktop", Arch: compute.NewArch(arch), VCpus: 2, Memory: compute.NewSize(2, compute.SizeUnitG), Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeVnc}, VideoModel: compute.VideoModelVirtio}
			config := newDomainConfig(vm, caps)
			applyDomainPolicy(config, vm, &libvirtxml.Caps{}, false)
			data, err := config.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(data, `<model type="virtio"`) || strings.Contains(data, "accel3d") {
				t.Fatalf("expected virtio 2D video: %s", data)
			}
			decoded := &libvirtxml.Domain{}
			if err := decoded.Unmarshal(data); err != nil {
				t.Fatal(err)
			}
			observed, err := VirtualMachineFromDomainConfig(decoded, &vir.DomainInfo{State: vir.DOMAIN_SHUTOFF})
			if err != nil {
				t.Fatal(err)
			}
			if observed.VideoModel != compute.VideoModelVirtio || observed.Graphic.Type != compute.GraphicTypeVnc {
				t.Fatalf("display settings lost on backend read: %+v", observed)
			}
		})
	}
}
