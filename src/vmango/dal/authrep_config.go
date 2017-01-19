package dal

import (
	"vmango/cfg"
	"vmango/models"
)

type ConfigAuthrep struct {
	authdb []*models.User
}

func NewConfigAuthrep(db []cfg.AuthUserConfig) *ConfigAuthrep {
	repo := &ConfigAuthrep{}
	for _, userConfig := range db {
		repo.authdb = append(repo.authdb, &models.User{
			Name:           userConfig.Username,
			HashedPassword: []byte(userConfig.PasswordHash),
		})
	}
	return repo
}

func (repo *ConfigAuthrep) Get(needle *models.User) (bool, error) {
	for _, user := range repo.authdb {
		if user.Name == needle.Name {
			*needle = *user
			return true, nil
		}
	}
	return false, nil
}
