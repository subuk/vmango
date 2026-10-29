package config

import (
	"fmt"
	"io/ioutil"
	"math"
	"subuk/vmango/compute"
	"subuk/vmango/configdrive"
	"subuk/vmango/util"

	"dario.cat/mergo"
	"github.com/hashicorp/hcl"
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

type OidcConfig struct {
	Title         string   `hcl:"title"`
	IssuerUrl     string   `hcl:"issuer_url"`
	ClientId      string   `hcl:"client_id"`
	ClientSecret  string   `hcl:"client_secret"`
	Scopes        []string `hcl:"scopes"`
	AllowedEmails []string `hcl:"allowed_emails"`
}

type WebConfig struct {
	APIDisabled                bool            `hcl:"api_disabled"`
	ConsoleSigningKey          string          `hcl:"console_signing_key"`
	ConsoleLinkLifetimeSeconds int             `hcl:"console_link_lifetime_seconds"`
	Listen                     string          `hcl:"listen"`
	Debug                      bool            `hcl:"debug"`
	BaseUrl                    string          `hcl:"base_url"`
	StaticVersion              string          `hcl:"static_version"`
	SessionSecret              string          `hcl:"session_secret"`
	SessionSecure              bool            `hcl:"session_secure"`
	SessionDomain              string          `hcl:"session_domain"`
	SessionMaxAge              int             `hcl:"session_max_age"`
	MediaUploadTmp             string          `hcl:"media_upload_tmp"`
	Users                      []UserWebConfig `hcl:"user"`
	Oidc                       OidcConfig      `hcl:"oidc"`
	Links                      []WebConfigLink `hcl:"link"`
	LinksTitle                 string          `hcl:"links_title"`
}

type ImageConfig struct {
	ID               string `hcl:",key"`
	Path             string `hcl:"path"`
	Name             string `hcl:"name"`
	OsName           string `hcl:"os_name"`
	OsVersion        string `hcl:"os_version"`
	OsArch           string `hcl:"os_arch"`
	Protected        bool   `hcl:"protected"`
	Hidden           bool   `hcl:"hidden"`
	Efi              *bool  `hcl:"efi"`
	Firmware         string `hcl:"firmware"`
	DiskBus          string `hcl:"disk_bus"`
	DiskController   string `hcl:"disk_controller"`
	NICModel         string `hcl:"nic_model"`
	GuestAgent       *bool  `hcl:"guest_agent"`
	Initialization   string `hcl:"initialization"`
	MinimumMemoryMiB int64  `hcl:"minimum_memory_mib"`
	MinimumDiskGiB   int64  `hcl:"minimum_disk_gib"`
}

type VolumeConfig struct {
	Path      string `hcl:",key"`
	Protected bool   `hcl:"protected"`
	Hidden    bool   `hcl:"hidden"`
}

// ImageDefinitions converts legacy path labels and new catalog IDs without
// consulting a node. Policies remain properties of storage paths.
func (c *Config) ImageDefinitions() ([]compute.Image, map[string]compute.VolumeMetadata, error) {
	images := []compute.Image{}
	policies := map[string]compute.VolumeMetadata{}
	mergePolicy := func(path string, protected, hidden bool) {
		old := policies[path]
		policies[path] = compute.VolumeMetadata{Protected: old.Protected || protected, Hidden: old.Hidden || hidden}
	}
	seen := map[string]bool{}
	for _, entry := range c.Images {
		path := entry.Path
		if path == "" {
			path = entry.ID
		}
		mergePolicy(path, entry.Protected, entry.Hidden)
		// Legacy image blocks without OS metadata were ordinary volume policies.
		if entry.Path == "" && entry.OsName == "" {
			continue
		}
		firmware := entry.Firmware
		if entry.Efi != nil {
			legacy := "auto"
			if *entry.Efi {
				legacy = "efi"
			}
			if firmware != "" && firmware != legacy {
				return nil, nil, fmt.Errorf("image %q: conflicting efi and firmware", entry.ID)
			}
			firmware = legacy
		}
		bus := compute.NewDeviceBus(entry.DiskBus)
		if entry.DiskBus != "" && bus == compute.DeviceBusUnknown {
			return nil, nil, fmt.Errorf("image %q: invalid disk_bus", entry.ID)
		}
		if entry.MinimumMemoryMiB < 0 || entry.MinimumDiskGiB < 0 || entry.MinimumMemoryMiB > math.MaxUint64/(1<<20) || entry.MinimumDiskGiB > math.MaxUint64/(1<<30) {
			return nil, nil, fmt.Errorf("image %q: resource minimum overflows", entry.ID)
		}
		name := entry.Name
		if name == "" {
			name = entry.ID
		}
		i := compute.Image{ID: entry.ID, Name: name, OSName: entry.OsName, OSVersion: entry.OsVersion, Arch: compute.NewArch(entry.OsArch), SourcePath: path,
			Defaults:           compute.ImageDefaults{Firmware: firmware, DiskBus: bus, DiskController: entry.DiskController, NICModel: entry.NICModel, GuestAgent: entry.GuestAgent, Initialization: entry.Initialization},
			MinimumMemoryBytes: uint64(entry.MinimumMemoryMiB) << 20, MinimumDiskBytes: uint64(entry.MinimumDiskGiB) << 30}
		if err := i.Validate(); err != nil {
			return nil, nil, err
		}
		if seen[i.ID] {
			return nil, nil, fmt.Errorf("duplicate image identifier %q", i.ID)
		}
		seen[i.ID] = true
		images = append(images, i)
	}
	for _, v := range c.Volumes {
		mergePolicy(v.Path, v.Protected, v.Hidden)
	}
	return images, policies, nil
}

type SubscribeConfig struct {
	Event     string `hcl:",key"`
	Script    string `hcl:"script"`
	Mandatory bool   `hcl:"mandatory"`
}

type LibvirtConfig struct {
	Name                   string `hcl:",key"`
	Uri                    string `hcl:"uri"`
	ConfigDriveSuffix      string `hcl:"config_drive_suffix"`
	ConfigDrivePool        string `hcl:"config_drive_pool"`
	ConfigDriveWriteFormat string `hcl:"config_drive_write_format"`
	Emulator               string `hcl:"emulator"`
	QcowPreallocMetadata   bool   `hcl:"qcow_prealloc_metadata"`
	Cache                  bool   `hcl:"cache"`
}

type Config struct {
	LogLevel   string            `hcl:"log_level"`
	Images     []ImageConfig     `hcl:"image"`
	Volumes    []VolumeConfig    `hcl:"volume"`
	Bridges    []string          `hcl:"bridges"`
	Libvirts   []LibvirtConfig   `hcl:"libvirt"`
	KeyFile    string            `hcl:"key_file"`
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
		LogLevel: "info",
		KeyFile:  "~/.vmango/authorized_keys",
		Web: WebConfig{
			Listen:                     ":8080",
			Debug:                      false,
			SessionMaxAge:              12 * 60 * 60,
			ConsoleLinkLifetimeSeconds: 3600,
			MediaUploadTmp:             "/tmp/",
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
	if len(config.Web.Oidc.Scopes) <= 0 {
		config.Web.Oidc.Scopes = []string{"openid", "profile", "email"}
	}
	if config.Web.ConsoleLinkLifetimeSeconds < 1 || config.Web.ConsoleLinkLifetimeSeconds > 86400*30 {
		return nil, fmt.Errorf("console_link_lifetime_seconds must be between 1 and 2592000")
	}
	if config.Web.ConsoleSigningKey != "" && len(config.Web.ConsoleSigningKey) < 32 {
		return nil, fmt.Errorf("console_signing_key must contain at least 32 random bytes")
	}
	if _, _, err := config.ImageDefinitions(); err != nil {
		return nil, err
	}
	return config, nil
}
