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

func TestUserNetworkUsesGuestArchitecture(t *testing.T) {
	for _, arch := range []string{"aarch64", "x86_64"} {
		config := newDomainConfig(&compute.VirtualMachine{Id: "test", Name: "test"}, &libvirtxml.DomainCaps{Arch: arch})
		repo := &VirtualMachineRepository{}
		if err := repo.attachInterface(config, &compute.VirtualMachineAttachedInterface{NetworkName: "qemu-usernet", Model: "virtio"}); err != nil {
			t.Fatal(err)
		}
		want := "virtio-net-device,"
		if arch == "x86_64" {
			want = "virtio-net-pci,"
		}
		args := config.QEMUCommandline.Args
		if !strings.HasPrefix(args[len(args)-1].Value, want) {
			t.Fatalf("%s: %v", arch, args)
		}
		config.VCPU = &libvirtxml.DomainVCPU{Value: 2}
		config.Memory = &libvirtxml.DomainMemory{Value: 1024, Unit: "MiB"}
		observed, err := VirtualMachineFromDomainConfig(config, &vir.DomainInfo{State: vir.DOMAIN_SHUTOFF})
		if err != nil || len(observed.Interfaces) != 1 || observed.Interfaces[0].NetworkName != "qemu-usernet" || observed.Interfaces[0].Model != "virtio" {
			t.Fatalf("QEMU network did not retain domain meaning: %+v, %v", observed, err)
		}
	}
}
