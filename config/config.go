package config

import (
	"fmt"
	"io/ioutil"
	"math"
	"subuk/vmango/auth"
	"subuk/vmango/compute"
	"subuk/vmango/configdrive"
	"subuk/vmango/util"

	"dario.cat/mergo"
	"github.com/hashicorp/hcl"
	"github.com/hashicorp/hcl/hcl/ast"
)

type UserAPIConfig struct {
	Id             string `hcl:",key"`
	FullName       string `hcl:"full_name"`
	Email          string `hcl:"email"`
	HashedPassword string `hcl:"hashed_password"`
}

type APIConfigLink struct {
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

type APIConfig struct {
	APIDisabled                bool            `hcl:"api_disabled"`
	ConsoleSigningKey          string          `hcl:"console_signing_key"`
	ConsoleLinkLifetimeSeconds int             `hcl:"console_link_lifetime_seconds"`
	Listen                     string          `hcl:"listen"`
	Debug                      bool            `hcl:"debug"`
	BaseUrl                    string          `hcl:"base_url"`
	JWTSigningKey              string          `hcl:"jwt_signing_key"`
	SessionDir                 string          `hcl:"session_dir"`
	TokenDir                   string          `hcl:"token_dir"`
	SessionSecure              bool            `hcl:"session_secure"`
	Users                      []UserAPIConfig `hcl:"user"`
	Oidc                       OidcConfig      `hcl:"oidc"`
	Links                      []APIConfigLink `hcl:"link"`
	LinksTitle                 string          `hcl:"links_title"`
}

func (c *APIConfig) AuthPolicy() auth.IdentityPolicy {
	p := auth.IdentityPolicy{OIDCIssuer: c.Oidc.IssuerUrl, OIDCClientID: c.Oidc.ClientId, AllowedEmails: c.Oidc.AllowedEmails}
	for _, u := range c.Users {
		p.Users = append(p.Users, auth.LocalUser{ID: u.Id, Name: u.FullName, Email: u.Email, PasswordHash: u.HashedPassword})
	}
	return p
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
	Tasks      TaskConfig        `hcl:"tasks"`
	LogLevel   string            `hcl:"log_level"`
	Images     []ImageConfig     `hcl:"image"`
	Volumes    []VolumeConfig    `hcl:"volume"`
	Bridges    []string          `hcl:"bridges"`
	Libvirts   []LibvirtConfig   `hcl:"libvirt"`
	KeyFile    string            `hcl:"key_file"`
	API        APIConfig         `hcl:"api"`
	Subscribes []SubscribeConfig `hcl:"subscribe"`

	LegacyLibvirtUri                    string   `hcl:"libvirt_uri"`
	LegacyLibvirtConfigDriveSuffix      string   `hcl:"libvirt_config_drive_suffix"`
	LegacyLibvirtConfigDrivePool        string   `hcl:"libvirt_config_drive_pool"`
	LegacyLibvirtConfigDriveWriteFormat string   `hcl:"libvirt_config_drive_write_format"`
	LegacyBridges                       []string `hcl:"bridges"`
}

type TaskConfig struct {
	Directory     string `hcl:"directory"`
	Workers       int    `hcl:"workers"`
	RetentionDays int    `hcl:"retention_days"`
}

func Default() *Config {
	return &Config{
		Tasks:    TaskConfig{Directory: "~/.vmango/tasks", Workers: 2},
		LogLevel: "info",
		KeyFile:  "~/.vmango/authorized_keys",
		API: APIConfig{
			Listen:                     ":8080",
			Debug:                      false,
			SessionDir:                 "~/.vmango/sessions",
			TokenDir:                   "~/.vmango/tokens",
			ConsoleLinkLifetimeSeconds: 3600,
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
	parsed, err := hcl.ParseBytes(content)
	if err != nil {
		return nil, util.NewError(err, "invalid configuration format")
	}
	if list, ok := parsed.Node.(*ast.ObjectList); ok && len(list.Filter("web").Items) > 0 {
		return nil, fmt.Errorf("legacy web configuration is unsupported: migrate web { ... } to api { ... }, configure jwt_signing_key, session_dir and token_dir; remove api_disabled and legacy session settings")
	}
	if list, ok := parsed.Node.(*ast.ObjectList); ok {
		for _, item := range list.Filter("api").Items {
			if obj, ok := item.Val.(*ast.ObjectType); ok {
				for _, removed := range []string{"session_secret", "session_domain", "session_max_age", "static_version", "media_upload_tmp"} {
					if len(obj.List.Filter(removed).Items) > 0 {
						return nil, fmt.Errorf("api.%s was removed; follow docs/UI-MIGRATION.md and configure JWT signing and credential stores", removed)
					}
				}
			}
		}
	}
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
	if len(config.API.Oidc.Scopes) <= 0 {
		config.API.Oidc.Scopes = []string{"openid", "profile", "email"}
	}
	if config.API.APIDisabled {
		return nil, fmt.Errorf("api_disabled=true is unsupported: the new UI requires the API; explicitly resolve this setting during migration")
	}
	if config.API.JWTSigningKey != "" {
		if err := auth.ValidateKey(config.API.JWTSigningKey); err != nil {
			return nil, err
		}
	}
	if config.API.SessionDir == config.API.TokenDir {
		return nil, fmt.Errorf("api.session_dir and api.token_dir must be separate directories")
	}
	if config.Tasks.Workers < 1 || config.Tasks.Workers > 32 || config.Tasks.RetentionDays < 0 || config.Tasks.RetentionDays > 36500 {
		return nil, fmt.Errorf("tasks.workers must be 1..32 and retention_days must be 0..36500 (0 retains history indefinitely)")
	}
	if util.ExpandHomeDir(config.Tasks.Directory) == util.ExpandHomeDir(config.API.SessionDir) || util.ExpandHomeDir(config.Tasks.Directory) == util.ExpandHomeDir(config.API.TokenDir) {
		return nil, fmt.Errorf("task and credential stores must use separate directories")
	}
	if config.API.ConsoleLinkLifetimeSeconds < 1 || config.API.ConsoleLinkLifetimeSeconds > 86400*30 {
		return nil, fmt.Errorf("console_link_lifetime_seconds must be between 1 and 2592000")
	}
	if config.API.ConsoleSigningKey != "" && len(config.API.ConsoleSigningKey) < 32 {
		return nil, fmt.Errorf("console_signing_key must contain at least 32 random bytes")
	}
	if config.API.ConsoleSigningKey != "" {
		if err := auth.ValidateKey(config.API.ConsoleSigningKey); err != nil {
			return nil, fmt.Errorf("console_signing_key: %w", err)
		}
		if config.API.ConsoleSigningKey == config.API.JWTSigningKey {
			return nil, fmt.Errorf("console_signing_key must differ from jwt_signing_key")
		}
	}
	if _, _, err := config.ImageDefinitions(); err != nil {
		return nil, err
	}
	return config, nil
}
