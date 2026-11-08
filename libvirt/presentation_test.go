package libvirt

import (
	"encoding/json"
	"reflect"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
	"testing"

	vir "libvirt.org/go/libvirt"
	x "libvirt.org/go/libvirtxml"
)

func presentationDomain(t *testing.T, p hardware.Presentation, arch string) (*x.Domain, hardware.Plan) {
	t.Helper()
	plan, e := hardware.Resolve(hardware.Profile{ID: "presentation", Presentation: &p}, "/qemu")
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(plan)
	vm := &compute.VirtualMachine{Id: "00000000-0000-4000-8000-000000000004", Name: "display", Arch: compute.NewArch(arch), VCpus: 2, Memory: compute.NewSize(1, compute.SizeUnitG)}
	repo := &VirtualMachineRepository{}
	if e = repo.ApplyHardware(vm, raw); e != nil {
		t.Fatal(e)
	}
	c := newDomainConfig(vm, &x.DomainCaps{Arch: arch, Machine: "test", Domain: "qemu", Path: "/qemu"})
	applyDomainPolicy(c, vm, &x.Caps{}, false)
	return c, plan
}
func TestPresentationDefaultsAndIndependentUSB(t *testing.T) {
	for _, arch := range []string{"x86_64", "aarch64"} {
		for _, p := range []hardware.Presentation{{}, {Type: "none"}, {Type: "none", USBController: "qemu-xhci"}, {Keyboard: "none", Pointer: "none"}, {Pointer: "none"}, {VideoModel: "vmvga", Pointer: "usb-mouse"}, {VideoModel: "none"}} {
			c, plan := presentationDomain(t, p, arch)
			if e := validatePresentation(c, plan.Profile.Presentation, true); e != nil {
				t.Fatal(arch, p, e)
			}
			o := observeDisplayDevices(c)
			if len(o.Controllers) != 1 {
				t.Fatal(o)
			}
			for _, in := range c.Devices.Inputs {
				if in.Address == nil || in.Address.USB == nil || in.Alias == nil {
					t.Fatal("unstable input", in)
				}
			}
			raw, e := c.Marshal()
			if e != nil {
				t.Fatal(e)
			}
			round := &x.Domain{}
			if e = round.Unmarshal(raw); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(o, observeDisplayDevices(round)) {
				t.Fatal("observation roundtrip", raw)
			}
			if !plan.Profile.Presentation.NeedsUSB() {
				c.Devices.Controllers = append(c.Devices.Controllers, x.DomainController{Type: "usb", Model: "piix3-uhci"})
				if validatePresentation(c, plan.Profile.Presentation, true) == nil {
					t.Fatal("backend implicit USB accepted")
				}
			}
		}
	}
}
func TestPresentationAdoptionConstraintsAndLegacy(t *testing.T) {
	c, p := presentationDomain(t, hardware.Presentation{}, "x86_64")
	c.Devices.Inputs = append(c.Devices.Inputs, x.DomainInput{Type: "keyboard", Bus: "ps2"})
	// Explicit requirements accept an existing matching device and preserve extras.
	extra := c.Devices.Inputs[0]
	extra.Alias = &x.DomainAlias{Name: "ua-extra-keyboard"}
	c.Devices.Inputs = append(c.Devices.Inputs, extra)
	explicit := *p.Profile.Presentation
	explicit.KeyboardExplicit = true
	if e := validatePresentation(c, &explicit, false); e != nil {
		t.Fatal("extra existing device rejected", e)
	}
	// Omitted input imposes no constraint and must not rebuild the domain.
	before, _ := c.Marshal()
	if e := validatePresentation(c, &hardware.Presentation{}, false); e != nil {
		t.Fatal(e)
	}
	after, _ := c.Marshal()
	if before != after {
		t.Fatal("adoption mutated XML")
	}
	disable := *p.Profile.Presentation
	disable.Keyboard = "none"
	disable.KeyboardExplicit = true
	if validatePresentation(c, &disable, false) == nil {
		t.Fatal("USB disable ignored")
	}
	c.Devices.Inputs = []x.DomainInput{{Type: "keyboard", Bus: "ps2"}, {Type: "mouse", Bus: "ps2"}}
	disable.Pointer = "none"
	disable.PointerExplicit = true
	if e := validatePresentation(c, &disable, false); e != nil {
		t.Fatal("platform PS/2 mistaken for USB", e)
	}
	observed := observeDisplayDevices(c)
	p.Presentation = &observed
	p.Profile.Presentation = &disable
	if e := presentationDrift(c, p); e != nil {
		t.Fatal(e)
	}
	c.Devices.Videos[0].Address = &x.DomainAddress{PCI: &x.DomainAddressPCI{Slot: uintPointer(9)}}
	if presentationDrift(c, p) == nil {
		t.Fatal("address drift accepted")
	}
	legacy, e := hardware.Resolve(hardware.Profile{ID: "old"}, "")
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(legacy)
	loaded, e := hardware.DecodePlan(raw)
	if e != nil || loaded.Profile.Presentation != nil || loaded.Profile.VideoModel != "none" || !reflect.DeepEqual(legacy, loaded) {
		t.Fatal("legacy plan reinterpreted", e)
	}
}
func TestObservedUnknownVideoIsNotNone(t *testing.T) {
	for _, model := range []string{"vmvga", "future-video"} {
		c, p := presentationDomain(t, hardware.Presentation{VideoModel: "vmvga"}, "x86_64")
		_ = p
		c.Devices.Videos[0].Model.Type = model
		vm, e := VirtualMachineFromDomainConfig(c, &vir.DomainInfo{})
		if e != nil {
			t.Fatal(e)
		}
		if model == "future-video" && vm.VideoModel != compute.VideoModelUnknown || model == "vmvga" && vm.VideoModel != compute.VideoModelVMVGA {
			t.Fatal(vm.VideoModel)
		}
	}
}

func TestPresentationRejectsRawX86NetworkBeforeBackendEffects(t *testing.T) {
	for _, network := range []string{"qemu-usernet", "macos-socket-vmnet"} {
		vm := &compute.VirtualMachine{Arch: compute.ArchAmd64, NodeId: "host", Interfaces: []*compute.VirtualMachineAttachedInterface{{NetworkName: network}}}
		_, plan := presentationDomain(t, hardware.Presentation{}, "x86_64")
		// No pool exists: rejection must happen before even acquiring a connection.
		if err := (&VirtualMachineRepository{}).validateHardwareCapabilities(vm, plan, false, true); err == nil {
			t.Fatal("unsafe new video/raw NIC combination accepted")
		}
		if err := validatePresentationNetwork(vm, hardware.Profile{}); err != nil {
			t.Fatal("legacy plan changed", err)
		}
		plan.Profile.Presentation.VideoModel = "none"
		if err := validatePresentationNetwork(vm, plan.Profile); err != nil {
			t.Fatal("video-none rejected", err)
		}
		plan.Profile.Presentation.VideoModel = "virtio"
		vm.Arch = compute.ArchAarch64
		if err := validatePresentationNetwork(vm, plan.Profile); err != nil {
			t.Fatal("ARM MMIO rejected", err)
		}
		vm.Arch = compute.ArchAmd64
		vm.Interfaces[0].NetworkName = "default"
		if err := validatePresentationNetwork(vm, plan.Profile); err != nil {
			t.Fatal("native network rejected", err)
		}
		vm.Interfaces[0].NetworkName = network
		vm.Interfaces[0].Bridge = true
		if err := validatePresentationNetwork(vm, plan.Profile); err != nil {
			t.Fatal("bridge rejected", err)
		}
	}
}
