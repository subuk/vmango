package dal

import (
	"fmt"
	"vmango/domain"
)

const (
	LibvirtProvider = "libvirt"
)

func ProviderFactory(pc *domain.ProviderConfig) (*domain.Provider, error) {
	switch pc.Type {
	case LibvirtProvider:
		return LibvirtProviderFactory(pc)
	default:
		return nil, fmt.Errorf("unknown provider type")
	}
}
