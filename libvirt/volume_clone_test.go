package libvirt

import (
	"errors"
	vir "libvirt.org/go/libvirt"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"
	"testing"
)

func TestCloneVolumeConfigModes(t *testing.T) {
	for _, format := range []compute.VolumeFormat{compute.VolumeFormatRaw, compute.VolumeFormatQcow2} {
		source := &compute.Volume{Path: "/pool/base", Format: format, Size: compute.NewSize(1, compute.SizeUnitG)}
		p := compute.VolumeCloneParams{NewName: "child", NewSize: compute.NewSize(2, compute.SizeUnitG), Format: compute.VolumeFormatQcow2, CloneMode: compute.CloneModeOverlay}
		config, err := cloneVolumeConfig(p, source)
		if err != nil {
			t.Fatal(err)
		}
		if config.BackingStore == nil || config.BackingStore.Path != source.Path || config.BackingStore.Format.Type != format.String() || config.Capacity.Value != p.NewSize.Bytes() {
			t.Fatalf("invalid overlay: %#v", config)
		}
		description, err := config.Marshal()
		if err != nil || !strings.Contains(description, "<backingStore>") {
			t.Fatalf("missing backing XML: %s %v", description, err)
		}
		p.CloneMode = ""
		config, err = cloneVolumeConfig(p, source)
		if err != nil || config.BackingStore != nil {
			t.Fatalf("legacy copy acquired backing store: %#v %v", config, err)
		}
		source.BackingStore = &compute.VolumeBackingStore{Path: "/pool/base0", Format: compute.VolumeFormatRaw}
		config, err = cloneVolumeConfig(p, source)
		if err != nil || config.BackingStore != nil {
			t.Fatalf("copy of overlay must flatten: %#v %v", config, err)
		}
		p.CloneMode = compute.CloneModeOverlay
		if _, err = cloneVolumeConfig(p, source); err == nil {
			t.Fatal("accepted overlay of overlay")
		}
	}
}

func TestCloneRejectsInvalidDestination(t *testing.T) {
	source := &compute.Volume{Path: "/base", Format: compute.VolumeFormatRaw, Size: compute.NewSize(2, compute.SizeUnitG)}
	for _, p := range []compute.VolumeCloneParams{
		{CloneMode: compute.CloneModeOverlay, Format: compute.VolumeFormatRaw, NewSize: source.Size},
		{CloneMode: compute.CloneModeOverlay, Format: compute.VolumeFormatQcow2, NewSize: compute.NewSize(1, compute.SizeUnitG)},
		{CloneMode: "invalid", Format: compute.VolumeFormatQcow2, NewSize: source.Size},
	} {
		if _, err := cloneVolumeConfig(p, source); err == nil {
			t.Fatalf("accepted invalid request: %+v", p)
		}
	}
}

func TestVerifyClonedVolume(t *testing.T) {
	source := &compute.Volume{Path: "/base", Format: compute.VolumeFormatRaw, Size: compute.NewSize(1, compute.SizeUnitG)}
	p := compute.VolumeCloneParams{CloneMode: compute.CloneModeOverlay, Format: compute.VolumeFormatQcow2, NewSize: source.Size}
	actual := &compute.Volume{Format: p.Format, Size: p.NewSize, BackingStore: &compute.VolumeBackingStore{Path: source.Path, Format: source.Format}}
	if err := verifyClonedVolume(p, source, actual); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []*compute.Volume{
		{Format: p.Format, Size: p.NewSize},
		{Format: compute.VolumeFormatRaw, Size: p.NewSize, BackingStore: actual.BackingStore},
		{Format: p.Format, Size: compute.NewSize(2, compute.SizeUnitG), BackingStore: actual.BackingStore},
		{Format: p.Format, Size: p.NewSize, BackingStore: &compute.VolumeBackingStore{Path: "/wrong", Format: source.Format}},
		{Format: p.Format, Size: p.NewSize, BackingStore: &compute.VolumeBackingStore{Path: source.Path, Format: compute.VolumeFormatQcow2}},
	} {
		if err := verifyClonedVolume(p, source, bad); err == nil {
			t.Fatalf("accepted incorrect result: %+v", bad)
		}
	}
	p.CloneMode = compute.CloneModeCopy
	if err := verifyClonedVolume(p, source, actual); err == nil {
		t.Fatal("accepted backing store on independent copy")
	}
	actual.BackingStore = nil
	if err := verifyClonedVolume(p, source, actual); err != nil {
		t.Fatal(err)
	}
}

func TestCloneDiagnosticPreservesContextAndLibvirtCause(t *testing.T) {
	cause := vir.Error{Code: vir.ERR_OPERATION_FAILED, Domain: vir.FROM_STORAGE, Message: "backend clone failure"}
	params := compute.VolumeCloneParams{NodeId: "host-a", StorageClass: "efficient"}
	ref := compute.VolumeRef(params.NodeId, "/pool/child")
	err := cloneMutationError(repositoryError(cause, "create overlay"), params, ref, []tasks.Effect{confirmed(ref, "created")})
	var detail *tasks.DiagnosticError
	if !errors.As(err, &detail) {
		t.Fatal("lost diagnostic")
	}
	for _, expected := range []string{"host-a", "efficient", "backend clone failure"} {
		if !strings.Contains(detail.Details.Message, expected) {
			t.Fatalf("missing context %q: %s", expected, detail.Details.Message)
		}
	}
	if detail.Details.Operation != "create overlay" || len(detail.Effects) != 1 || !errors.Is(err, vir.ERR_OPERATION_FAILED) {
		t.Fatalf("lost backend cause/effect: %+v", detail)
	}
}
