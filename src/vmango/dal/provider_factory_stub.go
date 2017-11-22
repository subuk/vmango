package dal

import (
	"fmt"
	"vmango/domain"
)

//
type StubProviderFactory struct {
	Configs   *StubProviderConfigrep
	Providers map[string]*domain.Provider
	Err       error
}

func NewStubProviderFactory() *StubProviderFactory {
	f := &StubProviderFactory{
		Providers: map[string]*domain.Provider{},
	}
	f.Configs = &StubProviderConfigrep{Factory: f}
	return f
}

func (f *StubProviderFactory) Add(provider *domain.Provider) {
	key := provider.Name
	// fmt.Println(f)
	f.Providers[key] = provider
}

func (f *StubProviderFactory) Produce(pc *domain.ProviderConfig) (*domain.Provider, error) {
	provider, exist := f.Providers[pc.Name]
	if !exist {
		return nil, fmt.Errorf("not found")
	}
	return provider, f.Err
}
