package config

import (
	"os"
	"strings"
	"testing"
)

func TestPresentationBlocks(t *testing.T) {
	for _, tc := range []struct {
		body, graphics, video, keyboard, pointer string
		usb                                      bool
	}{
		{"", "vnc", "virtio", "usb", "usb-tablet", true},
		{`graphics {type="none"}`, "none", "none", "none", "none", false},
		{`graphics {type="none"} input {keyboard="none" pointer="none"} usb_controller {model="qemu-xhci"}`, "none", "none", "none", "none", true},
		{`graphics {video_model="vmvga"} input {pointer="usb-mouse"}`, "vnc", "vmvga", "usb", "usb-mouse", true},
		{`input {keyboard="none" pointer="none"}`, "vnc", "virtio", "none", "none", false},
		{`input {pointer="none"}`, "vnc", "virtio", "usb", "none", true},
	} {
		c, e := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+tc.body+`}`, 1)))
		if e != nil {
			t.Fatal(tc.body, e)
		}
		profiles, e := c.HardwareProfiles()
		if e != nil {
			t.Fatal(e)
		}
		p := profiles["test"]["default"].Presentation
		if p == nil || p.Type != tc.graphics || p.VideoModel != tc.video || p.Keyboard != tc.keyboard || p.Pointer != tc.pointer || p.NeedsUSB() != tc.usb {
			t.Fatalf("%s: %+v", tc.body, p)
		}
		if p.KeyboardExplicit != strings.Contains(tc.body, "keyboard=") || p.PointerExplicit != strings.Contains(tc.body, "pointer=") {
			t.Fatal("omission explicitness lost", p)
		}
	}
	for _, body := range []string{`graphics="vnc"`, `video_model="none"`, `graphics {} graphics {}`, `input {} input {}`, `usb_controller {model="qemu-xhci"} usb_controller {model="qemu-xhci"}`, `graphics {bad="vnc"}`, `input {bus="usb"}`, `usb_controller {ports=4}`, `usb_controller {}`, `input {keyboard="ps2"}`, `input {pointer="tablet"}`, `graphics {type="none" video_model="virtio"}`, `graphics {type="none"} input {keyboard="usb"}`, `graphics {type="vnc" type="spice"}`, `input {pointer=""}`, `usb_controller {model="ehci"}`, `graphics "named" {}`, `graphics {video_model="vga"}`} {
		_, e := ParseBytes([]byte(strings.Replace(cloudConfig, `hardware_profile "default" {}`, `hardware_profile "default" {`+body+`}`, 1)))
		if e == nil || !strings.Contains(e.Error(), "hardware_profile default") {
			t.Fatalf("%s: %v", body, e)
		}
		if (body == `graphics="vnc"` || body == `video_model="none"`) && (!strings.Contains(e.Error(), "line ") || !strings.Contains(e.Error(), HardwareMigrationGuide)) {
			t.Fatal("migration location/guide missing", e)
		}
	}
}
func TestDocumentedGraphicsExamples(t *testing.T) {
	raw, e := os.ReadFile("../docs/examples/graphics.conf")
	if e != nil {
		t.Fatal(e)
	}
	c, e := ParseBytes(raw)
	if e != nil {
		t.Fatal(e)
	}
	p, e := c.HardwareProfiles()
	if e != nil || len(p["graphics-demo"]) != 7 {
		t.Fatal(p, e)
	}
}
