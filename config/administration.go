package config

import (
	"net/url"
	"sort"
	"subuk/vmango/compute"
	"time"

	"github.com/google/uuid"
)

// AdministrationSnapshot projects accepted configuration once, after startup
// normalization. Neither the raw file nor live backend state is retained.
func (cfg *Config) AdministrationSnapshot(version string) (compute.AdministrationSnapshot, error) {
	clouds, err := cfg.CloudDefinitions()
	if err != nil {
		return compute.AdministrationSnapshot{}, err
	}
	images, _, err := cfg.ImageDefinitions()
	if err != nil {
		return compute.AdministrationSnapshot{}, err
	}
	s := compute.AdministrationSnapshot{
		Installation: compute.AdministrationInstallation{SnapshotID: uuid.NewString(), LoadedAt: time.Now().UTC().Format(time.RFC3339), Version: version, Activation: "restart", LogLevel: cfg.LogLevel, Tasks: compute.AdministrationTasks{Workers: cfg.Tasks.Workers, RetentionDays: cfg.Tasks.RetentionDays}, ImageTransfers: compute.AdministrationTransfers{MaxBytes: uint64(cfg.ImageTransfers.MaxBytes), TimeoutSeconds: cfg.ImageTransfers.TimeoutSeconds, IdleTimeoutSeconds: cfg.ImageTransfers.IdleTimeoutSeconds}},
		Clouds:       []compute.AdministrationCloud{}, Hosts: []compute.AdministrationHost{}, Images: []compute.AdministrationImage{},
	}
	hostConfig := map[string]CloudHostConfig{}
	for _, h := range cfg.Hosts() {
		hostConfig[h.Node] = h
	}
	for _, c := range clouds {
		d := compute.AdministrationCloud{ID: c.ID, Name: c.Name, BackendKind: "libvirt", Capabilities: compute.AdministrationCapabilities{HostInventory: true, ImageAvailability: true}, Zones: append([]compute.CatalogEntry{}, c.Zones...), InstanceTypes: append([]compute.InstanceType{}, c.InstanceTypes...), Networks: append([]compute.CatalogEntry{}, c.Networks...), StorageClasses: append([]compute.CatalogEntry{}, c.StorageClasses...), Images: append([]string{}, c.Images...), Libvirt: &compute.AdministrationLibvirtCloud{Hosts: []compute.AdministrationHostReference{}}}
		sort.Slice(d.Zones, func(i, j int) bool { return d.Zones[i].ID < d.Zones[j].ID })
		sort.Slice(d.InstanceTypes, func(i, j int) bool { return d.InstanceTypes[i].ID < d.InstanceTypes[j].ID })
		sort.Slice(d.Networks, func(i, j int) bool { return d.Networks[i].ID < d.Networks[j].ID })
		sort.Slice(d.StorageClasses, func(i, j int) bool { return d.StorageClasses[i].ID < d.StorageClasses[j].ID })
		sort.Strings(d.Images)
		for _, h := range c.Hosts {
			ref := compute.AdministrationHostReference{ID: h.Node, CloudID: c.ID, Zone: h.Zone}
			d.Libvirt.Hosts = append(d.Libvirt.Hosts, ref)
			conf := hostConfig[h.Node]
			emulator := compute.AdministrationSetting{Value: conf.Emulator, State: "configured", Source: "host.emulator"}
			if conf.Emulator == "" {
				emulator.State = "unresolved"
				emulator.Source = "backend"
			}
			labels := map[string]string{}
			for k, v := range h.Labels {
				labels[k] = v
			}
			out := compute.AdministrationHost{ID: h.Node, CloudID: c.ID, Zone: h.Zone, Labels: labels, CPUOvercommit: h.CPUOvercommit, CPUReserve: h.CPUReserve, MemoryReserveBytes: h.MemoryReserveBytes, Emulator: emulator, ImagePool: conf.ImagePool, ConfigDrivePool: conf.ConfigDrivePool, ConfigDriveSuffix: conf.ConfigDriveSuffix, ConfigDriveFormat: conf.ConfigDriveWriteFormat, GraphicsListen: conf.GraphicsListen, Cache: conf.Cache, QcowPreallocMetadata: conf.QcowPreallocMetadata, Networks: []compute.AdministrationNetworkBinding{}, Storage: []compute.AdministrationStorageBinding{}}
			for id, b := range h.Networks {
				kind := "network"
				if b.Bridge {
					kind = "bridge"
				}
				out.Networks = append(out.Networks, compute.AdministrationNetworkBinding{ID: id, Kind: kind, Backend: b.Network, Model: b.Model, VLAN: b.VLAN})
			}
			for id, b := range h.Storage {
				out.Storage = append(out.Storage, compute.AdministrationStorageBinding{ID: id, Pool: b.Pool, Format: b.Format.String()})
			}
			sort.Slice(out.Networks, func(i, j int) bool { return out.Networks[i].ID < out.Networks[j].ID })
			sort.Slice(out.Storage, func(i, j int) bool { return out.Storage[i].ID < out.Storage[j].ID })
			s.Hosts = append(s.Hosts, out)
		}
		sort.Slice(d.Libvirt.Hosts, func(i, j int) bool { return d.Libvirt.Hosts[i].ID < d.Libvirt.Hosts[j].ID })
		s.Clouds = append(s.Clouds, d)
	}
	for _, image := range images {
		s.Images = append(s.Images, administrationImage(image))
	}
	return s, nil
}

func administrationImage(i compute.Image) compute.AdministrationImage {
	d := i.Defaults
	graphics, video := d.Display()
	bus := d.BootDiskBus().String()
	guestAgent := d.GuestAgentEnabled()
	out := compute.AdministrationImage{ID: i.ID, CloudID: i.CloudID, Name: i.Name, OSName: i.OSName, OSVersion: i.OSVersion, Arch: i.Arch.String(), MinimumMemoryBytes: i.MinimumMemoryBytes, MinimumDiskBytes: i.MinimumDiskBytes, Protected: i.ConfiguredPolicy.Protected, Hidden: i.ConfiguredPolicy.Hidden, Defaults: compute.AdministrationImageDefaults{Firmware: d.Firmware, DiskBus: bus, DiskController: d.DiskController, NICModel: d.NICModel, Graphics: graphics.String(), VideoModel: video.String(), GuestAgent: &guestAgent, Initialization: d.Initialization}, Settings: []compute.AdministrationFieldState{}, Source: compute.AdministrationImageSource{Mode: "local", Path: i.SourcePath}}
	for _, field := range []struct{ name, value, source string }{{"firmware", d.Firmware, "target backend"}, {"disk_controller", d.DiskController, "target backend SCSI controller"}, {"nic_model", d.NICModel, "target network binding, then domain virtio default"}, {"initialization", d.Initialization, "target host config_drive_format"}} {
		state, source := "configured", "image."+field.name
		if field.value == "" {
			state, source = "inherited", field.source
		}
		out.Settings = append(out.Settings, compute.AdministrationFieldState{Field: field.name, State: state, Source: source})
	}
	busState, busSource := "configured", "image.disk_bus"
	if d.DiskBus == compute.DeviceBusUnknown {
		busState, busSource = "defaulted", "domain boot disk default"
	}
	out.Settings = append(out.Settings, compute.AdministrationFieldState{Field: "disk_bus", State: busState, Source: busSource})
	for _, field := range []struct{ name, value string }{{"graphics", d.Graphics}, {"video_model", d.VideoModel}} {
		state, source := "configured", "image."+field.name
		if field.value == "" {
			state, source = "defaulted", "domain display default"
		}
		out.Settings = append(out.Settings, compute.AdministrationFieldState{Field: field.name, State: state, Source: source})
	}
	state, source := "configured", "image.guest_agent"
	if d.GuestAgent == nil {
		state, source = "defaulted", "domain guest agent default"
	}
	out.Settings = append(out.Settings, compute.AdministrationFieldState{Field: "guest_agent", State: state, Source: source})
	if i.Download != nil {
		u, _ := url.Parse(i.Download.URL)
		remote := &compute.AdministrationRemoteSource{Redacted: true}
		if u != nil {
			remote.Scheme = u.Scheme
			remote.Host = u.Hostname()
			remote.Port = u.Port()
		}
		out.Source = compute.AdministrationImageSource{Mode: "download", Remote: remote, SHA256: i.Download.SHA256, Format: i.Download.Format.String(), SizeBytes: i.Download.SizeBytes}
	}
	return out
}
