package libvirt

import (
	"encoding/json"
	x "libvirt.org/go/libvirtxml"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
	"testing"
)

func storageProfile(t *testing.T, bus, model, cdBus, cdModel string, enabled bool) hardware.Profile {
	t.Helper()
	p, e := hardware.Normalize(hardware.Profile{ID: "storage", Disks: &hardware.Disks{Bus: bus, ControllerModel: model}, ConfigDrive: hardware.ConfigDrive{Enabled: &enabled, Bus: cdBus, ControllerModel: cdModel}})
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func addStorageTestDisk(t *testing.T, c *x.Domain, p hardware.Profile, alias string) *x.DomainAddress {
	t.Helper()
	d := storagePolicy(p, alias)
	a, e := allocateStorageAddress(c, d, true)
	if e != nil {
		t.Fatal(e)
	}
	disk := storageProbeDisk(&compute.VirtualMachineAttachedVolume{Alias: alias, DeviceBus: compute.NewDeviceBus(d.Bus), DeviceType: compute.DeviceTypeDisk}, NewDeviceNamerFromDisks(c.Devices.Disks))
	disk.Address = a
	c.Devices.Disks = append(c.Devices.Disks, *disk)
	return a
}

func TestStorageDependenciesAndSATABoundary(t *testing.T) {
	p := storageProfile(t, "sata", "", "sata", "", true)
	plan, e := hardware.Resolve(p, "")
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, d := range plan.Devices {
		if d.Kind == hardware.Controller && d.Model == "sata" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("SATA dependencies: %+v", plan.Devices)
	}
	c := &x.Domain{}
	ensureStorageControllers(c, p)
	addStorageTestDisk(t, c, p, "root")
	addStorageTestDisk(t, c, p, "configdrive")
	for i := 0; i < 4; i++ {
		addStorageTestDisk(t, c, p, "data")
	}
	before, _ := json.Marshal(c)
	if _, e = allocateStorageAddress(c, p.DiskPolicy(), true); e == nil || !strings.Contains(e.Error(), "capacity 6") {
		t.Fatalf("SATA overflow: %v", e)
	}
	after, _ := json.Marshal(c)
	if string(before) != string(after) {
		t.Fatal("overflow mutated topology")
	}
	freed := c.Devices.Disks[2].Address
	c.Devices.Disks = append(c.Devices.Disks[:2], c.Devices.Disks[3:]...)
	unchanged, _ := json.Marshal(c.Devices.Disks)
	a, e := allocateStorageAddress(c, p.DiskPolicy(), false)
	if e != nil || !reflect.DeepEqual(a, freed) {
		t.Fatalf("did not reuse freed address: %v %+v", e, a)
	}
	preserved, _ := json.Marshal(c.Devices.Disks)
	if string(unchanged) != string(preserved) {
		t.Fatal("allocation moved another disk")
	}
	if len(c.Devices.Controllers) != 1 {
		t.Fatal("controller expanded")
	}
}

func TestSCSIModelAddressLimits(t *testing.T) {
	for _, tt := range []struct {
		model string
		count int
	}{{"lsilogic", 7}, {"lsisas1068", 8}, {"vmpvscsi", 64}} {
		t.Run(tt.model, func(t *testing.T) {
			p := storageProfile(t, "scsi", tt.model, "virtio", "", false)
			c := &x.Domain{}
			ensureStorageControllers(c, p)
			for i := 0; i < tt.count; i++ {
				a := addStorageTestDisk(t, c, p, "data")
				if tt.model == "lsilogic" && valueOrZero(a.Drive.Unit) == 7 {
					t.Fatal("reserved SCSI ID allocated")
				}
			}
			if _, e := allocateStorageAddress(c, p.DiskPolicy(), true); e == nil {
				t.Fatal("controller overflow accepted")
			}
			if len(c.Devices.Controllers) != 1 {
				t.Fatal("extra controller created")
			}
		})
	}
	p := storageProfile(t, "scsi", "virtio-scsi", "virtio", "", false)
	c := &x.Domain{}
	ensureStorageControllers(c, p)
	for i := 0; i < 257; i++ {
		addStorageTestDisk(t, c, p, "data")
	}
	a := c.Devices.Disks[256].Address.Drive
	if valueOrZero(a.Target) != 0 || valueOrZero(a.Unit) != 1 {
		t.Fatalf("virtio-scsi did not move to next LUN: %+v", a)
	}
}

func TestIndependentModelsAndExistingMultipleControllers(t *testing.T) {
	p := storageProfile(t, "scsi", "lsilogic", "scsi", "virtio-scsi", true)
	c := &x.Domain{}
	ensureStorageControllers(c, p)
	if len(c.Devices.Controllers) != 2 {
		t.Fatal(c.Devices.Controllers)
	}
	root := addStorageTestDisk(t, c, p, "root")
	cd := addStorageTestDisk(t, c, p, "configdrive")
	if valueOrZero(root.Drive.Controller) == valueOrZero(cd.Drive.Controller) {
		t.Fatal("different models shared controller")
	}
	plan, _ := hardware.Resolve(p, "")
	if e := validateAdoptionControllers(c, plan, &compute.AttachmentMetadata{ConfigDrive: "sdb"}); e != nil {
		t.Fatal(e)
	}
	c.Devices.Controllers = append(c.Devices.Controllers, x.DomainController{Type: "scsi", Model: "lsilogic", Index: uintPointer(5)})
	for i := 1; i < 7; i++ {
		addStorageTestDisk(t, c, p, "data")
	}
	a, e := allocateStorageAddress(c, p.DiskPolicy(), false)
	if e != nil || valueOrZero(a.Drive.Controller) != 5 {
		t.Fatalf("existing second controller unavailable: %v %+v", e, a)
	}
	if e = validateAdoptionControllers(c, plan, &compute.AttachmentMetadata{ConfigDrive: "sdb"}); e != nil {
		t.Fatal(e)
	}
	c.Devices.Controllers[0].Model = "unsupported-scsi"
	if e = validateAdoptionControllers(c, plan, &compute.AttachmentMetadata{ConfigDrive: "sdb"}); e == nil {
		t.Fatal("unsupported controller silently converted")
	}
	observed := observeStorage(c)
	if observed.Disks[0].ControllerModel != "unsupported-scsi" {
		t.Fatal("observation hid unknown model")
	}
}

func TestVirtioAllocationPreservesExistingPCITopology(t *testing.T) {
	c := &x.Domain{Devices: &x.DomainDeviceList{Controllers: []x.DomainController{{Type: "pci", Model: "pcie-root", Index: uintPointer(0)}, {Type: "pci", Model: "pcie-root-port", Index: uintPointer(1), Address: &x.DomainAddress{PCI: &x.DomainAddressPCI{Bus: uintPointer(0), Slot: uintPointer(2)}}}}}}
	before, _ := json.Marshal(c.Devices.Controllers)
	a, e := allocateVirtioTransport(c)
	if e != nil {
		t.Fatal(e)
	}
	if valueOrZero(a.PCI.Bus) == 0 && valueOrZero(a.PCI.Slot) == 2 {
		t.Fatal("occupied root-port slot reused")
	}
	after, _ := json.Marshal(c.Devices.Controllers)
	if string(before) != string(after) {
		t.Fatal("PCI topology modified")
	}
	c = &x.Domain{Devices: &x.DomainDeviceList{Controllers: []x.DomainController{{Type: "pci", Model: "pcie-root-port", Index: uintPointer(1)}}, Disks: []x.DomainDisk{{Address: &x.DomainAddress{PCI: &x.DomainAddressPCI{Bus: uintPointer(1), Slot: uintPointer(0)}}}}}}
	if _, e = allocateVirtioTransport(c); e == nil {
		t.Fatal("full PCIe port expanded")
	}
}

func TestSATAObservationAndTargetNames(t *testing.T) {
	p := storageProfile(t, "sata", "", "scsi", "virtio-scsi", true)
	c := &x.Domain{}
	ensureStorageControllers(c, p)
	addStorageTestDisk(t, c, p, "root")
	addStorageTestDisk(t, c, p, "configdrive")
	if c.Devices.Disks[0].Target.Dev == c.Devices.Disks[1].Target.Dev {
		t.Fatal("SATA and SCSI target names collide")
	}
	observed := observeStorage(c)
	if observed.Disks[0].Bus != "sata" || observed.Disks[0].Address == nil || observed.Disks[1].ControllerModel != "virtio-scsi" {
		t.Fatal(observed)
	}
	raw, _ := c.Marshal()
	var round x.Domain
	if e := round.Unmarshal(raw); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(observed, observeStorage(&round)) {
		t.Fatal("XML storage observation changed")
	}
	if VirtualMachineAttachedVolumeFromDomainDiskConfig(c.Devices.Disks[0]).DeviceBus != compute.DeviceBusSata {
		t.Fatal("SATA observed as unknown")
	}
}

func TestRunningDiskPreflightNeedsNoBackendSideEffects(t *testing.T) {
	repo := &VirtualMachineRepository{}
	if err := repo.ResolveDiskAttachment(&compute.VirtualMachine{State: compute.StateRunning}, &compute.VirtualMachineAttachedVolume{}); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatal(err)
	}
}

func TestInitialSATAFullSetCountsConfigDrive(t *testing.T) {
	p := storageProfile(t, "sata", "", "sata", "", true)
	vm := &compute.VirtualMachine{AppliedPolicy: testPlan(t, p)}
	repo := &VirtualMachineRepository{}
	for i := 0; i < 6; i++ {
		d := &compute.VirtualMachineAttachedVolume{Alias: "data"}
		if err := repo.ResolveDiskAttachment(vm, d); err != nil {
			t.Fatal(err)
		}
		vm.Volumes = append(vm.Volumes, d)
	}
	cd := &compute.VirtualMachineAttachedVolume{Alias: "configdrive", GeneratedArtifact: true}
	if err := repo.ResolveDiskAttachment(vm, cd); err == nil || !strings.Contains(err.Error(), "capacity 6") {
		t.Fatal("config drive was not counted", err)
	}
}

func TestVirtioAllocationRejectsUntrackedRawPCIDevices(t *testing.T) {
	c := &x.Domain{Devices: &x.DomainDeviceList{Controllers: []x.DomainController{{Type: "pci", Model: "pci-root", Index: uintPointer(0)}}}, QEMUCommandline: &x.DomainQEMUCommandline{Args: []x.DomainQEMUCommandlineArg{{Value: "-device"}, {Value: "virtio-net-pci,netdev=vmngmcos1"}}}}
	before, _ := c.Marshal()
	if _, err := allocateVirtioTransport(c); err == nil || !strings.Contains(err.Error(), "raw QEMU PCI") {
		t.Fatalf("untracked PCI consumer accepted: %v", err)
	}
	after, _ := c.Marshal()
	if before != after {
		t.Fatal("rejection mutated existing topology")
	}
	c.QEMUCommandline.Args[1].Value = "virtio-net-device,netdev=vmngmcos1"
	if _, err := allocateVirtioTransport(c); err != nil {
		t.Fatalf("MMIO NIC consumed a PCI slot: %v", err)
	}
}

func TestVirtioAllocationReservesImplicitI440FXChipset(t *testing.T) {
	c := &x.Domain{Devices: &x.DomainDeviceList{Controllers: []x.DomainController{{Type: "pci", Model: "pci-root", Index: uintPointer(0)}}}}
	address, err := allocateVirtioTransport(c)
	if err != nil || valueOrZero(address.PCI.Slot) < 2 {
		t.Fatalf("allocated chipset slot: %+v %v", address, err)
	}
}
