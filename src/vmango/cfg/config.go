package cfg

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
	"github.com/hashicorp/hcl"
)

type LXDImageServerConfig struct {
	Name   string   `hcl:"name"`
	Filter []string `hcl:"filter"`
}

type LXDConfig struct {
	Name       string `hcl:",key"`
	Url        string `hcl:"url"`
	ServerCert string `hcl:"server_cert"`
	Cert       string `hcl:"cert"`
	Key        string `hcl:"key"`
	Password   string `hcl:"password"`

	ImageServer LXDImageServerConfig `hcl:"image_server"`
}

type HypervisorConfig struct {
	Name             string   `hcl:",key"`
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
	Listen         string   `hcl:"listen"`
	SessionSecret  string   `hcl:"session_secret"`
	StaticCache    string   `hcl:"static_cache"`
	Debug          bool     `hcl:"debug"`
	SSLKey         string   `hcl:"ssl_key"`
	SSLCert        string   `hcl:"ssl_cert"`
	TrustedProxies []string `hcl:"trusted_proxies"`

	Hypervisors []HypervisorConfig `hcl:"hypervisor"`
	LXDServers  []LXDConfig        `hcl:"lxd"`
	SSHKeys     []SSHKeyConfig     `hcl:"ssh_key"`
	Plans       []PlanConfig       `hcl:"plan"`
	Users       []AuthUserConfig   `hcl:"user"`
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
	names := map[string]struct{}{}
	for index := range config.Hypervisors {
		hypervisor := &config.Hypervisors[index]
		if _, exist := names[hypervisor.Name]; exist {
			errors = multierror.Append(errors, fmt.Errorf("duplicated hypervisor name '%s'", hypervisor.Name))
		}
		names[hypervisor.Name] = struct{}{}
		if hypervisor.VmTemplate == "" {
			errors = multierror.Append(errors, fmt.Errorf("hypervisor.%s.vm_template required", hypervisor.Name))
		}
		if hypervisor.ImageStoragePool == "" {
			errors = multierror.Append(errors, fmt.Errorf("hypervisor.%s.image_storage_pool required", hypervisor.Name))
		}
		if hypervisor.RootStoragePool == "" {
			errors = multierror.Append(errors, fmt.Errorf("hypervisor.%s.root_storage_pool required", hypervisor.Name))
		}
		if hypervisor.Network == "" {
			errors = multierror.Append(errors, fmt.Errorf("hypervisor.%s.network required", hypervisor.Name))
		}
		if hypervisor.VmTemplate == "" {
			errors = multierror.Append(errors, fmt.Errorf("hypervisor.%s.vm_template required", hypervisor.Name))
		}
		if hypervisor.VolTemplate == "" {
			errors = multierror.Append(errors, fmt.Errorf("hypervisor.%s.volume_template required", hypervisor.Name))
		}

		hypervisor.VmTemplate = ResolveFilename(root, hypervisor.VmTemplate)
		if err := FileAvailaible(hypervisor.VmTemplate); err != nil {
			errors = multierror.Append(errors, fmt.Errorf("failed to stat hypervisor.%s.vm_template: %s", hypervisor.Name, err))
		}
		hypervisor.VolTemplate = ResolveFilename(root, hypervisor.VolTemplate)
		if err := FileAvailaible(hypervisor.VolTemplate); err != nil {
			errors = multierror.Append(errors, fmt.Errorf("failed to stat hypervisor.%s.volume_template: %s", hypervisor.Name, err))
		}
	}
	for index := range config.LXDServers {
		lxdServer := &config.LXDServers[index]
		if _, exists := names[lxdServer.Name]; exists {
			errors = multierror.Append(errors, fmt.Errorf("duplicated lxd server name '%s'", lxdServer.Name))
		}
		names[lxdServer.Name] = struct{}{}
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

func (cfg *Config) IsTLS() bool {
	return cfg.SSLKey != "" && cfg.SSLCert != ""
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
