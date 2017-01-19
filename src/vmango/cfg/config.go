package cfg

import (
	"github.com/hashicorp/hcl"
	"io/ioutil"
)

type HypervisorConfig struct {
	Url              string `hcl:"url"`
	ImageStoragePool string `hcl:"image_storage_pool"`
	Network          string `hcl:"network"`
	VmTemplate       string `hcl:"vm_template"`
}

type SSHKeyConfig struct {
	Name   string `hcl:",key"`
	Public string `hcl:"public"`
}

type PlanConfig struct {
	Name     string `hcl:",key"`
	Memory   int    `hcl:"memory"`
	Cpus     int    `hcl:"cpus"`
	DiskSize int    `hcl:"disk_size"`
}

type Config struct {
	Listen       string `hcl:"listen"`
	TemplatePath string `hcl:"template_path"`
	StaticPath   string `hcl:"static_path"`
	DbPath       string `hcl:"db_path"`

	Hypervisor HypervisorConfig `hcl:"hypervisor"`
	SSHKeys    []SSHKeyConfig   `hcl:"ssh_key"`
	Plans      []PlanConfig     `hcl:"plan"`
}

func ParseConfig(filename string) (*Config, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	config := &Config{}
	if err := hcl.Unmarshal(content, config); err != nil {
		return nil, err
	}
	return config, nil
}
