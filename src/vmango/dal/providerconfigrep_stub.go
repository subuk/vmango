package dal

import (
	"vmango/domain"
)

//
type StubProviderConfigrep struct {
	Factory    *StubProviderFactory
	ListIdsErr error
	GetErr     error
}

func (repo *StubProviderConfigrep) ListIds() ([]string, error) {
	ret := []string{}
	for name, _ := range repo.Factory.Providers {
		ret = append(ret, name)
	}
	return ret, repo.ListIdsErr
}

func (repo *StubProviderConfigrep) Get(id string) (*domain.ProviderConfig, error) {
	return &domain.ProviderConfig{Name: id}, repo.GetErr
}
