package dal

import (
	"fmt"
	"io/ioutil"
	"strings"
	"vmango/cfg"
	"vmango/domain"
)

type ConfigProviderConfigrep struct {
	providerConfigs map[string]domain.ProviderConfig
	providerIds     []string
}

func NewConfigProviderConfigrep(hypervisors []cfg.HypervisorConfig) (*ConfigProviderConfigrep, error) {
	providerConfigs := map[string]domain.ProviderConfig{}
	providerIds := []string{}
	for _, hv := range hypervisors {
		var vmTemplateContent, volTemplateContent string
		if hv.VmTemplate != "" {
			content, err := ioutil.ReadFile(hv.VmTemplate)
			if err != nil {
				return nil, err
			}
			vmTemplateContent = string(content)
		}
		if hv.VolTemplate != "" {
			content, err := ioutil.ReadFile(hv.VolTemplate)
			if err != nil {
				return nil, err
			}
			volTemplateContent = string(content)
		}
		providerIds = append(providerIds, hv.Name)
		providerConfigs[hv.Name] = domain.ProviderConfig{
			Name: hv.Name,
			Type: "libvirt",
			Params: map[string]string{
				"url":                hv.Url,
				"machine_template":   vmTemplateContent,
				"volume_template":    volTemplateContent,
				"network":            hv.Network,
				"root_storage_pool":  hv.RootStoragePool,
				"image_storage_pool": hv.ImageStoragePool,
				"ignore_vms":         strings.Join(hv.IgnoreVms, ","),
			},
		}
	}
	return &ConfigProviderConfigrep{
		providerConfigs: providerConfigs,
		providerIds:     providerIds,
	}, nil
}

func (repo *ConfigProviderConfigrep) ListIds() ([]string, error) {
	return repo.providerIds, nil

}

func (repo *ConfigProviderConfigrep) Get(id string) (*domain.ProviderConfig, error) {
	if config, exist := repo.providerConfigs[id]; exist {
		return &config, nil
	}
	return nil, fmt.Errorf("not found")
}
