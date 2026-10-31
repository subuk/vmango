package compute

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

// VolumeAddress is a host-local backend address. Its public representation is
// reversible transport encoding; backend identity and task lock scopes stay raw.
type VolumeAddress struct {
	Host string
	Path string
}

func (a VolumeAddress) ID() string {
	return base64.RawURLEncoding.EncodeToString([]byte(a.Host)) + "." + base64.RawURLEncoding.EncodeToString([]byte(a.Path))
}

func ParseVolumeID(id string) (VolumeAddress, error) {
	parts := strings.Split(id, ".")
	if len(parts) != 2 {
		return VolumeAddress{}, fmt.Errorf("%w: invalid volume identifier", ErrInvalid)
	}
	values := make([]string, 2)
	for n, part := range parts {
		decoded, err := base64.RawURLEncoding.DecodeString(part)
		if err != nil || len(decoded) == 0 || base64.RawURLEncoding.EncodeToString(decoded) != part || (n == 1 && len(decoded) > 4096) {
			return VolumeAddress{}, fmt.Errorf("%w: invalid volume identifier", ErrInvalid)
		}
		values[n] = string(decoded)
	}
	return VolumeAddress{Host: values[0], Path: values[1]}, nil
}

// VolumeAddress validates cloud membership before a caller can touch storage.
func (s *CloudService) VolumeAddress(cloudID, id string) (VolumeAddress, error) {
	if _, err := s.Get(cloudID); err != nil {
		return VolumeAddress{}, err
	}
	a, err := ParseVolumeID(id)
	if err != nil {
		return a, err
	}
	_, _, err = s.ForHost(cloudID, a.Host)
	return a, err
}

func (s *CloudService) GetVolume(cloudID, id string) (*Volume, error) {
	a, err := s.VolumeAddress(cloudID, id)
	if err != nil {
		return nil, err
	}
	return s.volumeAtAddress(a)
}

func (s *CloudService) volumeAtAddress(a VolumeAddress) (*Volume, error) {
	v, err := s.Volumes.Get(a.Path, a.Host)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// MachineVolume resolves the VM independently from the submitted disk address.
// The volume host may never override VM discovery or select another host's path.
func (s *CloudService) MachineVolume(ctx context.Context, cloudID, machine, id string) (*Volume, error) {
	_, volume, err := s.LocateMachineVolume(ctx, cloudID, machine, id)
	return volume, err
}

// LocateMachineVolume returns the independently resolved VM placement so a
// caller can pin that placement in the existing durable command. Re-resolving
// the VM after discarding the disk host could select an equal path elsewhere.
func (s *CloudService) LocateMachineVolume(ctx context.Context, cloudID, machine, id string) (*VirtualMachine, *Volume, error) {
	a, err := s.VolumeAddress(cloudID, id)
	if err != nil {
		return nil, nil, err
	}
	vm, err := s.GetMachine(ctx, cloudID, machine)
	if err != nil {
		return nil, nil, err
	}
	if vm.NodeId != a.Host {
		return nil, nil, invalid("volume_id", "The disk and machine must belong to the same host.")
	}
	volume, err := s.volumeAtAddress(a)
	return vm, volume, err
}
