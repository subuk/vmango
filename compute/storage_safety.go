package compute

import (
	"fmt"
	"io"
)

func validateCloneSource(p VolumeCloneParams, source *Volume) error {
	if !p.CloneMode.Valid() {
		return fmt.Errorf("%w: unknown clone mode", ErrInvalid)
	}
	if p.SourceIdentity != "" && source.Identity != p.SourceIdentity {
		return fmt.Errorf("%w: accepted source identity changed", ErrConflict)
	}
	if source.Format != VolumeFormatRaw && source.Format != VolumeFormatQcow2 {
		return fmt.Errorf("%w: unsupported clone source format", ErrInvalid)
	}
	if p.CloneMode.Effective() == CloneModeOverlay {
		if p.Format != VolumeFormatQcow2 {
			return invalid("storage_class", "The selected storage class cannot create dependent copies with its configured disk format. Contact your administrator.")
		}
		if source.BackingStore != nil {
			return &DependencyConflictError{Message: "A dependent copy cannot be used as the source of another dependent copy. Select a storage class that creates independent copies."}
		}
	}
	return nil
}

// ValidateUnreferenced uses the infrastructure view, independently of caller
// visibility. Conflict messages deliberately do not identify dependent disks.
func (s *VolumeService) ValidateUnreferenced(node string, paths []string) error {
	if len(paths) == 0 || s.Dependencies == nil {
		return nil
	}
	state, err := s.Dependencies.InspectDependencies(node)
	if err != nil {
		return err
	}
	return state.ValidateUnreferenced(paths)
}

func (s *VirtualMachineService) ValidateStart(id, node string) error {
	if s.Dependencies == nil {
		return nil
	}
	state, err := s.Dependencies.InspectDependencies(node)
	if err != nil {
		return err
	}
	paths, ok := state.WritableDisks[id]
	if !ok {
		return fmt.Errorf("%w: cannot verify the machine's writable disks", ErrUnavailable)
	}
	for _, path := range paths {
		if state.Referenced(path) {
			return &DependencyConflictError{Message: "The machine would write to a disk used as the source of a dependent copy. Its source must remain unchanged while that copy exists."}
		}
	}
	return nil
}

// Upload can overwrite an existing disk, including an image or config drive.
func (s *VolumeService) Upload(path, node string, content io.Reader, size uint64) error {
	// Resolve access before performing the host-wide dependency inspection.
	if _, err := s.Get(path, node); err != nil {
		return err
	}
	if err := s.ValidateUnreferenced(node, []string{path}); err != nil {
		return err
	}
	return s.VolumeRepository.Upload(path, node, content, size)
}
