package catalog_test

import (
	"errors"
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
