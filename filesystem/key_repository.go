package filesystem

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"subuk/vmango/compute"
	"subuk/vmango/util"

	"golang.org/x/crypto/ssh"

	"github.com/rs/zerolog"
)

const KEY_UPLOAD_FILENAME = "vmango_authorized_keys"

type KeyRepository struct {
	filename string
	logger   zerolog.Logger
}

func NewKeyRepository(filename string, logger zerolog.Logger) (*KeyRepository, error) {
	dirname := filepath.Dir(filename)
	if err := os.MkdirAll(dirname, 0755); err != nil {
		return nil, util.NewError(err, "cannot create base directory")
	}
	file, err := os.OpenFile(filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, util.NewError(err, "cannot open key file for writing")
	}
	defer file.Close()

	repo := &KeyRepository{filename: util.ExpandHomeDir(filename), logger: logger}
	return repo, nil
}

func (repo *KeyRepository) parseKey(input []byte) (*compute.Key, error) {
	pubkey, comment, options, _, err := ssh.ParseAuthorizedKey(input)
	if err != nil {
		return nil, err
	}
	key := &compute.Key{
		Type:        pubkey.Type(),
		Value:       input,
		Comment:     comment,
		Options:     options,
		Fingerprint: ssh.FingerprintLegacyMD5(pubkey),
	}
	return key, nil
}

func (repo *KeyRepository) List() ([]*compute.Key, error) {
	file, err := os.Open(repo.filename)
	if err != nil {
		return nil, util.NewError(err, "cannot open key file")
	}
	defer file.Close()

	keys := []*compute.Key{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, err := repo.parseKey(scanner.Bytes())
		if err != nil {
			repo.logger.Warn().Err(err).Msg("ignoring invalid key line")
			continue
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func (repo *KeyRepository) Get(fingerprint string) (*compute.Key, error) {
	keys, err := repo.List()
	if err != nil {
		return nil, util.NewError(err, "cannot load keys")
	}
	for _, key := range keys {
		if key.Fingerprint == fingerprint {
			return key, nil
		}
	}
	return nil, compute.ErrKeyNotFound
}

func (repo *KeyRepository) Add(input []byte) error {
	input = bytes.TrimSpace(input)

	newKey, err := repo.parseKey(input)
	if err != nil {
		return util.NewError(err, "cannot parse provided key")
	}
	existingKey, err := repo.Get(newKey.Fingerprint)
	if err != nil && err != compute.ErrKeyNotFound {
		return util.NewError(err, "cannot check if key already exists")
	}
	if existingKey != nil {
		return compute.ErrKeyAlreadyExists
	}
	file, err := os.OpenFile(repo.filename, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return util.NewError(err, "cannot open key file")
	}
	defer file.Close()

	input = append(input, '\n')
	if _, err := file.Write(input); err != nil {
		return util.NewError(err, "cannot write key")
	}

	return nil
}

func (repo *KeyRepository) Delete(fingerprint string) error {
	file, err := os.Open(repo.filename)
	if err != nil {
		return util.NewError(err, "cannot open key file")
	}
	defer file.Close()

	keyFound := false
	contentWithoutKey := []byte{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if bytes.Equal(line, []byte("")) {
			continue
		}
		key, err := repo.parseKey(line)
		line = append(line, '\n')
		if err != nil {
			contentWithoutKey = append(contentWithoutKey, line...)
			continue
		}
		if key.Fingerprint != fingerprint {
			contentWithoutKey = append(contentWithoutKey, line...)
			continue
		}
		keyFound = true
	}
	if !keyFound {
		return compute.ErrKeyNotFound
	}
	if err := ioutil.WriteFile(file.Name(), contentWithoutKey, 0644); err != nil {
		return util.NewError(err, "cannot rewrite key file")
	}
	return nil
}
