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

func TestVirtioImageDisplayCapabilities(t *testing.T) {
	image := compute.Image{ID: "desktop", Arch: compute.ArchAarch64, SourcePath: "/source", Defaults: compute.ImageDefaults{Graphics: "vnc", VideoModel: "virtio"}}
	if err := image.Validate(); err != nil {
		t.Fatal(err)
	}
	caps := compute.VirtualMachineCapabilities{Arch: image.Arch, Graphics: []compute.GraphicType{compute.GraphicTypeVnc}, VideoModels: []compute.VideoModel{compute.VideoModelNone}}
	if image.Compatible(caps) {
		t.Fatal("virtio display offered without backend video support")
	}
	caps.VideoModels = append(caps.VideoModels, compute.VideoModelVirtio)
	if !image.Compatible(caps) {
		t.Fatal("virtio display rejected despite backend support")
	}
	graphics, video := image.Defaults.Display()
	vm := &compute.VirtualMachine{Id: "desktop", NodeId: "node", VCpus: 2, Memory: compute.NewSize(2, compute.SizeUnitG), Graphic: compute.VirtualMachineGraphic{Type: graphics}, VideoModel: video}
	if err := compute.ValidateVM(vm); err != nil {
		t.Fatalf("image-selected display rejected at VM save: %v", err)
	}
	if vm.VideoModel.String() != "virtio" {
		t.Fatalf("display name = %q", vm.VideoModel.String())
	}
}
