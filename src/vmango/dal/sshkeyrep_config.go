package dal

import (
	"vmango/cfg"
	"vmango/domain"
)

type ConfigSSHKeyrep struct {
	keys []*domain.SSHKey
}

func NewConfigSSHKeyrep(keyConfigs []cfg.SSHKeyConfig) *ConfigSSHKeyrep {
	repo := &ConfigSSHKeyrep{
		keys: []*domain.SSHKey{},
	}
	for _, keyConfig := range keyConfigs {
		key := &domain.SSHKey{
			Name:   keyConfig.Name,
			Public: keyConfig.Public,
		}
		repo.keys = append(repo.keys, key)
	}
	return repo
}

func (repo *ConfigSSHKeyrep) List(keys *[]*domain.SSHKey) error {
	*keys = *(&repo.keys)
	return nil

}

func (repo *ConfigSSHKeyrep) Get(needle *domain.SSHKey) (bool, error) {
	for _, key := range repo.keys {
		if key.Name == needle.Name {
			*needle = *key
			return true, nil
		}
	}
	return false, nil
}
