package dal

import (
	"vmango/models"
)

type Provider interface {
	Name() string
	Machines() Machinerep
	Images() Imagerep
	Status(*models.StatusInfo) error
}

type Providers map[string]Provider

func (providers Providers) Add(p Provider) {
	providers[p.Name()] = p
}

func (providers Providers) Get(name string) Provider {
	return providers[name]
}
