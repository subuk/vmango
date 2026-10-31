package compute

import (
	"context"
	"encoding/json"
	"sort"
)

// Administration models contain only the allowlisted, accepted configuration.
// They deliberately do not require a node repository or a libvirt connection.
type AdministrationCapabilities struct {
	HostInventory     bool `json:"host_inventory"`
	ImageAvailability bool `json:"image_availability"`
}
type AdministrationInstallation struct {
	SnapshotID     string                  `json:"snapshot_id"`
	LoadedAt       string                  `json:"loaded_at"`
	Version        string                  `json:"version"`
	Activation     string                  `json:"activation"`
	LogLevel       string                  `json:"log_level"`
	Tasks          AdministrationTasks     `json:"tasks"`
	ImageTransfers AdministrationTransfers `json:"image_transfers"`
}
type AdministrationTasks struct {
	Workers       int `json:"workers"`
	RetentionDays int `json:"retention_days"`
}
type AdministrationTransfers struct {
	MaxBytes           uint64 `json:"max_bytes"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	IdleTimeoutSeconds int    `json:"idle_timeout_seconds"`
}
type AdministrationHostReference struct {
	ID      string `json:"id"`
	CloudID string `json:"cloud_id"`
	Zone    string `json:"zone"`
}
type AdministrationLibvirtCloud struct {
	Hosts []AdministrationHostReference `json:"hosts" nullable:"false"`
}
type AdministrationCloud struct {
	ID             string                      `json:"id"`
	Name           string                      `json:"name"`
	BackendKind    string                      `json:"backend_kind"`
	Capabilities   AdministrationCapabilities  `json:"capabilities"`
	Zones          []CatalogEntry              `json:"zones" nullable:"false"`
	InstanceTypes  []InstanceType              `json:"instance_types" nullable:"false"`
	Networks       []CatalogEntry              `json:"networks" nullable:"false"`
	StorageClasses []CatalogEntry              `json:"storage_classes" nullable:"false"`
	Images         []string                    `json:"images" nullable:"false"`
	Libvirt        *AdministrationLibvirtCloud `json:"libvirt,omitempty"`
}
type AdministrationSetting struct {
	Value  string `json:"value"`
	State  string `json:"state" enum:"configured,defaulted,inherited,unresolved"`
	Source string `json:"source"`
}
type AdministrationFieldState struct {
	Field  string `json:"field"`
	State  string `json:"state" enum:"configured,defaulted,inherited,unresolved"`
	Source string `json:"source"`
}
type AdministrationNetworkBinding struct {
	ID      string `json:"id"`
	Kind    string `json:"kind" enum:"network,bridge"`
	Backend string `json:"backend"`
	Model   string `json:"model"`
	VLAN    uint   `json:"vlan"`
}
type AdministrationStorageBinding struct {
	ID     string `json:"id"`
	Pool   string `json:"pool"`
	Format string `json:"format"`
}
type AdministrationHost struct {
	ID                   string                         `json:"id"`
	CloudID              string                         `json:"cloud_id"`
	Zone                 string                         `json:"zone"`
	Labels               map[string]string              `json:"labels"`
	CPUOvercommit        int                            `json:"cpu_overcommit"`
	CPUReserve           int                            `json:"cpu_reserve"`
	MemoryReserveBytes   uint64                         `json:"memory_reserve_bytes"`
	Emulator             AdministrationSetting          `json:"emulator"`
	ImagePool            string                         `json:"image_pool"`
	ConfigDrivePool      string                         `json:"config_drive_pool"`
	ConfigDriveSuffix    string                         `json:"config_drive_suffix"`
	ConfigDriveFormat    string                         `json:"config_drive_format"`
	GraphicsListen       string                         `json:"graphics_listen"`
	Cache                bool                           `json:"cache"`
	QcowPreallocMetadata bool                           `json:"qcow_prealloc_metadata"`
	Networks             []AdministrationNetworkBinding `json:"networks" nullable:"false"`
	Storage              []AdministrationStorageBinding `json:"storage" nullable:"false"`
}
type AdministrationRemoteSource struct {
	Scheme   string `json:"scheme"`
	Host     string `json:"host"`
	Port     string `json:"port"`
	Redacted bool   `json:"redacted"`
}
type AdministrationImageSource struct {
	Mode      string                      `json:"mode" enum:"local,download"`
	Path      string                      `json:"path,omitempty"`
	Remote    *AdministrationRemoteSource `json:"remote,omitempty"`
	SHA256    string                      `json:"sha256"`
	Format    string                      `json:"format"`
	SizeBytes uint64                      `json:"size_bytes"`
}
type AdministrationImageDefaults struct {
	Firmware       string `json:"firmware"`
	DiskBus        string `json:"disk_bus"`
	DiskController string `json:"disk_controller"`
	NICModel       string `json:"nic_model"`
	Graphics       string `json:"graphics"`
	VideoModel     string `json:"video_model"`
	GuestAgent     *bool  `json:"guest_agent"`
	Initialization string `json:"initialization"`
}
type AdministrationImage struct {
	ID                 string                      `json:"id"`
	CloudID            string                      `json:"cloud_id"`
	Name               string                      `json:"name"`
	OSName             string                      `json:"os_name"`
	OSVersion          string                      `json:"os_version"`
	Arch               string                      `json:"arch"`
	MinimumMemoryBytes uint64                      `json:"minimum_memory_bytes"`
	MinimumDiskBytes   uint64                      `json:"minimum_disk_bytes"`
	Protected          bool                        `json:"protected"`
	Hidden             bool                        `json:"hidden"`
	Defaults           AdministrationImageDefaults `json:"defaults"`
	Settings           []AdministrationFieldState  `json:"settings" nullable:"false"`
	Source             AdministrationImageSource   `json:"source"`
}
type AdministrationSnapshot struct {
	Installation AdministrationInstallation
	Clouds       []AdministrationCloud
	Hosts        []AdministrationHost
	Images       []AdministrationImage
}
type AdministrationUnavailableHost struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}
type AdministrationObservationError struct{ Diagnostic string }

func (e *AdministrationObservationError) Error() string { return e.Diagnostic }
func (e *AdministrationObservationError) Unwrap() error { return ErrUnavailable }

type AdministrationHostList struct {
	Items            []*Node
	UnavailableHosts []AdministrationUnavailableHost
}
type AdministrationImageHost struct {
	HostID        string `json:"host_id"`
	Observation   string `json:"observation" enum:"observed,unavailable,unknown"`
	Compatibility string `json:"compatibility" enum:"supported,unsupported,unknown"`
	SourceState   string `json:"source_state" enum:"ready,downloadable,preparing,failed,missing,hidden,attached,unknown"`
	Code          string `json:"code,omitempty"`
	VolumePath    string `json:"volume_path,omitempty"`
	Pool          string `json:"pool,omitempty"`
}
type AdministrationLibvirtImage struct {
	Hosts []AdministrationImageHost `json:"hosts" nullable:"false"`
}
type AdministrationImageAvailability struct {
	CloudID string                      `json:"cloud_id"`
	ImageID string                      `json:"image_id"`
	Libvirt *AdministrationLibvirtImage `json:"libvirt,omitempty"`
}

// AdministrationObservations is optional. Common configuration is fully usable
// without an implementation, including providers with no host inventory.
type AdministrationObservations interface {
	Host(context.Context, string, string) (*Node, error)
	Networks(context.Context, string, string) ([]*Network, error)
	Pools(context.Context, string, string) ([]*VolumePool, error)
	Capabilities(context.Context, string, string, Arch) (VirtualMachineCapabilities, error)
	Catalog(context.Context, string, string) (HostCatalog, error)
	ImageAvailability(context.Context, string, string, []AdministrationHostReference) (AdministrationImageAvailability, error)
}
type AdministrationService struct {
	snapshot     AdministrationSnapshot
	observations map[string]AdministrationObservations
}

func administrationCopy[T any](v T) T {
	b, _ := json.Marshal(v)
	var result T
	_ = json.Unmarshal(b, &result)
	return result
}
func NewAdministrationService(snapshot AdministrationSnapshot, observations map[string]AdministrationObservations) *AdministrationService {
	snapshot = administrationCopy(snapshot)
	if snapshot.Clouds == nil {
		snapshot.Clouds = []AdministrationCloud{}
	}
	if snapshot.Hosts == nil {
		snapshot.Hosts = []AdministrationHost{}
	}
	if snapshot.Images == nil {
		snapshot.Images = []AdministrationImage{}
	}
	for n := range snapshot.Clouds {
		c := &snapshot.Clouds[n]
		if c.Zones == nil {
			c.Zones = []CatalogEntry{}
		}
		if c.InstanceTypes == nil {
			c.InstanceTypes = []InstanceType{}
		}
		if c.Networks == nil {
			c.Networks = []CatalogEntry{}
		}
		if c.StorageClasses == nil {
			c.StorageClasses = []CatalogEntry{}
		}
		if c.Images == nil {
			c.Images = []string{}
		}
		if c.Libvirt != nil && c.Libvirt.Hosts == nil {
			c.Libvirt.Hosts = []AdministrationHostReference{}
		}
	}
	for n := range snapshot.Hosts {
		h := &snapshot.Hosts[n]
		if h.Labels == nil {
			h.Labels = map[string]string{}
		}
		if h.Networks == nil {
			h.Networks = []AdministrationNetworkBinding{}
		}
		if h.Storage == nil {
			h.Storage = []AdministrationStorageBinding{}
		}
	}
	for n := range snapshot.Images {
		if snapshot.Images[n].Settings == nil {
			snapshot.Images[n].Settings = []AdministrationFieldState{}
		}
	}
	sort.Slice(snapshot.Clouds, func(i, j int) bool { return snapshot.Clouds[i].ID < snapshot.Clouds[j].ID })
	sort.Slice(snapshot.Hosts, func(i, j int) bool { return snapshot.Hosts[i].ID < snapshot.Hosts[j].ID })
	sort.Slice(snapshot.Images, func(i, j int) bool { return snapshot.Images[i].ID < snapshot.Images[j].ID })
	backends := map[string]AdministrationObservations{}
	for id, backend := range observations {
		backends[id] = backend
	}
	return &AdministrationService{snapshot: snapshot, observations: backends}
}
func (s *AdministrationService) Installation() AdministrationInstallation {
	return s.snapshot.Installation
}
func (s *AdministrationService) Clouds() []AdministrationCloud {
	return administrationCopy(s.snapshot.Clouds)
}
func (s *AdministrationService) Cloud(id string) (AdministrationCloud, error) {
	if s != nil {
		for _, c := range s.snapshot.Clouds {
			if c.ID == id {
				return administrationCopy(c), nil
			}
		}
	}
	return AdministrationCloud{}, ErrNotFound
}
func (s *AdministrationService) Hosts(cloud string) ([]AdministrationHostReference, error) {
	c, err := s.Cloud(cloud)
	if err != nil {
		return nil, err
	}
	if !c.Capabilities.HostInventory {
		return nil, ErrNotFound
	}
	out := []AdministrationHostReference{}
	for _, h := range s.snapshot.Hosts {
		if h.CloudID == cloud {
			out = append(out, AdministrationHostReference{h.ID, h.CloudID, h.Zone})
		}
	}
	return out, nil
}
func (s *AdministrationService) HostConfiguration(cloud, node string) (AdministrationHost, error) {
	if _, err := s.Hosts(cloud); err != nil {
		return AdministrationHost{}, err
	}
	for _, h := range s.snapshot.Hosts {
		if h.CloudID == cloud && h.ID == node {
			return administrationCopy(h), nil
		}
	}
	return AdministrationHost{}, ErrNotFound
}
func (s *AdministrationService) Images(cloud, arch string) ([]AdministrationImage, error) {
	if _, err := s.Cloud(cloud); err != nil {
		return nil, err
	}
	if arch != "" && arch != "x86_64" && arch != "aarch64" {
		return nil, ErrInvalid
	}
	out := []AdministrationImage{}
	for _, i := range s.snapshot.Images {
		if i.CloudID == cloud && (arch == "" || i.Arch == arch) {
			out = append(out, administrationCopy(i))
		}
	}
	return out, nil
}
func (s *AdministrationService) Image(cloud, id string) (AdministrationImage, error) {
	images, err := s.Images(cloud, "")
	if err != nil {
		return AdministrationImage{}, err
	}
	for _, i := range images {
		if i.ID == id {
			return i, nil
		}
	}
	return AdministrationImage{}, ErrNotFound
}
func (s *AdministrationService) hostBackend(cloud, node string) (AdministrationObservations, error) {
	if _, err := s.HostConfiguration(cloud, node); err != nil {
		return nil, err
	}
	b := s.observations[cloud]
	if b == nil {
		return nil, ErrUnavailable
	}
	return b, nil
}
func (s *AdministrationService) LiveHost(ctx context.Context, cloud, node string) (*Node, error) {
	b, err := s.hostBackend(cloud, node)
	if err != nil {
		return nil, err
	}
	return b.Host(ctx, cloud, node)
}
func (s *AdministrationService) LiveHosts(ctx context.Context, cloud string) (AdministrationHostList, error) {
	hosts, err := s.Hosts(cloud)
	if err != nil {
		return AdministrationHostList{}, err
	}
	out := AdministrationHostList{Items: []*Node{}, UnavailableHosts: []AdministrationUnavailableHost{}}
	for _, h := range hosts {
		n, err := s.LiveHost(ctx, cloud, h.ID)
		if err != nil {
			out.UnavailableHosts = append(out.UnavailableHosts, AdministrationUnavailableHost{h.ID, "host_unavailable"})
			continue
		}
		out.Items = append(out.Items, n)
	}
	return out, nil
}
func (s *AdministrationService) LiveNetworks(ctx context.Context, cloud, node string) ([]*Network, error) {
	b, err := s.hostBackend(cloud, node)
	if err != nil {
		return nil, err
	}
	return b.Networks(ctx, cloud, node)
}
func (s *AdministrationService) LivePools(ctx context.Context, cloud, node string) ([]*VolumePool, error) {
	b, err := s.hostBackend(cloud, node)
	if err != nil {
		return nil, err
	}
	return b.Pools(ctx, cloud, node)
}
func (s *AdministrationService) LiveCapabilities(ctx context.Context, cloud, node string, arch Arch) (VirtualMachineCapabilities, error) {
	b, err := s.hostBackend(cloud, node)
	if err != nil {
		return VirtualMachineCapabilities{}, err
	}
	return b.Capabilities(ctx, cloud, node, arch)
}
func (s *AdministrationService) LiveCatalog(ctx context.Context, cloud, node string) (HostCatalog, error) {
	b, err := s.hostBackend(cloud, node)
	if err != nil {
		return HostCatalog{}, err
	}
	return b.Catalog(ctx, cloud, node)
}
func (s *AdministrationService) ImageAvailability(ctx context.Context, cloud, image string) (AdministrationImageAvailability, error) {
	c, err := s.Cloud(cloud)
	if err != nil {
		return AdministrationImageAvailability{}, err
	}
	if !c.Capabilities.ImageAvailability {
		return AdministrationImageAvailability{}, ErrNotFound
	}
	if _, err = s.Image(cloud, image); err != nil {
		return AdministrationImageAvailability{}, err
	}
	b := s.observations[cloud]
	if b == nil {
		return AdministrationImageAvailability{}, ErrUnavailable
	}
	hosts := []AdministrationHostReference{}
	if c.Libvirt != nil {
		hosts = c.Libvirt.Hosts
	}
	return b.ImageAvailability(ctx, cloud, image, hosts)
}
