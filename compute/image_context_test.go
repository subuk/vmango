package compute

import "testing"

func TestLegacyPinnedImagesInheritAcceptedCloud(t *testing.T) {
	enabled := false
	old := Image{ID: "/legacy/path", SourcePath: "/accepted/source", Arch: ArchAmd64, Defaults: ImageDefaults{Firmware: "efi", GuestAgent: &enabled}}
	service := NewImageService(pinnedImages{{CloudID: "cloud", ID: old.ID, SourcePath: "/changed/today", Arch: ArchAmd64}})
	view := service.ForCloud("cloud").Snapshot([]Image{old}, nil, false)
	image, err := view.Get(old.ID)
	if err != nil || image.CloudID != "cloud" || image.SourcePath != "/accepted/source" || image.Defaults.Firmware != "efi" {
		t.Fatalf("legacy accepted image changed: %+v %v", image, err)
	}
	*image.Defaults.GuestAgent = true
	if enabled {
		t.Fatal("snapshot aliases accepted definition")
	}
	if _, err := view.ForCloud("different").Get(old.ID); err == nil {
		t.Fatal("pinned cloud escaped")
	}
}

func TestAcceptedImageCloudMismatchFailsBeforeMutation(t *testing.T) {
	for _, c := range []InfrastructureCommand{
		{CloudID: "a", Catalog: &Cloud{ID: "b"}, Images: []Image{{ID: "linux"}}},
		{CloudID: "a", Images: []Image{{CloudID: "b", ID: "linux"}}},
		{CloudID: "a", Machine: &VirtualMachine{Metadata: &MachineMetadata{Cloud: "b"}}, Images: []Image{{ID: "linux"}}},
	} {
		if err := validateAcceptedImageContext(c); err == nil {
			t.Fatal("inconsistent accepted image context allowed")
		}
	}
	if err := validateAcceptedImageContext(InfrastructureCommand{CloudID: "a", Catalog: &Cloud{ID: "a"}, Images: []Image{{ID: "/legacy/path"}}}); err != nil {
		t.Fatal("legacy snapshot context rejected", err)
	}
}
