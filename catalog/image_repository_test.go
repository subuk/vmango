package catalog_test

import (
	"errors"
	"reflect"
	"subuk/vmango/catalog"
	"subuk/vmango/compute"
	"testing"
)

func TestCloudOwnedImageIdentityAndCopies(t *testing.T) {
	disabled := false
	a := compute.Image{CloudID: "a", ID: "linux", SourcePath: "/shared", Arch: compute.ArchAmd64, Defaults: compute.ImageDefaults{Firmware: "bios", GuestAgent: &disabled}}
	b := a
	b.CloudID = "b"
	b.SourcePath = "/other"
	b.Defaults.Firmware = "efi"
	repo, err := catalog.NewImageRepository([]compute.Image{a, b})
	if err != nil {
		t.Fatal(err)
	}
	service := compute.NewImageService(repo)
	for _, want := range []compute.Image{a, b} {
		got, err := service.ForCloud(want.CloudID).Get("linux")
		if err != nil || got.CloudID != want.CloudID || got.SourcePath != want.SourcePath || got.Defaults.Firmware != want.Defaults.Firmware {
			t.Fatalf("cross-cloud lookup: %+v %v", got, err)
		}
		*got.Defaults.GuestAgent = true
		got, _ = service.ForCloud(want.CloudID).Get("linux")
		if *got.Defaults.GuestAgent {
			t.Fatal("caller mutated accepted image")
		}
		list, _ := service.ForCloud(want.CloudID).List()
		if len(list) != 1 || list[0].CloudID != want.CloudID {
			t.Fatal(list)
		}
	}
	if _, err := service.ForCloud("unknown").Get("linux"); !errors.Is(err, compute.ErrNotFound) {
		t.Fatalf("missing cloud fell back: %v", err)
	}
	if _, err := service.Get("linux"); !errors.Is(err, compute.ErrNotFound) {
		t.Fatalf("unscoped lookup fell back: %v", err)
	}
	if _, err := catalog.NewImageRepository([]compute.Image{a, a}); err == nil {
		t.Fatal("duplicate within a cloud accepted")
	}
}

func TestImageDataDisksAreCopied(t *testing.T) {
	want := []compute.ImageDataDisk{{Name: "userdata", SizeBytes: 16 << 30}, {Name: "cache", SizeBytes: 2 << 30}}
	input := compute.Image{CloudID: "local", ID: "android", SourcePath: "/android", Arch: compute.ArchAarch64,
		DataDisks: []compute.ImageDataDisk{{Name: "userdata", SizeBytes: 16 << 30}, {Name: "cache", SizeBytes: 2 << 30}}}
	repo, err := catalog.NewImageRepository([]compute.Image{input})
	if err != nil {
		t.Fatal(err)
	}
	input.DataDisks[0].SizeBytes = 1
	got, err := repo.Get("local", "android")
	if err != nil || !reflect.DeepEqual(got.DataDisks, want) {
		t.Fatalf("constructor input changed stored disks: %+v, %v", got.DataDisks, err)
	}
	got.DataDisks[0].Name = "changed"
	list, err := repo.List("local")
	if err != nil || len(list) != 1 || !reflect.DeepEqual(list[0].DataDisks, want) {
		t.Fatalf("Get result changed stored disks: %+v, %v", list, err)
	}
	list[0].DataDisks[1].SizeBytes = 1
	got, err = repo.Get("local", "android")
	if err != nil || !reflect.DeepEqual(got.DataDisks, want) {
		t.Fatalf("List result changed stored disks: %+v, %v", got.DataDisks, err)
	}
}
