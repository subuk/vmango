package config

import (
	"fmt"
	"io/ioutil"
	"subuk/vmango/configdrive"
	"subuk/vmango/util"

	"github.com/hashicorp/hcl"
	"github.com/imdario/mergo"
)

type UserWebConfig struct {
	Id             string `hcl:",key"`
	FullName       string `hcl:"full_name"`
	Email          string `hcl:"email"`
	HashedPassword string `hcl:"hashed_password"`
}

type WebConfigLink struct {
	Title  string `hcl:",key"`
	Active bool   `hcl:"active"`
	Url    string `hcl:"url"`
}

type WebConfig struct {
	Listen         string          `hcl:"listen"`
	Debug          bool            `hcl:"debug"`
	StaticVersion  string          `hcl:"static_version"`
	SessionSecret  string          `hcl:"session_secret"`
	SessionSecure  bool            `hcl:"session_secure"`
	SessionDomain  string          `hcl:"session_domain"`
	SessionMaxAge  int             `hcl:"session_max_age"`
	MediaUploadTmp string          `hcl:"media_upload_tmp"`
	Users          []UserWebConfig `hcl:"user"`
	Links          []WebConfigLink `hcl:"link"`
	LinksTitle     string          `hcl:"links_title"`
}

type ImageConfig struct {
	Path      string `hcl:",key"`
	OsName    string `hcl:"os_name"`
	OsVersion string `hcl:"os_version"`
	OsArch    string `hcl:"os_arch"`
	Protected bool   `hcl:"protected"`
	Hidden    bool   `hcl:"hidden"`
}

type SubscribeConfig struct {
	Event     string `hcl:",key"`
	Script    string `hcl:"script"`
	Mandatory bool   `hcl:"mandatory"`
}

type LibvirtConfig struct {
	Name                   string   `hcl:",key"`
	Uri                    string   `hcl:"uri"`
	ConfigDriveSuffix      string   `hcl:"config_drive_suffix"`
	ConfigDrivePool        string   `hcl:"config_drive_pool"`
	ConfigDriveWriteFormat string   `hcl:"config_drive_write_format"`
	Cache                  bool     `hcl:"cache"`
	HiddenVolumes          []string `hcl:"hidden_volumes"`
}

type Config struct {
	LogLevel   string            `hcl:"log_level"`
	Images     []ImageConfig     `hcl:"image"`
	Bridges    []string          `hcl:"bridges"`
	Libvirts   []LibvirtConfig   `hcl:"libvirt"`
	KeyFile    string            `hcl:"key_file"`
	ImageFile  string            `hcl:"image_file"`
	Web        WebConfig         `hcl:"web"`
	Subscribes []SubscribeConfig `hcl:"subscribe"`

	LegacyLibvirtUri                    string   `hcl:"libvirt_uri"`
	LegacyLibvirtConfigDriveSuffix      string   `hcl:"libvirt_config_drive_suffix"`
	LegacyLibvirtConfigDrivePool        string   `hcl:"libvirt_config_drive_pool"`
	LegacyLibvirtConfigDriveWriteFormat string   `hcl:"libvirt_config_drive_write_format"`
	LegacyBridges                       []string `hcl:"bridges"`
}

func Default() *Config {
	return &Config{
		LogLevel:  "info",
		ImageFile: "~/.vmango/images.json",
		KeyFile:   "~/.vmango/authorized_keys",
		Web: WebConfig{
			Listen:         ":8080",
			Debug:          false,
			SessionMaxAge:  12 * 60 * 60,
			MediaUploadTmp: "/tmp/",
		},
	}
}

const OLD_BRIDGES_WARNING = `=======

Please remove 'bridges' option from configuration file and create libvirt networks like this:

<network>
  <name>br0</name>
  <forward mode='bridge'/>
  <bridge name='br0'/>
</network>

=======`

func Parse(filename string) (*Config, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, util.NewError(err, "cannot read configuration file")
	}
	config := &Config{}
	if err := hcl.Unmarshal(content, config); err != nil {
		return nil, util.NewError(err, "invalid configuration format")
	}
	if err := mergo.Merge(config, Default()); err != nil {
		return nil, util.NewError(err, "cannot apply default configuration value")
	}
	if config.LegacyLibvirtUri != "" && len(config.Libvirts) == 0 {
		config.Libvirts = append(config.Libvirts, LibvirtConfig{
			Name:                   "__legacy_config_format__",
			Uri:                    config.LegacyLibvirtUri,
			ConfigDriveSuffix:      config.LegacyLibvirtConfigDriveSuffix,
			ConfigDrivePool:        config.LegacyLibvirtConfigDrivePool,
			ConfigDriveWriteFormat: config.LegacyLibvirtConfigDriveWriteFormat,
		})
	}
	if len(config.LegacyBridges) > 0 {
		fmt.Println(OLD_BRIDGES_WARNING)
	}

	libvirt_ids := map[string]struct{}{}
	for index := range config.Libvirts {
		libvirt := &config.Libvirts[index]
		if _, exists := libvirt_ids[libvirt.Name]; exists {
			return nil, fmt.Errorf("duplicate libvirt connection '%s'", libvirt.Name)
		}
		libvirt_ids[libvirt.Name] = struct{}{}

		if libvirt.Uri == "" {
			return nil, fmt.Errorf("no uri specified for libvirt connection '%s'", libvirt.Name)
		}
		if libvirt.ConfigDriveWriteFormat == "" {
			libvirt.ConfigDriveWriteFormat = configdrive.FormatNoCloud.String()
		}
		if libvirt.ConfigDriveSuffix == "" {
			libvirt.ConfigDriveSuffix = "_config.iso"
		}
		if libvirt.ConfigDrivePool == "" {
			libvirt.ConfigDrivePool = "default"
		}
	}
	return config, nil
}
