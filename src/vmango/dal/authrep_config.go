package dal

import (
	"vmango/cfg"
	"vmango/domain"
)

type ConfigAuthrep struct {
	authdb []*domain.User
}

func NewConfigAuthrep(db []cfg.AuthUserConfig) *ConfigAuthrep {
	repo := &ConfigAuthrep{}
	for _, userConfig := range db {
		repo.authdb = append(repo.authdb, &domain.User{
			Name:           userConfig.Username,
			HashedPassword: []byte(userConfig.PasswordHash),
		})
	}
	return repo
}

func (repo *ConfigAuthrep) Get(needle *domain.User) (bool, error) {
	for _, user := range repo.authdb {
		if user.Name == needle.Name {
			*needle = *user
			return true, nil
		}
	}
	return false, nil
}
