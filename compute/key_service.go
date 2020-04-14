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

type KeyService struct {
	KeyRepository
}

func NewKeyService(repo KeyRepository) *KeyService {
	return &KeyService{repo}
}
