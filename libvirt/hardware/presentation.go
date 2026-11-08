package hardware

import (
	"fmt"
	"slices"
)

// Presentation is present only in policies resolved with the block interface.
// Its explicitness flags constrain adoption; defaults apply only to new guests.
type Presentation struct {
	Type             string `json:"type"`
	VideoModel       string `json:"video_model"`
	Keyboard         string `json:"keyboard"`
	Pointer          string `json:"pointer"`
	USBController    string `json:"usb_controller,omitempty"`
	TypeExplicit     bool   `json:"type_explicit,omitempty"`
	VideoExplicit    bool   `json:"video_explicit,omitempty"`
	KeyboardExplicit bool   `json:"keyboard_explicit,omitempty"`
	PointerExplicit  bool   `json:"pointer_explicit,omitempty"`
}

func NormalizePresentation(p Presentation) (Presentation, error) {
	if p.Type == "" {
		p.Type = "vnc"
	}
	if !slices.Contains([]string{"none", "vnc", "spice"}, p.Type) {
		return p, fmt.Errorf("graphics.type must be none, vnc or spice")
	}
	if p.VideoModel == "" {
		if p.Type == "none" {
			p.VideoModel = "none"
		} else {
			p.VideoModel = "virtio"
		}
	}
	if p.Keyboard == "" {
		if p.Type == "none" {
			p.Keyboard = "none"
		} else {
			p.Keyboard = "usb"
		}
	}
	if p.Pointer == "" {
		if p.Type == "none" {
			p.Pointer = "none"
		} else {
			p.Pointer = "usb-tablet"
		}
	}
	if !slices.Contains([]string{"none", "virtio", "vmvga", "qxl", "cirrus"}, p.VideoModel) {
		return p, fmt.Errorf("graphics.video_model must be virtio, vmvga, qxl, cirrus or none")
	}
	if !slices.Contains([]string{"usb", "none"}, p.Keyboard) {
		return p, fmt.Errorf("input.keyboard must be usb or none")
	}
	if !slices.Contains([]string{"usb-tablet", "usb-mouse", "none"}, p.Pointer) {
		return p, fmt.Errorf("input.pointer must be usb-tablet, usb-mouse or none")
	}
	if p.Type == "none" && (p.VideoModel != "none" || p.Keyboard != "none" || p.Pointer != "" && p.Pointer != "none") {
		return p, fmt.Errorf("graphics.type=none conflicts with enabled video or input")
	}
	if p.USBController != "" && p.USBController != "qemu-xhci" {
		return p, fmt.Errorf("usb_controller.model must be qemu-xhci")
	}
	return p, nil
}
func (p Presentation) NeedsUSB() bool {
	return p.USBController != "" || p.Keyboard == "usb" || p.Pointer != "" && p.Pointer != "none"
}
func addPresentationDependencies(plan *Plan) {
	p := plan.Profile.Presentation
	if p == nil {
		return
	}
	if p.NeedsUSB() {
		plan.Devices = append(plan.Devices, Device{ID: "usb-0", Kind: Controller, Model: "qemu-xhci"})
	}
	if p.VideoModel != "none" {
		plan.Devices = append(plan.Devices, Device{ID: "video-0", Kind: "video", Model: p.VideoModel})
	}
	if p.Keyboard == "usb" {
		plan.Devices = append(plan.Devices, Device{ID: "input-keyboard", Kind: "input", Model: "usb-keyboard", Dependencies: []string{"usb-0"}})
	}
	if p.Pointer != "" && p.Pointer != "none" {
		plan.Devices = append(plan.Devices, Device{ID: "input-pointer", Kind: "input", Model: p.Pointer, Dependencies: []string{"usb-0"}})
	}
}

type PresentationDevice struct {
	Kind    string `json:"kind"`
	Model   string `json:"model"`
	Bus     string `json:"bus,omitempty"`
	Alias   string `json:"alias,omitempty"`
	Address string `json:"address,omitempty"`
	Index   *uint  `json:"index,omitempty"`
}
type PresentationObservation struct {
	Graphics    []PresentationDevice `json:"graphics"`
	Videos      []PresentationDevice `json:"videos"`
	Inputs      []PresentationDevice `json:"inputs"`
	Controllers []PresentationDevice `json:"controllers"`
}
