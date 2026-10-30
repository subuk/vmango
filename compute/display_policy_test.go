package compute_test

import (
	"subuk/vmango/compute"
	"testing"
)

func TestImageDisplayCapabilities(t *testing.T) {
	image := compute.Image{ID: "test", Arch: compute.ArchAmd64, SourcePath: "/source"}
	caps := compute.VirtualMachineCapabilities{Arch: compute.ArchAmd64, Graphics: []compute.GraphicType{compute.GraphicTypeNone}, VideoModels: []compute.VideoModel{compute.VideoModelNone}}
	if image.Compatible(caps) {
		t.Fatal("default VNC offered on headless-only backend")
	}
	image.Defaults.Graphics = "none"
	if !image.Compatible(caps) {
		t.Fatal("headless image rejected")
	}
	image.Defaults.Graphics = "spice"
	image.Defaults.VideoModel = "qxl"
	caps.Graphics = append(caps.Graphics, compute.GraphicTypeSpice)
	if image.Compatible(caps) {
		t.Fatal("QXL offered without backend video support")
	}
	caps.VideoModels = append(caps.VideoModels, compute.VideoModelQxl)
	if !image.Compatible(caps) {
		t.Fatal("compatible display combination rejected")
	}
}
