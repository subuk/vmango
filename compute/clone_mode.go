package compute

// CloneMode is the storage class policy for disks created from existing data.
type CloneMode string

const (
	CloneModeCopy    CloneMode = "copy"
	CloneModeOverlay CloneMode = "overlay"
)

func (m CloneMode) Effective() CloneMode {
	if m == "" {
		return CloneModeCopy
	}
	return m
}

func (m CloneMode) Valid() bool {
	return m.Effective() == CloneModeCopy || m == CloneModeOverlay
}

// StorageClass describes a cloud-wide policy; host bindings supply pool and format.
type StorageClass struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CloneMode   CloneMode `json:"clone_mode" enum:"copy,overlay"`
}

func (c Cloud) StorageClass(id string) (StorageClass, error) {
	for _, class := range c.StorageClasses {
		if class.ID == id {
			class.CloneMode = class.CloneMode.Effective()
			return class, nil
		}
	}
	return StorageClass{}, invalid("storage_class", "Unknown storage class.")
}

func hasStorageClass(classes []StorageClass, id string) bool {
	for _, class := range classes {
		if class.ID == id {
			return true
		}
	}
	return false
}
