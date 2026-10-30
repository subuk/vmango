package compute_test

import (
	"errors"
	"subuk/vmango/catalog"
	"subuk/vmango/compute"
	"testing"
)

type selectionVolumes struct {
	compute.VolumeRepository
	items []*compute.Volume
	err   error
}

func (r *selectionVolumes) List(compute.VolumeListOptions) ([]*compute.Volume, error) {
	return r.items, r.err
}
func (r *selectionVolumes) Get(path, node string) (*compute.Volume, error) {
	for _, v := range r.items {
		if v.Path == path && v.NodeId == node {
			return v, nil
		}
	}
	return nil, compute.ErrVolumeNotFound
}

func TestAttachableVolumesExcludeAllImageSourcesAndRevalidate(t *testing.T) {
	catalogue, err := catalog.NewImageRepository([]compute.Image{{ID: "amd64", SourcePath: "/x86-image", Arch: compute.ArchAmd64}, {ID: "arm64", SourcePath: "/arm-image", Arch: compute.ArchAarch64}})
	if err != nil {
		t.Fatal(err)
	}
	images := compute.NewImageService(catalogue)
	repo := &selectionVolumes{items: []*compute.Volume{
		{NodeId: "node", Path: "/x86-image"}, {NodeId: "node", Path: "/arm-image"}, {NodeId: "node", Path: "/attached", AttachedTo: "vm"}, {NodeId: "node", Path: "/hidden", Metadata: compute.VolumeMetadata{Hidden: true}}, {NodeId: "node", Path: "/protected", Metadata: compute.VolumeMetadata{Protected: true}}, {NodeId: "node", Path: "/ordinary"},
	}}
	service := compute.NewVolumeService(repo)
	items, err := service.Attachable(compute.VolumeListOptions{NodeIds: []string{"node"}}, images)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Path != "/protected" || items[1].Path != "/ordinary" {
		t.Fatal("incorrect attachable selection", items)
	}
	for _, path := range []string{"/x86-image", "/arm-image", "/attached", "/hidden"} {
		if err := service.ValidateAttachable(path, "node", images); !errors.Is(err, compute.ErrConflict) {
			t.Fatal("ineligible disk accepted", path, err)
		}
	}
	if err := service.ValidateAttachable("/protected", "node", images); err != nil {
		t.Fatal("protection was incorrectly treated as attachment prohibition", err)
	}
	repo.items[5].AttachedTo = "another-vm"
	if err := service.ValidateAttachable("/ordinary", "node", images); !errors.Is(err, compute.ErrConflict) {
		t.Fatal("attachment eligibility used stale list", err)
	}
	if _, err := service.Get("/hidden", "node"); err != nil {
		t.Fatal("authorized detail read should preserve hidden disk labels", err)
	}
	if err := service.Delete("/hidden", "node"); !errors.Is(err, compute.ErrVolumeNotFound) {
		t.Fatal("detail visibility changed hidden-volume mutation policy", err)
	}
}
