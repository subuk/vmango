package libvirt

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"

	vir "libvirt.org/go/libvirt"
	x "libvirt.org/go/libvirtxml"
)

// Called only for new policies. Persisted legacy plans retain their former
// architecture-specific input behavior in newDomainConfig.
func applyPresentation(c *x.Domain, p *hardware.Presentation) {
	if p == nil {
		return
	}
	if c.Devices == nil {
		c.Devices = &x.DomainDeviceList{}
	}
	model := "none"
	if p.NeedsUSB() {
		model = "qemu-xhci"
	}
	ctl := x.DomainController{Type: "usb", Model: model, Index: uintPointer(0)}
	if p.NeedsUSB() {
		ctl.USB = &x.DomainControllerUSB{Port: uintPointer(4)}
		ctl.Alias = &x.DomainAlias{Name: "ua-vmango-usb-0"}
	}
	c.Devices.Controllers = append(c.Devices.Controllers, ctl)
	add := func(kind, alias, port string) {
		c.Devices.Inputs = append(c.Devices.Inputs, x.DomainInput{Type: kind, Bus: "usb", Alias: &x.DomainAlias{Name: alias}, Address: &x.DomainAddress{USB: &x.DomainAddressUSB{Bus: uintPointer(0), Port: port}}})
	}
	if p.Keyboard == "usb" {
		add("keyboard", "ua-vmango-keyboard", "1")
	}
	if p.Pointer != "none" {
		add(strings.TrimPrefix(p.Pointer, "usb-"), "ua-vmango-pointer", "2")
	}
}
func observeDisplayDevices(c *x.Domain) hardware.PresentationObservation {
	o := hardware.PresentationObservation{Graphics: []hardware.PresentationDevice{}, Videos: []hardware.PresentationDevice{}, Inputs: []hardware.PresentationDevice{}, Controllers: []hardware.PresentationDevice{}}
	if c.Devices == nil {
		return o
	}
	address := func(a *x.DomainAddress) string {
		if a == nil {
			return ""
		}
		raw, _ := xml.Marshal(a)
		return string(raw)
	}
	alias := func(a *x.DomainAlias) string {
		if a == nil {
			return ""
		}
		return a.Name
	}
	for _, g := range c.Devices.Graphics {
		d := hardware.PresentationDevice{Kind: "graphics", Model: "unknown"}
		switch {
		case g.VNC != nil:
			d.Model = "vnc"
			d.Address = graphicsListenerAddress(g.VNC.Listen, g.VNC.Listeners)
		case g.Spice != nil:
			d.Model = "spice"
			d.Address = graphicsListenerAddress(g.Spice.Listen, g.Spice.Listeners)
		}
		o.Graphics = append(o.Graphics, d)
	}
	for _, v := range c.Devices.Videos {
		o.Videos = append(o.Videos, hardware.PresentationDevice{Kind: "video", Model: v.Model.Type, Alias: alias(v.Alias), Address: address(v.Address)})
	}
	for _, i := range c.Devices.Inputs {
		o.Inputs = append(o.Inputs, hardware.PresentationDevice{Kind: "input", Model: i.Type, Bus: i.Bus, Alias: alias(i.Alias), Address: address(i.Address)})
	}
	for _, ctl := range c.Devices.Controllers {
		if ctl.Type == "usb" {
			o.Controllers = append(o.Controllers, hardware.PresentationDevice{Kind: "controller", Model: ctl.Model, Index: ctl.Index, Alias: alias(ctl.Alias), Address: address(ctl.Address)})
		}
	}
	return o
}
func validatePresentation(c *x.Domain, p *hardware.Presentation, creation bool) error {
	if p == nil {
		return nil
	}
	fail := func(s string) error {
		return &compute.ValidationError{Field: "hardware_profile", Message: "Graphics/input policy conflicts with " + s + "; choose a compatible profile without replacing existing devices."}
	}
	o := observeDisplayDevices(c)
	if creation || p.TypeExplicit {
		if p.Type == "none" {
			if len(o.Graphics) > 0 {
				return fail("existing graphics listeners")
			}
		} else {
			matches := 0
			for _, g := range o.Graphics {
				if g.Model == p.Type {
					matches++
				}
			}
			if matches == 0 || creation && len(o.Graphics) != 1 {
				return fail("graphics type")
			}
		}
	}
	if creation || p.VideoExplicit || p.TypeExplicit && p.Type == "none" {
		models := []string{}
		for _, v := range o.Videos {
			if v.Model != "none" {
				models = append(models, v.Model)
			}
		}
		if p.VideoModel == "none" {
			if len(models) > 0 {
				return fail("video disable")
			}
		} else {
			matches := 0
			for _, m := range models {
				if m == p.VideoModel {
					matches++
				}
			}
			if matches == 0 || creation && len(models) != 1 {
				return fail("video model")
			}
		}
	}
	hasXHCI := false
	activeUSB := 0
	for _, ctl := range o.Controllers {
		if ctl.Model != "none" {
			activeUSB++
		}
		if ctl.Model == "qemu-xhci" {
			hasXHCI = true
		}
	}
	if creation {
		if p.NeedsUSB() && (activeUSB != 1 || !hasXHCI) || !p.NeedsUSB() && activeUSB != 0 {
			return fail("unexpected backend USB-controller defaults")
		}
	}
	if p.USBController != "" && !hasXHCI {
		return fail("explicit qemu-xhci controller")
	}
	for _, constraint := range []struct {
		check      bool
		kind, want string
	}{{creation || p.KeyboardExplicit, "keyboard", p.Keyboard}, {creation || p.PointerExplicit, "pointer", p.Pointer}} {
		if !constraint.check {
			continue
		}
		count := 0
		matching := false
		for _, in := range o.Inputs {
			if in.Bus != "usb" {
				continue
			}
			if constraint.kind == "keyboard" && in.Model != "keyboard" || constraint.kind == "pointer" && in.Model != "mouse" && in.Model != "tablet" {
				continue
			}
			count++
			matching = matching || constraint.kind == "keyboard" && constraint.want == "usb" || "usb-"+in.Model == constraint.want
		}
		if constraint.want == "none" {
			if count > 0 {
				return fail("explicit USB " + constraint.kind + " disable")
			}
		} else if count == 0 || creation && count != 1 || !matching || !hasXHCI {
			return fail("USB " + constraint.kind + "/XHCI dependency")
		}
	}
	return nil
}
func validatePresentationCapabilities(conn *vir.Connect, p hardware.Profile, caps *x.DomainCaps) error {
	if p.Presentation == nil {
		return nil
	}
	c := firmwareProbe(caps)
	applyDomainPowerPolicy(c)
	applyPresentation(c, p.Presentation)
	vm := &compute.VirtualMachine{VCpus: 1, Memory: compute.NewSize(512, compute.SizeUnitM), Graphic: compute.VirtualMachineGraphic{Type: compute.NewGraphicType(p.Graphics), Listen: "127.0.0.1"}, VideoModel: compute.NewVideoModel(p.VideoModel)}
	applyDomainPolicy(c, vm, &x.Caps{}, false)
	raw, err := c.Marshal()
	if err != nil {
		return err
	}
	if _, err = conn.DomainXMLToNative("qemu-argv", raw, 0); err != nil {
		return &compute.ValidationError{Field: "hardware_profile", Message: fmt.Sprintf("Graphics/video/input requirements are unavailable for %s/%s on %s: %v", caps.Arch, caps.Machine, caps.Path, err)}
	}
	return nil
}
func (repo *VirtualMachineRepository) recordPresentation(domain *vir.Domain, vm *compute.VirtualMachine) error {
	plan, err := hardware.DecodePlan(vm.AppliedPolicy)
	if err != nil {
		return err
	}
	if plan.Profile.Presentation == nil {
		return nil
	}
	raw, err := domain.GetXMLDesc(vir.DOMAIN_XML_INACTIVE | vir.DOMAIN_XML_SECURE)
	if err != nil {
		return err
	}
	c := &x.Domain{}
	if err = c.Unmarshal(raw); err != nil {
		return err
	}
	if err = validatePresentation(c, plan.Profile.Presentation, true); err != nil {
		return err
	}
	observed := observeDisplayDevices(c)
	plan.Presentation = &observed
	vm.AppliedPolicy, err = json.Marshal(plan)
	if err != nil {
		return err
	}
	if err = writeHardwareMetadata(c, vm.AppliedPolicy); err != nil {
		return err
	}
	metadata, err := xml.Marshal(hardwareXML{Snapshot: string(vm.AppliedPolicy)})
	if err != nil {
		return err
	}
	return domain.SetMetadata(vir.DOMAIN_METADATA_ELEMENT, string(metadata), "hardware", hardwareNamespace, vir.DOMAIN_AFFECT_CONFIG)
}
func presentationDrift(c *x.Domain, plan hardware.Plan) error {
	if err := validatePresentation(c, plan.Profile.Presentation, false); err != nil {
		return err
	}
	if plan.Presentation != nil && !reflect.DeepEqual(*plan.Presentation, observeDisplayDevices(c)) {
		return fmt.Errorf("applied graphics/input devices or addresses changed; inspect and explicitly re-adopt the VM")
	}
	return nil
}

// Raw x86 QEMU NICs use automatic PCI slots outside libvirt's allocation.
// Reserving only video/USB would still leave implicit PCI root ports untracked.
func validatePresentationNetwork(vm *compute.VirtualMachine, p hardware.Profile) error {
	if vm.Arch != compute.ArchAmd64 || p.Presentation == nil || p.Presentation.VideoModel == "none" {
		return nil
	}
	for _, nic := range vm.Interfaces {
		if !nic.Bridge && (nic.NetworkName == "qemu-usernet" || nic.NetworkName == "macos-socket-vmnet") {
			return &compute.ValidationError{Field: "network", Message: fmt.Sprintf("Host %s: x86 video with raw QEMU network %s has unsafe PCI address allocation. Select a native libvirt network/bridge, or explicitly disable video in the hardware profile. Existing VM devices will not be moved.", vm.NodeId, nic.NetworkName)}
		}
	}
	return nil
}
