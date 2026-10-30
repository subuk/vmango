package compute

import (
	"fmt"
	"net"
	"regexp"
	"slices"
)

var resourceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

func ValidateVM(vm *VirtualMachine) error {
	if vm.Id == "" || vm.NodeId == "" || vm.VCpus < 1 || vm.VCpus > 65535 {
		return fmt.Errorf("%w: VM ID, node and 1..65535 vCPUs are required", ErrInvalid)
	}
	b, err := sizeBytes(vm.Memory)
	if err != nil || b == 0 {
		return &ValidationError{Field: "memory_bytes", Message: "Memory must be a positive byte size."}
	}
	if !slices.Contains([]GraphicType{GraphicTypeNone, GraphicTypeVnc, GraphicTypeSpice}, vm.Graphic.Type) || !slices.Contains([]VideoModel{VideoModelNone, VideoModelCirrus, VideoModelQxl}, vm.VideoModel) {
		return fmt.Errorf("%w: unsupported graphics or video model", ErrInvalid)
	}
	return nil
}

func ValidateVolumeAttachment(v *VirtualMachineAttachedVolume) error {
	if v.Path == "" || !slices.Contains([]DeviceType{DeviceTypeDisk, DeviceTypeCdrom}, v.DeviceType) || !slices.Contains([]DeviceBus{DeviceBusVirtio, DeviceBusScsi, DeviceBusIde}, v.DeviceBus) {
		return fmt.Errorf("%w: volume path, device type and bus are required", ErrInvalid)
	}
	return nil
}

func ValidateInterface(v *VirtualMachineAttachedInterface) error {
	if v.Model == "" {
		v.Model = "virtio"
	}
	if v.NetworkName == "" || v.AccessVlan > 4094 || !slices.Contains([]string{"virtio", "e1000", "rtl8139"}, v.Model) {
		return &ValidationError{Field: "network", Message: "Choose a supported network/model and a VLAN between 0 and 4094."}
	}
	if v.Mac != "" {
		if mac, err := net.ParseMAC(v.Mac); err != nil || len(mac) != 6 {
			return &ValidationError{Field: "mac", Message: "Enter a valid six-byte MAC address, or omit it to generate one."}
		}
	}
	return nil
}

func (s *VirtualMachineService) Save(vm *VirtualMachine) error {
	if s.CloudMode && vm.Metadata == nil {
		return fmt.Errorf("%w: explicit adoption is required", ErrConflict)
	}
	if err := ValidateVM(vm); err != nil {
		return err
	}
	return s.VirtualMachineRepository.Save(vm)
}

func (s *VirtualMachineService) AttachVolume(id, node string, v *VirtualMachineAttachedVolume) error {
	if err := s.RequireManaged(id, node); err != nil {
		return err
	}
	if err := ValidateVolumeAttachment(v); err != nil {
		return err
	}
	return s.VirtualMachineRepository.AttachVolume(id, node, v)
}

func (s *VirtualMachineService) AttachInterface(id, node string, v *VirtualMachineAttachedInterface) error {
	if err := s.ValidateNetwork(node, v); err != nil {
		return err
	}
	if err := s.RequireManaged(id, node); err != nil {
		return err
	}
	if err := ValidateInterface(v); err != nil {
		return err
	}
	return s.VirtualMachineRepository.AttachInterface(id, node, v)
}

func validateVolumeCreate(p VolumeCreateParams) error {
	if !resourceName.MatchString(p.Name) || p.NodeId == "" || p.Pool == "" {
		return fmt.Errorf("%w: invalid volume name, node or pool", ErrInvalid)
	}
	b, err := sizeBytes(p.Size)
	if err != nil || b == 0 || !slices.Contains([]VolumeFormat{VolumeFormatRaw, VolumeFormatQcow2, VolumeFormatIso}, p.Format) {
		return fmt.Errorf("%w: invalid volume size or format", ErrInvalid)
	}
	return nil
}

func (s *VolumeService) Create(p VolumeCreateParams) (*Volume, error) {
	if err := validateVolumeCreate(p); err != nil {
		return nil, err
	}
	if err := s.ValidateCreate(p); err != nil {
		return nil, err
	}
	v, err := s.VolumeRepository.Create(p)
	if err != nil {
		return v, err
	}
	if err = s.recordOwnership(v, p.Cloud, p.StorageClass); err != nil {
		return v, ownershipFailure(v, err)
	}
	return v, nil
}

func (s *VolumeService) Clone(p VolumeCloneParams) (*Volume, error) {
	return s.clone(p, false)
}

// clone permits hidden sources only after the manager has verified a pinned
// managed artifact. Public volume commands always use Clone's visible-source path.
func (s *VolumeService) clone(p VolumeCloneParams, verifiedManaged bool) (*Volume, error) {
	source, err := s.Get(p.OriginalPath, p.NodeId)
	if err != nil {
		return nil, err
	}
	if source.Metadata.Hidden && !verifiedManaged {
		return nil, ErrVolumeNotFound
	}
	if source.AttachedTo != "" {
		return nil, fmt.Errorf("%w: source volume is attached", ErrConflict)
	}
	if p.NewSize.Value == 0 {
		p.NewSize = source.Size
	}
	if p.Format == VolumeFormatUnknown {
		p.Format = source.Format
	}
	create := VolumeCreateParams{NodeId: p.NodeId, Name: p.NewName, Pool: p.NewPool, Size: p.NewSize, Format: p.Format}
	if err := validateVolumeCreate(create); err != nil {
		return nil, err
	}
	if p.NewSize.Bytes() < source.Size.Bytes() {
		return nil, fmt.Errorf("%w: clone cannot shrink source", ErrInvalid)
	}
	if err := s.ValidateCreate(create); err != nil {
		return nil, err
	}
	v, err := s.VolumeRepository.Clone(p)
	if err != nil {
		return v, err
	}
	if err = s.recordOwnership(v, p.Cloud, p.StorageClass); err != nil {
		return v, ownershipFailure(v, err)
	}
	return v, nil
}

func (s *VolumeService) Resize(path, node string, size Size) error {
	v, err := s.Get(path, node)
	if err != nil {
		return err
	}
	if v.Metadata.Hidden {
		return ErrVolumeNotFound
	}
	if s.Ownership != nil && !s.Owned(v) {
		return fmt.Errorf("%w: volume ownership is not confirmed", ErrConflict)
	}
	if v.Metadata.Protected {
		return fmt.Errorf("%w: volume is protected", ErrConflict)
	}
	b, err := sizeBytes(size)
	if err != nil || b == 0 || b < v.Size.Bytes() {
		return fmt.Errorf("%w: resize cannot shrink volume", ErrInvalid)
	}
	return s.VolumeRepository.Resize(path, node, size)
}

func (s *VolumeService) Delete(path, node string) error {
	v, err := s.Get(path, node)
	if err != nil {
		return err
	}
	if v.Metadata.Hidden {
		return ErrVolumeNotFound
	}
	if s.Ownership != nil && !s.Owned(v) {
		return fmt.Errorf("%w: volume ownership is not confirmed", ErrConflict)
	}
	if v.Metadata.Protected || v.AttachedTo != "" {
		return fmt.Errorf("%w: volume is protected or attached", ErrConflict)
	}
	// Revoke before deletion so a later disk recreated at the same backend
	// path/key cannot inherit stale ownership, including after interruption.
	if s.Ownership != nil {
		if err := s.Ownership.Put(ownershipKey(node, path), VolumeOwnership{}); err != nil {
			return fmt.Errorf("cannot revoke volume ownership before deletion: %w", err)
		}
	}
	return s.VolumeRepository.Delete(path, node)
}
