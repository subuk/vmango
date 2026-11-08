package libvirt

import (
	"encoding/json"
	vir "libvirt.org/go/libvirt"
	xmlmodel "libvirt.org/go/libvirtxml"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
	"testing"
)

func boolPointer(v bool) *bool { return &v }
func fixtureHardwarePolicy(t *testing.T, vm *compute.VirtualMachine) json.RawMessage {
	t.Helper()
	p := hardware.Profile{ID: "fixture", GuestAgent: boolPointer(vm.GuestAgent), Hugepages: vm.Hugepages, Graphics: vm.Graphic.Type.String(), VideoModel: vm.VideoModel.String(), ConfigDrive: hardware.ConfigDrive{Enabled: boolPointer(false)}}
	if len(vm.Volumes) > 0 {
		p.DiskBus = vm.Volumes[0].DeviceBus.String()
	}
	for _, nic := range vm.Interfaces {
		if nic.Model != "" {
			p.NICModel = nic.Model
			break
		}
	}
	plan, err := hardware.Resolve(p, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func testPlan(t *testing.T, p hardware.Profile) json.RawMessage {
	t.Helper()
	plan, err := hardware.Resolve(p, "/accepted/emulator")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestHardwarePlanIndependentConfigDriveAndLaterDisks(t *testing.T) {
	repo := &VirtualMachineRepository{}
	raw := testPlan(t, hardware.Profile{ID: "linux", DiskBus: "scsi", ConfigDrive: hardware.ConfigDrive{Format: "openstack", DeviceType: "cdrom", Bus: "ide"}})
	vm := &compute.VirtualMachine{Name: "test", Config: &compute.VirtualMachineConfig{Hostname: "chosen", Userdata: []byte("data")}}
	if err := repo.ApplyHardware(vm, raw); err != nil {
		t.Fatal(err)
	}
	if vm.Config.Initialization != "openstack" || vm.Config.Hostname != "chosen" || vm.ConfigDriveAttachment.DeviceBus != compute.DeviceBusIde || vm.ConfigDriveAttachment.DeviceType != compute.DeviceTypeCdrom || vm.Graphic.Listen != "127.0.0.1" {
		t.Fatalf("lost applied settings: %+v", vm)
	}
	disk := &compute.VirtualMachineAttachedVolume{Alias: "data"}
	if err := repo.ResolveDiskAttachment(vm, disk); err != nil || disk.DeviceBus != compute.DeviceBusScsi {
		t.Fatalf("later disk ignored stored policy: %+v %v", disk, err)
	}
	disk.DeviceBus = compute.DeviceBusVirtio
	if err := repo.ResolveDiskAttachment(vm, disk); err == nil {
		t.Fatal("conflicting bus override accepted")
	}
	disabled := testPlan(t, hardware.Profile{ID: "headless", Graphics: "none", ConfigDrive: hardware.ConfigDrive{Enabled: boolPointer(false)}})
	if err := repo.ApplyHardware(&compute.VirtualMachine{Config: &compute.VirtualMachineConfig{Userdata: []byte("must not disappear")}}, disabled); err == nil {
		t.Fatal("discarded user-data")
	}
	clean := &compute.VirtualMachine{}
	if err := repo.ApplyHardware(clean, disabled); err != nil || clean.Config != nil || clean.ConfigDriveAttachment != nil || clean.Graphic.Listen != "" {
		t.Fatalf("disabled policy created devices: %+v %v", clean, err)
	}
}
func TestAppliedPolicyMetadataSurvivesTaskHistoryAndUnrelatedMetadata(t *testing.T) {
	raw := testPlan(t, hardware.Profile{ID: "stable"})
	config := &xmlmodel.Domain{Metadata: &xmlmodel.DomainMetadata{XML: `<owner xmlns="urn:owner">keep</owner>`}}
	if err := writeHardwareMetadata(config, raw); err != nil {
		t.Fatal(err)
	}
	if err := writeHardwareMetadata(config, raw); err != nil {
		t.Fatal(err)
	}
	got, err := readHardwareMetadata(config)
	if err != nil || !reflect.DeepEqual(got, raw) || !strings.Contains(config.Metadata.XML, `<owner xmlns="urn:owner">keep</owner>`) {
		t.Fatalf("metadata drift: %s %v", got, err)
	}
	if strings.Count(config.Metadata.XML, "<policy") != 1 {
		t.Fatal("duplicated policy metadata")
	}
	vm := &compute.VirtualMachine{Metadata: &compute.MachineMetadata{Cloud: "c"}}
	if err := (&VirtualMachineRepository{}).ValidateAppliedPolicy(vm); err == nil {
		t.Fatal("missing policy accepted")
	}
	vm.AppliedPolicy = raw
	if err := (&VirtualMachineRepository{}).ValidateAppliedPolicy(vm); err != nil {
		t.Fatal(err)
	}
}
func TestExplicitAdoptionCompatibility(t *testing.T) {
	profile := hardware.Profile{ID: "adopt", GuestAgent: boolPointer(false), Graphics: "vnc", ConfigDrive: hardware.ConfigDrive{Enabled: boolPointer(false)}}
	plan, err := hardware.Resolve(profile, "")
	if err != nil {
		t.Fatal(err)
	}
	vm := &compute.VirtualMachine{Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeVnc, Listen: "0.0.0.0"}, VideoModel: compute.VideoModelNone, Volumes: []*compute.VirtualMachineAttachedVolume{{DeviceType: compute.DeviceTypeDisk, DeviceBus: compute.DeviceBusVirtio}}}
	if err = validateAdoptionPolicy(vm, plan); err != nil {
		t.Fatal(err)
	}
	if vm.Graphic.Listen != "0.0.0.0" {
		t.Fatal("adoption rewrote existing listener")
	}
	vm.Volumes = append(vm.Volumes, &compute.VirtualMachineAttachedVolume{DeviceType: compute.DeviceTypeDisk, DeviceBus: compute.DeviceBusIde})
	if err = validateAdoptionPolicy(vm, plan); err == nil {
		t.Fatal("mixed ordinary buses accepted")
	}
	vm.Volumes = vm.Volumes[:1]
	plan.Profile.ConfigDrive.Enabled = boolPointer(true)
	if err = validateAdoptionPolicy(vm, plan); err == nil {
		t.Fatal("enabled profile accepted absent configdrive")
	}
	vm.Volumes = append(vm.Volumes, &compute.VirtualMachineAttachedVolume{Alias: "configdrive", DeviceType: compute.DeviceTypeDisk, DeviceBus: compute.DeviceBusVirtio})
	if err = validateAdoptionPolicy(vm, plan); err == nil {
		t.Fatal("unreadable configdrive guessed")
	}
	vm.Config = &compute.VirtualMachineConfig{Initialization: "openstack"}
	if err = validateAdoptionPolicy(vm, plan); err == nil {
		t.Fatal("different configdrive format accepted")
	}
	vm.Config.Initialization = "nocloud"
	if err = validateAdoptionPolicy(vm, plan); err != nil {
		t.Fatal(err)
	}
}
func TestDisabledVideoCannotBeReintroducedByLibvirtDefaults(t *testing.T) {
	config := &xmlmodel.Domain{Devices: &xmlmodel.DomainDeviceList{}}
	vm := &compute.VirtualMachine{Memory: compute.NewSize(1, compute.SizeUnitG), VCpus: 1, Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeVnc, Listen: "127.0.0.1"}, VideoModel: compute.VideoModelNone}
	applyDomainPolicy(config, vm, &xmlmodel.Caps{}, false)
	if len(config.Devices.Videos) != 1 || config.Devices.Videos[0].Model.Type != "none" {
		t.Fatal("omission lets libvirt add its default video device")
	}
}

func TestResourceChangePreservesLegacyTopologyForRAMOnly(t *testing.T) {
	config := &xmlmodel.Domain{VCPU: &xmlmodel.DomainVCPU{Value: 2}, CPU: &xmlmodel.DomainCPU{Mode: "host-passthrough", Topology: &xmlmodel.DomainCPUTopology{Sockets: 1, Cores: 1, Threads: 2}}, Devices: &xmlmodel.DomainDeviceList{}}
	if err := validateResourceTopology(config, 4); err == nil {
		t.Fatal("legacy topology resized")
	}
	if err := validateResourceTopology(config, 2); err != nil {
		t.Fatal(err)
	}
	applyDomainPolicy(config, &compute.VirtualMachine{VCpus: 2, Memory: compute.NewSize(2, compute.SizeUnitG)}, &xmlmodel.Caps{}, true)
	if config.CPU.Topology.Cores != 1 || config.CPU.Topology.Threads != 2 || config.CPU.Mode != "host-passthrough" {
		t.Fatal("RAM-only update changed CPU")
	}
}

func TestCapturedHostTopologyOverridesLaterHostDefaults(t *testing.T) {
	plan, err := hardware.Resolve(hardware.Profile{ID: "stable"}, "/accepted/qemu")
	if err != nil {
		t.Fatal(err)
	}
	plan.CPUThreads = 2
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	vm := &compute.VirtualMachine{VCpus: 4, Memory: compute.NewSize(2, compute.SizeUnitG), AppliedPolicy: raw}
	config := &xmlmodel.Domain{}
	applyDomainResources(config, vm, &xmlmodel.Caps{Host: xmlmodel.CapsHost{CPU: &xmlmodel.CapsHostCPU{Topology: &xmlmodel.CapsHostCPUTopology{Threads: 4}}}})
	if config.CPU.Topology.Threads != 2 || config.CPU.Topology.Cores != 2 {
		t.Fatal("accepted topology re-resolved from current host")
	}
}
func TestGeneratedConfigDriveMarkerCannotBeForgedByAlias(t *testing.T) {
	repo := &VirtualMachineRepository{}
	vm := &compute.VirtualMachine{AppliedPolicy: testPlan(t, hardware.Profile{ID: "p", DiskBus: "scsi"})}
	forged := &compute.VirtualMachineAttachedVolume{Alias: "configdrive", DeviceBus: compute.DeviceBusVirtio}
	if err := repo.ResolveDiskAttachment(vm, forged); err == nil {
		t.Fatal("untrusted alias bypassed ordinary disk policy")
	}
	if err := repo.ApplyHardware(vm, vm.AppliedPolicy); err != nil {
		t.Fatal(err)
	}
	if err := repo.ResolveDiskAttachment(vm, vm.ConfigDriveAttachment); err != nil {
		t.Fatal(err)
	}
}

func TestCPUSetAffinitySurvivesMemoryOnlyChange(t *testing.T) {
	config := &xmlmodel.Domain{VCPU: &xmlmodel.DomainVCPU{Value: 2, Current: 2, Placement: "static", CPUSet: "1-3,^2"}, Memory: &xmlmodel.DomainMemory{Value: 1024, Unit: "MiB"}, OS: &xmlmodel.DomainOS{Type: &xmlmodel.DomainOSType{Arch: "x86_64"}}, Devices: &xmlmodel.DomainDeviceList{}}
	vm, err := VirtualMachineFromDomainConfig(config, &vir.DomainInfo{State: vir.DOMAIN_SHUTOFF})
	if err != nil {
		t.Fatal(err)
	}
	if vm.Cpupin == nil || !reflect.DeepEqual(vm.Cpupin.Vcpus[0], []uint{1, 3}) || !reflect.DeepEqual(vm.Cpupin.Vcpus[1], []uint{1, 3}) {
		t.Fatalf("global affinity not observed: %+v", vm.Cpupin)
	}
	if err = validateResourceTopology(config, 2); err != nil {
		t.Fatal(err)
	}
	if err = validateResourceTopology(config, 4); err == nil {
		t.Fatal("CPUSet pinning permitted incompatible CPU-count change")
	}
	vm.Memory = compute.NewSize(2, compute.SizeUnitG)
	before := *config.VCPU
	applyDomainPolicy(config, vm, &xmlmodel.Caps{}, true)
	if !reflect.DeepEqual(*config.VCPU, before) {
		t.Fatalf("memory change discarded CPU affinity: %+v", config.VCPU)
	}
}
func TestLaterInterfaceUsesAppliedPolicyWithoutExistingInterfaces(t *testing.T) {
	repo := &VirtualMachineRepository{}
	vm := &compute.VirtualMachine{AppliedPolicy: testPlan(t, hardware.Profile{ID: "stable", NICModel: "virtio"})}
	nic := &compute.VirtualMachineAttachedInterface{}
	if err := repo.ResolveInterfaceAttachment(vm, nic); err != nil || nic.Model != "virtio" {
		t.Fatalf("missing persisted model: %+v %v", nic, err)
	}
	nic.Model = "e1000"
	if err := repo.ResolveInterfaceAttachment(vm, nic); err == nil {
		t.Fatal("host binding overrode applied NIC policy")
	}
}
func TestSyntheticNetworksRejectConflictingNICPolicy(t *testing.T) {
	repo := &VirtualMachineRepository{}
	for _, network := range []string{"qemu-usernet", "macos-socket-vmnet"} {
		nic := &compute.VirtualMachineAttachedInterface{NetworkName: network, Model: "e1000"}
		if err := repo.ValidateNetwork("host", nic); err == nil {
			t.Fatalf("%s silently substituted virtio", network)
		}
		nic.Model = "virtio"
		if err := repo.ValidateNetwork("host", nic); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPlatformBindingSurvivesMetadataAndUnrelatedResourceChange(t *testing.T) {
	plan, err := hardware.Resolve(hardware.Profile{ID: "stable", Machine: &hardware.Machine{Type: "q35", TypeExplicit: true, Acceleration: "hardware"}}, "/saved/qemu")
	if err != nil {
		t.Fatal(err)
	}
	plan.Machine, plan.DomainType = "pc-q35-8.2", "kvm"
	raw, _ := json.Marshal(plan)
	vm := &compute.VirtualMachine{Id: "vm", Arch: compute.ArchAmd64, VCpus: 2, Memory: compute.NewSize(2, compute.SizeUnitG), AppliedPolicy: raw}
	config := newDomainConfig(vm, &xmlmodel.DomainCaps{Arch: "x86_64", Machine: plan.Machine, Domain: plan.DomainType, Path: plan.Emulator})
	if err := writeHardwareMetadata(config, raw); err != nil {
		t.Fatal(err)
	}
	original := domainPlatform(config)
	applyDomainPolicy(config, vm, &xmlmodel.Caps{}, true)
	stored, err := readHardwareMetadata(config)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := hardware.DecodePlan(stored)
	if err != nil || restored.Platform() != original || domainPlatform(config) != original || restored.Profile.Machine.Type != "q35" {
		t.Fatal("platform or requested alias lost", err)
	}
	for _, observed := range []hardware.PlatformBinding{{Machine: "pc-q35-9.2", DomainType: "kvm", Emulator: plan.Emulator}, {Machine: plan.Machine, DomainType: "qemu", Emulator: plan.Emulator}, {Machine: plan.Machine, DomainType: "kvm", Emulator: "/replacement"}} {
		if err := validateMachineCapabilities(restored.Platform(), "x86_64", &xmlmodel.DomainCaps{Arch: "x86_64", Machine: observed.Machine, Domain: observed.DomainType, Path: observed.Emulator}); err == nil {
			t.Fatal("live validation changed accepted platform")
		}
	}
}

func TestOrdinaryMutationRejectsPlatformDriftWithoutConversion(t *testing.T) {
	plan, _ := hardware.Resolve(hardware.Profile{ID: "stable"}, "/saved/qemu")
	plan.Machine, plan.DomainType = "pc-q35-8.2", "kvm"
	raw, _ := json.Marshal(plan)
	observed, _ := json.Marshal(hardware.PlatformBinding{Machine: "pc-q35-9.2", DomainType: "kvm", Emulator: "/saved/qemu"})
	vm := &compute.VirtualMachine{Metadata: &compute.MachineMetadata{Cloud: "c"}, AppliedPolicy: raw, ObservedBackend: observed}
	if err := (&VirtualMachineRepository{}).ValidateAppliedPolicy(vm); err == nil {
		t.Fatal("external platform drift was silently accepted")
	}
	if string(vm.AppliedPolicy) != string(raw) || string(vm.ObservedBackend) != string(observed) {
		t.Fatal("validation rewrote platform")
	}
}
