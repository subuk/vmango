package config

import (
	"fmt"
	"subuk/vmango/compute"
)

type NamespaceConfig struct {
	ID          string   `hcl:",key"`
	Name        string   `hcl:"name"`
	Description string   `hcl:"description"`
	Clouds      []string `hcl:"clouds"`
}

func (cfg *Config) NamespaceDefinitions() ([]compute.Namespace, error) {
	clouds := map[string]bool{}
	for _, c := range cfg.Clouds {
		clouds[c.ID] = true
	}
	definitions := make([]compute.Namespace, 0, len(cfg.Namespaces))
	for _, n := range cfg.Namespaces {
		for _, cloud := range n.Clouds {
			if !clouds[cloud] {
				return nil, fmt.Errorf("namespace %q references unknown cloud %q", n.ID, cloud)
			}
		}
		definitions = append(definitions, compute.Namespace{ID: compute.NamespaceID(n.ID), Name: n.Name, Description: n.Description, Clouds: n.Clouds})
	}
	service, err := compute.NewNamespaceService(definitions)
	if err != nil {
		return nil, err
	}
	return service.List(), nil
}
