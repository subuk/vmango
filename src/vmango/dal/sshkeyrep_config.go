package dal

import (
	"vmango/cfg"
	"vmango/models"
)

type ConfigSSHKeyrep struct {
	keys []*models.SSHKey
}

func NewConfigSSHKeyrep(keyConfigs []cfg.SSHKeyConfig) *ConfigSSHKeyrep {
	repo := &ConfigSSHKeyrep{
		keys: []*models.SSHKey{},
	}
	for _, keyConfig := range keyConfigs {
		key := &models.SSHKey{
			Name:   keyConfig.Name,
			Public: keyConfig.Public,
		}
		repo.keys = append(repo.keys, key)
	}
	return repo
}

func (repo *ConfigSSHKeyrep) List(keys *[]*models.SSHKey) error {
	*keys = *(&repo.keys)
	return nil

}

func (repo *ConfigSSHKeyrep) Get(needle *models.SSHKey) (bool, error) {
	for _, key := range repo.keys {
		if key.Name == needle.Name {
			*needle = *key
			return true, nil
		}
	}
	return false, nil
}
