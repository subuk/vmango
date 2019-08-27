package compute

import (
	"errors"
)

var ErrKeyNotFound = errors.New("key not found")
var ErrKeyAlreadyExists = errors.New("already exists")

type KeyRepository interface {
	List() ([]*Key, error)
	Get(fingerprint string) (*Key, error)
	Add(input []byte) error
	Delete(fingerprint string) error
}

type Key struct {
	Type        string
	Value       []byte
	Comment     string
	Options     []string
	Fingerprint string
}

func (key *Key) ValueString() string {
	return string(key.Value)
}
