package config

import (
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
}

type ImageConfig struct {
	Path      string `hcl:",key"`
	OsName    string `hcl:"os_name"`
	OsVersion string `hcl:"os_version"`
	OsArch    string `hcl:"os_arch"`
	Protected bool   `hcl:"protected"`
}

type SubscribeConfig struct {
	Event     string `hcl:",key"`
	Script    string `hcl:"script"`
	Mandatory bool   `hcl:"mandatory"`
}

type Config struct {
	LibvirtUri                    string            `hcl:"libvirt_uri"`
	LibvirtConfigDriveSuffix      string            `hcl:"libvirt_config_drive_suffix"`
	LibvirtConfigDrivePool        string            `hcl:"libvirt_config_drive_pool"`
	LibvirtConfigDriveWriteFormat string            `hcl:"libvirt_config_drive_write_format"`
	Images                        []ImageConfig     `hcl:"image"`
	Bridges                       []string          `hcl:"bridges"`
	KeyFile                       string            `hcl:"key_file"`
	Web                           WebConfig         `hcl:"web"`
	Subscribes                    []SubscribeConfig `hcl:"subscribe"`
}

func Default() *Config {
	return &Config{
		LibvirtUri:                    "qemu:///system",
		LibvirtConfigDrivePool:        "default",
		LibvirtConfigDriveSuffix:      "_config.iso",
		LibvirtConfigDriveWriteFormat: configdrive.FormatNoCloud.String(),
		KeyFile:                       "~/.vmango/authorized_keys",
		Web: WebConfig{
			Listen:         ":8080",
			Debug:          false,
			SessionMaxAge:  12 * 60 * 60,
			MediaUploadTmp: "/tmp/",
		},
	}
}

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
	return config, nil
}
