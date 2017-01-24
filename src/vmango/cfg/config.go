package cfg

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/hcl"
	"io/ioutil"
	"os"
	"path/filepath"
)

type HypervisorConfig struct {
	Url              string   `hcl:"url"`
	ImageStoragePool string   `hcl:"image_storage_pool"`
	RootStoragePool  string   `hcl:"root_storage_pool"`
	Network          string   `hcl:"network"`
	VmTemplate       string   `hcl:"vm_template"`
	VolTemplate      string   `hcl:"volume_template"`
	IgnoreVms        []string `hcl:"ignore_vms"`
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

type AuthUserConfig struct {
	Username     string `hcl:",key"`
	PasswordHash string `hcl:"password"`
}

type Config struct {
	Listen        string `hcl:"listen"`
	SessionSecret string `hcl:"session_secret"`
	StaticCache   string `hcl:"static_cache"`
	Debug         bool   `hcl:"debug"`
	SSLKey        string `hcl:"ssl_key"`
	SSLCert       string `hcl:"ssl_cert"`

	Hypervisor HypervisorConfig `hcl:"hypervisor"`
	SSHKeys    []SSHKeyConfig   `hcl:"ssh_key"`
	Plans      []PlanConfig     `hcl:"plan"`
	Users      []AuthUserConfig `hcl:"user"`
}

func ResolveFilename(root string, filename string) string {
	resolved := filename
	if !filepath.IsAbs(filename) {
		resolved = filepath.Join(root, filename)
	}
	return resolved
}

func FileAvailaible(filename string) error {
	if _, err := os.Stat(filename); err != nil {
		return err
	}
	return nil
}

func (config *Config) Sanitize(root string) error {
	errors := &multierror.Error{}
	if config.Hypervisor.VmTemplate == "" {
		errors = multierror.Append(errors, fmt.Errorf("hypervisor.vm_template required"))
	}
	if config.Hypervisor.ImageStoragePool == "" {
		errors = multierror.Append(errors, fmt.Errorf("hypervisor.image_storage_pool required"))
	}
	if config.Hypervisor.RootStoragePool == "" {
		errors = multierror.Append(errors, fmt.Errorf("hypervisor.root_storage_pool required"))
	}
	if config.Hypervisor.Network == "" {
		errors = multierror.Append(errors, fmt.Errorf("hypervisor.network required"))
	}
	if config.Hypervisor.VmTemplate == "" {
		errors = multierror.Append(errors, fmt.Errorf("hypervisor.vm_template required"))
	}
	if config.Hypervisor.VolTemplate == "" {
		errors = multierror.Append(errors, fmt.Errorf("hypervisor.volume_template required"))
	}

	config.Hypervisor.VmTemplate = ResolveFilename(root, config.Hypervisor.VmTemplate)
	if err := FileAvailaible(config.Hypervisor.VmTemplate); err != nil {
		errors = multierror.Append(errors, fmt.Errorf("failed to stat hypervisor.vm_template: %s", err))
	}
	config.Hypervisor.VolTemplate = ResolveFilename(root, config.Hypervisor.VolTemplate)
	if err := FileAvailaible(config.Hypervisor.VolTemplate); err != nil {
		errors = multierror.Append(errors, fmt.Errorf("failed to stat hypervisor.volume_template: %s", err))
	}

	if config.SessionSecret == "" {
		errors = multierror.Append(errors, fmt.Errorf("session_secret required"))
	}

	if config.SSLKey != "" {
		config.SSLKey = ResolveFilename(root, config.SSLKey)
		if err := FileAvailaible(config.SSLKey); err != nil {
			errors = multierror.Append(errors, fmt.Errorf("failed to stat ssl_key: %s", err))
		}
	}
	if config.SSLCert != "" {
		config.SSLCert = ResolveFilename(root, config.SSLCert)
		if err := FileAvailaible(config.SSLCert); err != nil {
			errors = multierror.Append(errors, fmt.Errorf("failed to stat ssl_key: %s", err))
		}
	}
	return errors.ErrorOrNil()
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
