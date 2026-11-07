package libvirt

import (
	"errors"
	"fmt"
	"path"
	"subuk/vmango/compute"
	"subuk/vmango/tasks"

	xml "libvirt.org/go/libvirtxml"
)

func cloneMutationError(err error, params compute.VolumeCloneParams, ref tasks.ResourceRef, effects []tasks.Effect) error {
	err = mutationError(err, "storage-volume.clone", ref, effects)
	var diagnostic *tasks.DiagnosticError
	if errors.As(err, &diagnostic) {
		diagnostic.Details.Message = fmt.Sprintf("clone on host %s storage class %s: %s", params.NodeId, params.StorageClass, diagnostic.Details.Message)
	}
	return err
}

func cloneVolumeConfig(params compute.VolumeCloneParams, source *compute.Volume) (*xml.StorageVolume, error) {
	if !params.CloneMode.Valid() {
		return nil, fmt.Errorf("%w: unknown clone mode", compute.ErrInvalid)
	}
	if source.Format != compute.VolumeFormatRaw && source.Format != compute.VolumeFormatQcow2 {
		return nil, fmt.Errorf("%w: unsupported clone source format", compute.ErrInvalid)
	}
	if params.Format != compute.VolumeFormatRaw && params.Format != compute.VolumeFormatQcow2 {
		return nil, fmt.Errorf("%w: unsupported clone destination format", compute.ErrInvalid)
	}
	if params.NewSize.Bytes() < source.Size.Bytes() {
		return nil, fmt.Errorf("%w: clone cannot shrink source", compute.ErrInvalid)
	}
	config := &xml.StorageVolume{Name: params.NewName, Capacity: &xml.StorageVolumeSize{Unit: "bytes", Value: params.NewSize.Bytes()}, Target: &xml.StorageVolumeTarget{Format: &xml.StorageVolumeTargetFormat{Type: params.Format.String()}}}
	if params.CloneMode.Effective() == compute.CloneModeOverlay {
		if params.Format != compute.VolumeFormatQcow2 {
			return nil, fmt.Errorf("%w: dependent copies require a qcow2 destination", compute.ErrInvalid)
		}
		if source.BackingStore != nil {
			return nil, fmt.Errorf("%w: cannot create a dependent copy from a dependent copy", compute.ErrInvalid)
		}
		config.BackingStore = &xml.StorageVolumeBackingStore{Path: source.Path, Format: &xml.StorageVolumeTargetFormat{Type: source.Format.String()}}
		config.Allocation = &xml.StorageVolumeSize{Unit: "bytes", Value: 0}
	}
	return config, nil
}

func verifyClonedVolume(params compute.VolumeCloneParams, source, actual *compute.Volume) error {
	if actual.Format != params.Format || actual.Size.Bytes() != params.NewSize.Bytes() {
		return fmt.Errorf("created clone format or capacity does not match request")
	}
	if params.CloneMode.Effective() == compute.CloneModeCopy {
		if actual.BackingStore != nil {
			return fmt.Errorf("independent clone unexpectedly has a backing store")
		}
		return nil
	}
	if actual.BackingStore == nil || path.Clean(actual.BackingStore.Path) != path.Clean(source.Path) || actual.BackingStore.Format != source.Format {
		return fmt.Errorf("created clone backing store does not match source")
	}
	return nil
}
