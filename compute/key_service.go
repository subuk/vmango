package compute

import "errors"

var ErrKeyNotFound = errors.New("key not found")
var ErrKeyAlreadyExists = errors.New("already exists")

type KeyRepository interface {
	List() ([]*Key, error)
	Get(fingerprint string) (*Key, error)
	Add(input string) error
	Delete(fingerprint string) error
}

// NamespaceKeyRepository returns a view whose identity includes the namespace.
// It must never fall back to the legacy installation-wide key catalog.
type NamespaceKeyRepository interface {
	Keys(namespace NamespaceID) (KeyRepository, error)
}

type KeyService struct {
	repo       KeyRepository
	namespaces *NamespaceService
	scoped     NamespaceKeyRepository
	namespace  NamespaceID
}

// NewKeyService exposes an explicitly supplied repository, including legacy
// repositories used by operator migration. Workload wiring uses NewNamespaceKeyService.
func NewKeyService(repo KeyRepository) *KeyService {
	return &KeyService{repo: repo}
}

func NewNamespaceKeyService(repo NamespaceKeyRepository, namespaces *NamespaceService) *KeyService {
	return &KeyService{scoped: repo, namespaces: namespaces}
}

func (s *KeyService) ForNamespace(id NamespaceID) (*KeyService, error) {
	if s == nil || s.scoped == nil || s.namespaces == nil {
		return nil, ErrUnavailable
	}
	if _, err := s.namespaces.Get(id); err != nil {
		return nil, err
	}
	repo, err := s.scoped.Keys(id)
	if err != nil {
		return nil, err
	}
	return &KeyService{repo: repo, namespace: id, namespaces: s.namespaces, scoped: s.scoped}, nil
}

func (s *KeyService) check() error {
	if s == nil || s.repo == nil {
		return ErrUnavailable
	}
	if s.namespaces != nil {
		_, err := s.namespaces.Get(s.namespace)
		return err
	}
	return nil
}

func (s *KeyService) List() ([]*Key, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return s.repo.List()
}
func (s *KeyService) Get(fingerprint string) (*Key, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return s.repo.Get(fingerprint)
}
func (s *KeyService) Add(input string) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.repo.Add(input)
}
func (s *KeyService) Delete(fingerprint string) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.repo.Delete(fingerprint)
}
