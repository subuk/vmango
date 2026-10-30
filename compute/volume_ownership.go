package compute

import (
	"fmt"
	"subuk/vmango/tasks"
	"time"
)

// VolumeOwnership records exact resources created by Vmango, separately from
// VM adoption and catalog bindings. The store never infers ownership by name.
type VolumeOwnership struct{ Cloud, StorageClass, Node, Path, Pool, Identity string }
type VolumeOwnershipRepository interface {
	Get(key string) (VolumeOwnership, error)
	Put(key string, value VolumeOwnership) error
}

func ownershipKey(node, path string) string { return node + "\x00" + path }
func (s *VolumeService) recordOwnership(v *Volume, cloud, class string) error {
	if s.Ownership == nil {
		return nil
	}
	if v.Identity == "" {
		return fmt.Errorf("backend did not return volume identity; ownership was not recorded")
	}
	return s.Ownership.Put(ownershipKey(v.NodeId, v.Path), VolumeOwnership{Cloud: cloud, StorageClass: class, Node: v.NodeId, Path: v.Path, Pool: v.Pool, Identity: v.Identity})
}
func (s *VolumeService) Owned(v *Volume) bool {
	if s.Ownership == nil {
		return false
	}
	o, err := s.Ownership.Get(ownershipKey(v.NodeId, v.Path))
	return err == nil && o.Path == v.Path && o.Node == v.NodeId && o.Pool == v.Pool && o.Identity != "" && o.Identity == v.Identity
}
func (s *VolumeService) OwnershipFor(v *Volume) (VolumeOwnership, bool) {
	if s.Ownership == nil {
		return VolumeOwnership{}, false
	}
	o, err := s.Ownership.Get(ownershipKey(v.NodeId, v.Path))
	return o, err == nil && s.Owned(v)
}

func ownershipFailure(v *Volume, err error) error {
	return &tasks.DiagnosticError{Details: tasks.Diagnostic{Message: "Volume was created but ownership metadata could not be persisted: " + err.Error(), Operation: "record-volume-ownership", Backend: "filesystem"}, Cause: err, Effects: []tasks.Effect{{Resource: VolumeRef(v.NodeId, v.Path), Action: "created", At: time.Now().UTC()}}}
}
