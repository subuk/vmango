package api

import (
	"fmt"
	"github.com/danielgtaylor/huma/v2"
	"strings"
	"subuk/vmango/compute"
	"testing"
)

func TestVolumeDTOReportsObservedDependency(t *testing.T) {
	v := &compute.Volume{NodeId: "host", Path: "/disk", Size: compute.NewSize(1, compute.SizeUnitG), Format: compute.VolumeFormatQcow2}
	if got := volumeDTO(v).CloneMode; got != compute.CloneModeCopy {
		t.Fatalf("independent disk mode = %q", got)
	}
	v.BackingStore = &compute.VolumeBackingStore{Path: "/base", Format: compute.VolumeFormatRaw}
	if got := volumeDTO(v).CloneMode; got != compute.CloneModeOverlay {
		t.Fatalf("dependent disk mode = %q", got)
	}
}

func TestDependencyConflictOnlyExposesSafeExplanation(t *testing.T) {
	safe := "This disk is the source of a dependent copy and cannot be deleted."
	err := apiError(fmt.Errorf("private foreign path: %w", &compute.DependencyConflictError{Message: safe}))
	problem, ok := err.(*huma.ErrorModel)
	if !ok || problem.Status != 409 || problem.Detail != safe || strings.Contains(problem.Detail, "private") {
		t.Fatalf("unsafe conflict response: %+v", err)
	}
}
