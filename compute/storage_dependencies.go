package compute

// VolumeBackingStore describes actual backend metadata, not storage class policy.
type VolumeBackingStore struct {
	Path   string
	Format VolumeFormat
}

type VolumeDependency struct {
	BasePath      string
	DependentPath string
}

// StorageDependencyState is a complete host observation. An incomplete scan
// returns an error; callers must never interpret its partial result as unused.
type StorageDependencyState struct {
	Dependencies   []VolumeDependency
	WritableDisks  map[string][]string
	CanonicalPaths map[string]string
}

func (s StorageDependencyState) Referenced(path string) bool {
	if canonical := s.CanonicalPaths[path]; canonical != "" {
		path = canonical
	}
	for _, dependency := range s.Dependencies {
		if dependency.BasePath == path {
			return true
		}
	}
	return false
}

func (s StorageDependencyState) ValidateUnreferenced(paths []string) error {
	for _, path := range paths {
		if s.Referenced(path) {
			return &DependencyConflictError{Message: "This disk is the source of a dependent copy and cannot be changed or deleted while that copy exists."}
		}
	}
	return nil
}

type StorageDependencyRepository interface {
	InspectDependencies(node string) (StorageDependencyState, error)
}

// DependencyConflictError contains only a caller-safe explanation, never the
// identity of a disk or VM discovered outside the caller's workload.
type DependencyConflictError struct{ Message string }

func (e *DependencyConflictError) Error() string { return e.Message }
func (e *DependencyConflictError) Unwrap() error { return ErrConflict }
