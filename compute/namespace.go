package compute

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
)

// NamespaceID is a stable configured workload boundary, independent of clouds.
type NamespaceID string

type Namespace struct {
	ID          NamespaceID `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Clouds      []string    `json:"clouds" nullable:"false" minItems:"1"`
}

type WorkloadContext struct {
	Namespace NamespaceID
	Cloud     string
}

// NamespaceService owns an immutable accepted configuration snapshot.
type NamespaceService struct {
	definitions []Namespace
	mutationMu  sync.Mutex
}

func NewNamespaceService(definitions []Namespace) (*NamespaceService, error) {
	s := &NamespaceService{definitions: make([]Namespace, 0, len(definitions))}
	seen := map[NamespaceID]bool{}
	for _, n := range definitions {
		if !ValidCatalogID(string(n.ID)) || seen[n.ID] {
			return nil, invalid("namespace", "namespace IDs must be valid unique catalog identifiers")
		}
		seen[n.ID] = true
		if len(n.Clouds) == 0 {
			return nil, invalid("clouds", "namespace requires at least one allowed cloud")
		}
		clouds := map[string]bool{}
		for _, cloud := range n.Clouds {
			if !ValidCatalogID(cloud) || clouds[cloud] {
				return nil, invalid("clouds", "namespace cloud IDs must be valid and unique")
			}
			clouds[cloud] = true
		}
		if n.Name == "" {
			n.Name = string(n.ID)
		}
		n.Clouds = slices.Clone(n.Clouds)
		s.definitions = append(s.definitions, n)
	}
	return s, nil
}

func (s *NamespaceService) List() []Namespace {
	result := []Namespace{}
	if s != nil {
		for _, n := range s.definitions {
			n.Clouds = slices.Clone(n.Clouds)
			result = append(result, n)
		}
	}
	return result
}

func (s *NamespaceService) Get(id NamespaceID) (Namespace, error) {
	if !ValidCatalogID(string(id)) {
		return Namespace{}, invalid("namespace", "a valid namespace is required")
	}
	if s != nil {
		for _, n := range s.definitions {
			if n.ID == id {
				n.Clouds = slices.Clone(n.Clouds)
				return n, nil
			}
		}
	}
	return Namespace{}, fmt.Errorf("%w: namespace is not configured", ErrNotFound)
}

func (s *NamespaceService) Admit(id NamespaceID, cloud string) error {
	n, err := s.Get(id)
	if err != nil {
		return err
	}
	if !slices.Contains(n.Clouds, cloud) {
		return fmt.Errorf("%w: cloud is not admitted by namespace", ErrNotFound)
	}
	return nil
}

// ValidResourceLabel validates display labels without imposing backend naming rules.
func ValidResourceLabel(label string) bool {
	return label != "" && strings.TrimSpace(label) == label && len(label) <= 128 && utf8.ValidString(label) && !strings.ContainsAny(label, "\x00\r\n")
}

// MachineNamespaceRepository reads authoritative persistent VM metadata.
// Assignment is idempotent and must verify persistence before returning success.
type MachineNamespaceRepository interface {
	GetNamespace(id, node string) (NamespaceID, string, error)
	AssignNamespace(id, node string, namespace NamespaceID, label string) error
}

type VolumeAssignment struct {
	OwnerVM     string      `json:"owner_vm,omitempty"`
	NamespaceID NamespaceID `json:"namespace_id"`
	Label       string      `json:"label"`
}

// VolumeAssignmentRepository is independent of deletion ownership. A nil result
// means no assignment; unreadable or mismatching records always return errors.
type VolumeAssignmentRepository interface {
	Get(volume *Volume, scope string) (*VolumeAssignment, error)
	Put(volume *Volume, scope string, assignment VolumeAssignment) error
	Delete(volume *Volume, scope string) error
}

// VolumeAssignmentCreationRepository prevents a newly created volume from
// inheriting an old record, even when the backend later reuses every identifier.
// The caller must first confirm the destination is absent from the backend.
type VolumeAssignmentCreationRepository interface {
	CheckCreate(node, path, scope string) error
}
