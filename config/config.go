package config

import (
	"fmt"
	"io/ioutil"
	"math"
	"net/url"
	"os"
	"strings"
	"subuk/vmango/auth"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
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
	JWTSigningKeyFile          string          `hcl:"jwt_signing_key_file"`
	APIDisabled                bool            `hcl:"api_disabled"`
	ConsoleLinkLifetimeSeconds int             `hcl:"console_link_lifetime_seconds"`
	Listen                     string          `hcl:"listen"`
	TLSCertFile                string          `hcl:"tls_cert_file"`
	TLSKeyFile                 string          `hcl:"tls_key_file"`
	Debug                      bool            `hcl:"debug"`
	BaseUrl                    string          `hcl:"base_url"`
	JWTSigningKey              string          `hcl:"-"`
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

func (c *APIConfig) ValidateTLS() error {
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("api.tls_cert_file and api.tls_key_file must be configured together")
	}
	if c.TLSCertFile != "" && c.BaseUrl != "" {
		u, err := url.Parse(c.BaseUrl)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("api.base_url must be an absolute HTTPS URL when native TLS is enabled")
		}
	}
	return nil
}

type ImageConfig struct {
	HardwareProfile    string `hcl:"hardware_profile"`
	ID                 string `hcl:",key"`
	Path               string `hcl:"path"`
	URL                string `hcl:"url"`
	SHA256             string `hcl:"sha256"`
	SourceFormat       string `hcl:"source_format"`
	SourceSizeBytes    int64  `hcl:"source_size_bytes"`
	ArchiveFormat      string `hcl:"archive_format"`
	ArchiveMember      string `hcl:"archive_member"`
	ExtractedSizeBytes int64  `hcl:"extracted_size_bytes"`
	Name               string `hcl:"name"`
	OsName             string `hcl:"os_name"`
	OsVersion          string `hcl:"os_version"`
	OsArch             string `hcl:"os_arch"`
	Protected          bool   `hcl:"protected"`
	Hidden             bool   `hcl:"hidden"`
	Efi                *bool  `hcl:"efi"`
	Firmware           string `hcl:"firmware"`
	DiskBus            string `hcl:"disk_bus"`
	DiskController     string `hcl:"disk_controller"`
	NICModel           string `hcl:"nic_model"`
	Graphics           string `hcl:"graphics"`
	VideoModel         string `hcl:"video_model"`
	GuestAgent         *bool  `hcl:"guest_agent"`
	Initialization     string `hcl:"initialization"`
	MinimumMemoryMiB   int64  `hcl:"minimum_memory_mib"`
	MinimumDiskGiB     int64  `hcl:"minimum_disk_gib"`

	DataDisks []ImageDataDiskConfig `hcl:"data_disk"`
}

type ImageDataDiskConfig struct {
	Name    string `hcl:",key"`
	SizeGiB int64  `hcl:"size_gib"`
}

type VolumeConfig struct {
	Path      string `hcl:",key"`
	Protected bool   `hcl:"protected"`
	Hidden    bool   `hcl:"hidden"`
}

// ImageDefinitions normalizes cloud-owned definitions without backend calls.
// Image-derived source policies are scoped to the owning cloud's hosts.
func (c *Config) ImageDefinitions() ([]compute.Image, map[string]compute.VolumeMetadata, error) {
	profiles, err := c.HardwareProfiles()
	if err != nil {
		return nil, nil, err
	}
	images := []compute.Image{}
	policies := map[string]compute.VolumeMetadata{}
	mergePolicy := func(path string, protected, hidden bool) {
		old := policies[path]
		policies[path] = compute.VolumeMetadata{Protected: old.Protected || protected, Hidden: old.Hidden || hidden}
	}
	if len(c.Images) != 0 {
		return nil, nil, fmt.Errorf("top-level image blocks were removed: move each definition into its owning cloud and replace path labels with stable IDs plus path; policy-only blocks become volume blocks (see docs/CLOUDS.md)")
	}
	for _, cloud := range c.Clouds {
		seen := map[string]bool{}
		artifacts := map[string]compute.ImageDownload{}
		downloads := map[string]compute.ImageDownload{}
		for _, entry := range cloud.Images {
			if !compute.ValidCatalogID(entry.ID) {
				return nil, nil, fmt.Errorf("cloud %s image %q: use a stable catalog ID and a separate path (see docs/CLOUDS.md)", cloud.ID, entry.ID)
			}
			path := entry.Path
			managed := entry.URL != "" || entry.SHA256 != ""
			if !managed && (entry.SourceFormat != "" || entry.SourceSizeBytes != 0 || entry.ArchiveFormat != "" || entry.ArchiveMember != "" || entry.ExtractedSizeBytes != 0) {
				return nil, nil, fmt.Errorf("image %q: download and archive settings require url and sha256", entry.ID)
			}
			if !managed {
				for _, host := range cloud.Hosts {
					mergePolicy(compute.VolumePolicyKey(host.Node, path), entry.Protected, entry.Hidden)
				}
			}
			profile, ok := profiles[cloud.ID][entry.HardwareProfile]
			if entry.HardwareProfile == "" || !ok {
				return nil, nil, fmt.Errorf("cloud %s image %s: hardware_profile must reference an existing named profile; see %s", cloud.ID, entry.ID, HardwareMigrationGuide)
			}
			policy, err := hardware.Encode(profile)
			if err != nil {
				return nil, nil, err
			}
			if entry.MinimumMemoryMiB < 0 || entry.MinimumDiskGiB < 0 || entry.MinimumMemoryMiB > math.MaxUint64/(1<<20) || entry.MinimumDiskGiB > math.MaxUint64/(1<<30) {
				return nil, nil, fmt.Errorf("image %q: resource minimum overflows", entry.ID)
			}
			name := entry.Name
			if name == "" {
				name = entry.ID
			}
			i := compute.Image{CloudID: cloud.ID, ConfiguredPolicy: compute.VolumeMetadata{Protected: entry.Protected, Hidden: entry.Hidden}, ID: entry.ID, Name: name, OSName: entry.OsName, OSVersion: entry.OsVersion, Arch: compute.NewArch(entry.OsArch), SourcePath: path,
				BackendPolicy:      policy,
				MinimumMemoryBytes: uint64(entry.MinimumMemoryMiB) << 20, MinimumDiskBytes: uint64(entry.MinimumDiskGiB) << 30}
			for _, disk := range entry.DataDisks {
				if disk.SizeGiB <= 0 || disk.SizeGiB > math.MaxUint64/(1<<30) {
					return nil, nil, fmt.Errorf("image %q data disk %q: size_gib must be positive and fit in bytes", entry.ID, disk.Name)
				}
				i.DataDisks = append(i.DataDisks, compute.ImageDataDisk{Name: disk.Name, SizeBytes: uint64(disk.SizeGiB) << 30})
			}
			if entry.SourceSizeBytes < 0 || entry.ExtractedSizeBytes < 0 {
				return nil, nil, fmt.Errorf("image %q: source_size_bytes and extracted_size_bytes must be nonnegative", entry.ID)
			}
			if managed {
				i.SourcePath = entry.Path
				i.Download = &compute.ImageDownload{URL: entry.URL, SHA256: strings.ToLower(entry.SHA256), Format: compute.NewVolumeFormat(entry.SourceFormat), SizeBytes: uint64(entry.SourceSizeBytes), ArchiveFormat: entry.ArchiveFormat, ArchiveMember: entry.ArchiveMember, ExtractedSizeBytes: uint64(entry.ExtractedSizeBytes)}
			} else if entry.SourceFormat != "" || entry.SourceSizeBytes != 0 || entry.ArchiveFormat != "" || entry.ArchiveMember != "" || entry.ExtractedSizeBytes != 0 {
				return nil, nil, fmt.Errorf("image %q: download and archive settings require url and sha256", entry.ID)
			}
			if i.Managed() {
				d := *i.Download
				if previous, ok := downloads[d.SHA256]; ok {
					if previous.ArchiveFormat != d.ArchiveFormat || (previous.SizeBytes != 0 && d.SizeBytes != 0 && previous.SizeBytes != d.SizeBytes) {
						return nil, nil, fmt.Errorf("image %q: conflicting archive format or source byte size for shared download", i.ID)
					}
					if d.SizeBytes == 0 {
						d.SizeBytes = previous.SizeBytes
					}
				}
				downloads[d.SHA256] = d
				key := d.SHA256 + "\x00" + d.ArchiveFormat + "\x00" + d.ArchiveMember
				if previous, ok := artifacts[key]; ok {
					if previous.Format != d.Format || (previous.ExtractedSizeBytes != 0 && d.ExtractedSizeBytes != 0 && previous.ExtractedSizeBytes != d.ExtractedSizeBytes) {
						return nil, nil, fmt.Errorf("image %q: conflicting format or extracted byte size for shared artifact", i.ID)
					}
					if d.ExtractedSizeBytes == 0 {
						d.ExtractedSizeBytes = previous.ExtractedSizeBytes
					}
				}
				artifacts[key] = d
			}
			if err := i.Validate(); err != nil {
				return nil, nil, err
			}
			if seen[i.ID] {
				return nil, nil, fmt.Errorf("duplicate image identifier %q", i.ID)
			}
			seen[i.ID] = true
			images = append(images, i)
		}
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

type Config struct {
	MigrationWarnings     []string            `hcl:"-"`
	Namespaces            []NamespaceConfig   `hcl:"namespace"`
	NamespaceDirectory    string              `hcl:"namespace_directory"`
	Clouds                []CloudConfig       `hcl:"cloud"`
	ImageTransfers        ImageTransferConfig `hcl:"image_transfers"`
	Tasks                 TaskConfig          `hcl:"tasks"`
	LogLevel              string              `hcl:"log_level"`
	Images                []ImageConfig       `hcl:"image"`
	Volumes               []VolumeConfig      `hcl:"volume"`
	Bridges               []string            `hcl:"bridges"`
	KeyFile               string              `hcl:"key_file"`
	CloudInitTemplatesDir string              `hcl:"cloud_init_templates_dir"`
	API                   APIConfig           `hcl:"api"`
	Subscribes            []SubscribeConfig   `hcl:"subscribe"`

	LegacyBridges []string `hcl:"bridges"`
}

type ImageTransferConfig struct {
	MaxBytes           int64 `hcl:"max_bytes"`
	TimeoutSeconds     int   `hcl:"timeout_seconds"`
	IdleTimeoutSeconds int   `hcl:"idle_timeout_seconds"`
}

type TaskConfig struct {
	Directory     string `hcl:"directory"`
	Workers       int    `hcl:"workers"`
	RetentionDays int    `hcl:"retention_days"`
}

func Default() *Config {
	return &Config{
		NamespaceDirectory:    "~/.vmango/namespaces",
		ImageTransfers:        ImageTransferConfig{MaxBytes: 16 << 30, TimeoutSeconds: 3600, IdleTimeoutSeconds: 120},
		Tasks:                 TaskConfig{Directory: "~/.vmango/tasks", Workers: 2},
		LogLevel:              "info",
		KeyFile:               "~/.vmango/authorized_keys",
		CloudInitTemplatesDir: "~/.vmango/cloud-init-templates",
		API: APIConfig{
			JWTSigningKeyFile:          "~/.vmango/jwt_signing_key",
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
	config, err := ParseBytes(content)
	if err == nil {
		for _, warning := range config.MigrationWarnings {
			fmt.Fprintf(os.Stderr, "%s: warning: %s\n", filename, warning)
		}
	}
	if err == nil && len(config.LegacyBridges) > 0 {
		fmt.Println(OLD_BRIDGES_WARNING)
	}
	return config, err
}

// ParseBytes validates configuration without opening stores, generating keys or
// starting services. Callers can validate a candidate before publishing it.
func ParseBytes(content []byte) (*Config, error) {
	config := &Config{}
	parsed, err := hcl.ParseBytes(content)
	if err != nil {
		return nil, util.NewError(err, "invalid configuration format")
	}
	if list, ok := parsed.Node.(*ast.ObjectList); ok && len(list.Filter("web").Items) > 0 {
		return nil, fmt.Errorf("legacy web configuration is unsupported: migrate web { ... } to api { ... }, configure jwt_signing_key, session_dir and token_dir; remove api_disabled and legacy session settings")
	}
	if list, ok := parsed.Node.(*ast.ObjectList); ok {
		for _, removed := range []string{"libvirt", "libvirt_uri", "libvirt_config_drive_suffix", "libvirt_config_drive_pool", "libvirt_config_drive_write_format"} {
			if len(list.Filter(removed).Items) > 0 {
				return nil, fmt.Errorf(`top-level %s configuration is unsupported: move libvirt settings into the matching cloud { host "name" { uri = "..." } } block; preserve host names and URIs (see docs/CLOUDS.md)`, removed)
			}
		}
		for _, item := range list.Filter("api").Items {
			if obj, ok := item.Val.(*ast.ObjectType); ok {
				for _, removed := range []string{"jwt_signing_key", "console_signing_key", "console_signing_key_file", "session_secret", "session_domain", "session_max_age", "static_version", "media_upload_tmp"} {
					if len(obj.List.Filter(removed).Items) > 0 {
						return nil, fmt.Errorf("api.%s was removed; follow docs/UI-MIGRATION.md and configure JWT signing and credential stores", removed)
					}
				}
			}
		}
	}
	if list, ok := parsed.Node.(*ast.ObjectList); ok {
		if err := validatePolicyOwners(list, ""); err != nil {
			return nil, err
		}
	}
	if list, ok := parsed.Node.(*ast.ObjectList); ok {
		config.MigrationWarnings = removeDeprecatedHardware(list)
	}
	if err := hcl.DecodeObject(config, parsed); err != nil {
		return nil, util.NewError(err, "invalid configuration format")
	}
	if err := mergo.Merge(config, Default()); err != nil {
		return nil, util.NewError(err, "cannot apply default configuration value")
	}
	if err := config.API.ValidateTLS(); err != nil {
		return nil, err
	}
	for cloudIndex := range config.Clouds {
		cloud := &config.Clouds[cloudIndex]
		for hostIndex := range cloud.Hosts {
			host := &cloud.Hosts[hostIndex]
			if host.ImagePool == "" {
				host.ImagePool = "default"
			}
			if host.ConfigDriveSuffix == "" {
				host.ConfigDriveSuffix = "_config.iso"
			}
			if host.ConfigDrivePool == "" {
				host.ConfigDrivePool = "default"
			}
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
	volumeMetadata := util.ExpandHomeDir(config.Tasks.Directory) + "-volumes"
	if volumeMetadata == util.ExpandHomeDir(config.API.SessionDir) || volumeMetadata == util.ExpandHomeDir(config.API.TokenDir) {
		return nil, fmt.Errorf("volume metadata and credential stores must use separate directories")
	}
	imageMetadata := util.ExpandHomeDir(config.Tasks.Directory) + "-images"
	if imageMetadata == util.ExpandHomeDir(config.API.SessionDir) || imageMetadata == util.ExpandHomeDir(config.API.TokenDir) {
		return nil, fmt.Errorf("image metadata and credential stores must use separate directories")
	}
	if config.API.ConsoleLinkLifetimeSeconds < 1 || config.API.ConsoleLinkLifetimeSeconds > 86400*30 {
		return nil, fmt.Errorf("console_link_lifetime_seconds must be between 1 and 2592000")
	}
	if _, _, err := config.ImageDefinitions(); err != nil {
		return nil, err
	}
	if _, err := config.CloudDefinitions(); err != nil {
		return nil, err
	}
	if _, err := config.NamespaceDefinitions(); err != nil {
		return nil, err
	}
	if config.ImageTransfers.MaxBytes <= 0 || config.ImageTransfers.MaxBytes > 1<<40 || config.ImageTransfers.TimeoutSeconds < 1 || config.ImageTransfers.TimeoutSeconds > 86400 || config.ImageTransfers.IdleTimeoutSeconds < 1 || config.ImageTransfers.IdleTimeoutSeconds > config.ImageTransfers.TimeoutSeconds {
		return nil, fmt.Errorf("image_transfers needs max_bytes in 1..1TiB and timeout_seconds/idle_timeout_seconds in 1..86400, idle <= total")
	}
	return config, nil
}

// Policy options have exactly one configuration owner; misplaced fields must
// not be silently ignored by the permissive HCL decoder.
func validatePolicyOwners(list *ast.ObjectList, owner string) error {
	owners := map[string]string{
		"guest_agent": "hardware_profile", "graphics": "hardware_profile", "video_model": "hardware_profile",
		"firmware": "hardware_profile", "disk_bus": "hardware_profile", "disk_controller": "hardware_profile", "nic_model": "hardware_profile", "hugepages": "hardware_profile", "config_drive": "hardware_profile",
		"machine": "hardware_profile", "acceleration": "machine",
		"smbios": "hardware_profile", "clock": "hardware_profile", "timer": "hardware_profile",
		"autostart":       "instance_type",
		"clone_mode":      "cloud.storage_class",
		"graphics_listen": "host", "uri": "host", "image_pool": "host",
		"config_drive_suffix": "host", "config_drive_pool": "host",
		"config_drive_write_format": "host", "emulator": "host",
		"qcow_prealloc_metadata": "host", "cache": "host",
	}
	for _, item := range list.Items {
		if len(item.Keys) == 0 {
			continue
		}
		key, _ := item.Keys[0].Token.Value().(string)
		if owner == "hardware_profile" {
			allowed := map[string]bool{"name": true, "firmware": true, "disk_bus": true, "disk_controller": true, "nic_model": true, "graphics": true, "video_model": true, "guest_agent": true, "hugepages": true, "config_drive": true, "machine": true, "smbios": true, "clock": true, "timer": true, "cpu": true, "placement": true, "hyperv": true, "kvm": true}
			if !allowed[key] {
				return fmt.Errorf("hardware_profile: unsupported setting %s; see %s", key, HardwareMigrationGuide)
			}
		}
		if key == "cpu_request" {
			return fmt.Errorf("cpu_request was removed: remove this field; admission counts the full instance_type.cpu quantity (see %s)", HardwareMigrationGuide)
		}
		if owner == "hardware_profile" && (key == "cpu" || key == "placement" || key == "hyperv" || key == "kvm") {
			if len(list.Filter(key).Items) > 1 {
				return fmt.Errorf("hardware_profile.%s may be declared only once", key)
			}
			if err := validateCPUBlock(item, key); err != nil {
				return err
			}
		}
		if (key == "placement" || key == "hyperv" || key == "kvm" || key == "cpu" && owner != "instance_type") && owner != "hardware_profile" {
			return fmt.Errorf("%s belongs only in hardware_profile", key)
		}
		if owner == "hardware_profile" && (key == "smbios" || key == "clock" || key == "timer") {
			if key != "timer" && len(list.Filter(key).Items) > 1 {
				return fmt.Errorf("hardware_profile.%s may be declared only once", key)
			}
			if err := validateGuestSystemBlock(item, key); err != nil {
				return err
			}
		}
		if owner == "machine" {
			if key != "type" && key != "acceleration" {
				return fmt.Errorf("hardware_profile.machine: unsupported setting %s", key)
			}
			if len(list.Filter(key).Items) > 1 {
				return fmt.Errorf("hardware_profile.machine.%s may be declared only once", key)
			}
			if _, block := item.Val.(*ast.ObjectType); block {
				return fmt.Errorf("hardware_profile.machine.%s must be a string", key)
			}
		}
		if owner == "hardware_profile" && key == "machine" {
			if len(list.Filter("machine").Items) > 1 {
				return fmt.Errorf("hardware_profile may declare machine only once")
			}
			if _, block := item.Val.(*ast.ObjectType); !block || len(item.Keys) != 1 {
				return fmt.Errorf("hardware_profile.machine must be one unnamed block")
			}
		}
		if owner == "config_drive" && key != "enabled" && key != "format" && key != "device_type" && key != "bus" {
			return fmt.Errorf("hardware_profile.config_drive: unsupported setting %s; see %s", key, HardwareMigrationGuide)
		}
		if key == "hardware_profile" {
			_, block := item.Val.(*ast.ObjectType)
			if (owner == "cloud" && !block) || (owner == "image" && block) || (owner != "cloud" && owner != "image") {
				return fmt.Errorf("hardware_profile must be a named cloud block or an image reference; per-image overrides are unsupported")
			}
		}
		if owner == "hardware_profile" && key == "config_drive" && len(list.Filter("config_drive").Items) > 1 {
			return fmt.Errorf("hardware_profile may declare config_drive only once")
		}
		if key == "libvirt" {
			return fmt.Errorf("libvirt blocks are unsupported: put backend settings directly in cloud.host")
		}
		if key == "image" && owner != "cloud" {
			return fmt.Errorf("top-level image blocks were removed: move definitions into each owning cloud; use a stable ID plus path, and volume blocks for policy-only entries (see docs/CLOUDS.md)")
		}
		if key == "images" && owner == "cloud" {
			return fmt.Errorf("cloud.images references were removed: put nested image definitions inside the cloud (see docs/CLOUDS.md)")
		}
		if key == "host" && owner != "cloud" {
			return fmt.Errorf("host blocks belong only inside cloud configuration")
		}
		if required, exists := owners[key]; exists && owner != required && !((owner == "image" && (key == "guest_agent" || key == "graphics" || key == "video_model" || key == "firmware" || key == "disk_bus" || key == "disk_controller" || key == "nic_model")) || (owner == "instance_type" && key == "hugepages")) {
			return fmt.Errorf("%s belongs only in %s configuration", key, required)
		}
		if object, ok := item.Val.(*ast.ObjectType); ok {
			switch key {
			case "storage_class":
				if err := validatePolicyOwners(object.List, owner+".storage_class"); err != nil {
					return err
				}
			case "image", "cloud", "host", "instance_type", "hardware_profile", "config_drive", "machine":
				if err := validatePolicyOwners(object.List, key); err != nil {
					if len(item.Keys) > 1 {
						return fmt.Errorf("%s %v: %w", key, item.Keys[1].Token.Value(), err)
					}
					return err
				}
			}
		}
	}
	return nil
}
