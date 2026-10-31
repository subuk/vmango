package api

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"subuk/vmango/compute"
)

type apiCloudImage struct {
	ID                 string           `json:"id"`
	Name               string           `json:"name"`
	OSName             string           `json:"os_name"`
	OSVersion          string           `json:"os_version"`
	Defaults           apiImageDefaults `json:"defaults"`
	Arch               string           `json:"arch"`
	MinimumMemoryBytes uint64           `json:"minimum_memory_bytes"`
	MinimumDiskBytes   uint64           `json:"minimum_disk_bytes"`
}
type apiPlacementCapabilities struct {
	Attributes []string `json:"attributes" nullable:"false"`
	Modes      []string `json:"modes" nullable:"false"`
	Operators  []string `json:"operators" nullable:"false"`
}
type apiCloud struct {
	UnavailableHosts    []string                      `json:"unavailable_hosts" nullable:"false"`
	StorageAvailability []compute.StorageAvailability `json:"storage_availability" nullable:"false"`
	ID                  string                        `json:"id"`
	Name                string                        `json:"name"`
	Zones               []compute.CatalogEntry        `json:"zones" nullable:"false"`
	InstanceTypes       []compute.InstanceType        `json:"instance_types" nullable:"false"`
	Networks            []compute.CatalogEntry        `json:"networks" nullable:"false"`
	StorageClasses      []compute.CatalogEntry        `json:"storage_classes" nullable:"false"`
	Images              []apiCloudImage               `json:"images" nullable:"false"`
	Placement           apiPlacementCapabilities      `json:"placement"`
	Offers              []compute.CloudOffer          `json:"offers" nullable:"false"`
}
type apiMachineList struct {
	Items            []apiVM  `json:"items" nullable:"false"`
	UnavailableHosts []string `json:"unavailable_hosts" nullable:"false"`
}

type apiCloudInput struct {
	Cloud string `path:"cloud"`
}

type apiCloudVolumesInput struct {
	Cloud string `path:"cloud"`
	Host  string `query:"host" doc:"Filter by the observed owning host; this does not select placement."`
	Usage string `query:"usage" enum:"attachable"`
}
type apiCloudCreateInput struct {
	SubmissionInput
	Cloud string `path:"cloud"`
	Body  apiMachineCreate
}
type apiCloudVolumeInput struct {
	SubmissionInput
	Cloud string `path:"cloud"`
	Body  compute.CloudVolumeSpec
}
type apiCloudMachineInput struct {
	SubmissionInput
	Cloud string `path:"cloud"`
	VM    string `path:"vm"`
}
type apiChangeTypeInput struct {
	SubmissionInput
	Cloud string `path:"cloud"`
	VM    string `path:"vm"`
	Body  struct {
		InstanceType string `json:"instance_type" minLength:"1"`
	}
}

// apiMachineCreate is an input projection, deliberately separate from durable
// MachineCreateSpec so accepted tasks retain their existing host/path payloads.
type apiMachineCreate struct {
	Name          string                 `json:"name"`
	Image         string                 `json:"image"`
	InstanceType  string                 `json:"instance_type"`
	Zone          string                 `json:"zone"`
	Network       string                 `json:"network"`
	StorageClass  string                 `json:"storage_class"`
	Placement     compute.Placement      `json:"placement,omitempty"`
	Start         bool                   `json:"start"`
	KeyIDs        []string               `json:"keys,omitempty"`
	UserData      string                 `json:"user_data,omitempty"`
	Disks         []compute.DataDiskSpec `json:"new_disks,omitempty"`
	ExistingDisks []apiExistingDisk      `json:"existing_disks,omitempty"`
}
type apiExistingDisk struct {
	VolumeID string `json:"volume_id" minLength:"1"`
}

func (in apiMachineCreate) domain(cloudID string, clouds *compute.CloudService) (compute.MachineCreateSpec, error) {
	out := compute.MachineCreateSpec{Name: in.Name, Image: in.Image, InstanceType: in.InstanceType, Zone: in.Zone, Network: in.Network, StorageClass: in.StorageClass, Placement: in.Placement, Start: in.Start, KeyIDs: in.KeyIDs, UserData: in.UserData, Disks: in.Disks}
	for _, disk := range in.ExistingDisks {
		v, err := clouds.VolumeAddress(cloudID, disk.VolumeID)
		if err != nil {
			return out, err
		}
		out.ExistingDisks = append(out.ExistingDisks, compute.LocalDiskRef{Host: v.Host, Path: v.Path})
	}
	return out, nil
}

type apiVolumeList struct {
	Items            []apiVolume `json:"items" nullable:"false"`
	UnavailableHosts []string    `json:"unavailable_hosts" nullable:"false"`
}

func (e *Environ) cloudDTO(c compute.Cloud) (apiCloud, error) {
	d := apiCloud{ID: c.ID, Name: c.Name, Zones: c.Zones, InstanceTypes: c.InstanceTypes, Networks: c.Networks, StorageClasses: c.StorageClasses, Images: []apiCloudImage{}, Placement: apiPlacementCapabilities{Attributes: c.PlacementAttributes(), Modes: []string{"required", "preferred"}, Operators: []string{"equals"}}}
	for _, id := range c.Images {
		i, err := e.images.ForCloud(c.ID).Get(id)
		if err != nil {
			return d, err
		}
		bus := ""
		if i.Defaults.DiskBus != compute.DeviceBusUnknown {
			bus = i.Defaults.DiskBus.String()
		}
		graphics, video := i.Defaults.Display()
		d.Images = append(d.Images, apiCloudImage{ID: i.ID, Name: i.Name, OSName: i.OSName, OSVersion: i.OSVersion, Arch: i.Arch.String(), MinimumMemoryBytes: i.MinimumMemoryBytes, MinimumDiskBytes: i.MinimumDiskBytes, Defaults: apiImageDefaults{i.Defaults.Firmware, bus, i.Defaults.DiskController, i.Defaults.NICModel, graphics.String(), video.String(), i.Defaults.GuestAgent, i.Defaults.Initialization}})
	}
	d.Offers, d.StorageAvailability, d.UnavailableHosts = e.clouds.Discover(c)
	return d, nil
}
func (e *Environ) registerClouds(api huma.API) {
	registerAPI(api, "GET", "/clouds/{cloud}/volumes", "list-cloud-volumes", "List cloud volumes, optionally excluding attached disks and image sources", func(ctx context.Context, in *apiCloudVolumesInput) (*apiResult[apiVolumeList], error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		if in.Host != "" {
			if _, _, err := e.clouds.ForHost(in.Cloud, in.Host); err != nil {
				return nil, apiError(err)
			}
		}
		volumes, err := e.clouds.ListVolumes(in.Cloud, in.Usage == "attachable", in.Host)
		if err != nil {
			return nil, apiError(err)
		}
		out := apiVolumeList{Items: []apiVolume{}, UnavailableHosts: volumes.UnavailableHosts}
		for _, v := range volumes.Volumes {
			if !v.Metadata.Hidden && (in.Host == "" || v.NodeId == in.Host) {
				out.Items = append(out.Items, e.volumeDTO(v))
			}
		}
		return &apiResult[apiVolumeList]{Body: out}, nil
	})
	registerAPI(api, "GET", "/clouds", "list-clouds", "List configured cloud catalogs", func(ctx context.Context, in *apiEmpty) (*apiResult[[]apiCloud], error) {
		out := []apiCloud{}
		if e.clouds != nil {
			for _, c := range e.clouds.Definitions {
				d, err := e.cloudDTO(c)
				if err != nil {
					return nil, apiError(err)
				}
				out = append(out, d)
			}
		}
		return &apiResult[[]apiCloud]{Body: out}, nil
	})
	registerAPI(api, "GET", "/clouds/{cloud}", "get-cloud", "Discover a cloud catalog and placement capabilities", func(ctx context.Context, in *apiCloudInput) (*apiResult[apiCloud], error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		c, err := e.clouds.Get(in.Cloud)
		if err != nil {
			return nil, apiError(err)
		}
		d, err := e.cloudDTO(c)
		return &apiResult[apiCloud]{Body: d}, apiError(err)
	})
	registerAPI(api, "GET", "/clouds/{cloud}/machines", "list-cloud-machines", "List managed and unmanaged machines in a cloud", func(ctx context.Context, in *apiCloudInput) (*apiResult[apiMachineList], error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		c, err := e.clouds.Get(in.Cloud)
		if err != nil {
			return nil, apiError(err)
		}
		result, err := e.clouds.ListMachines(ctx, c.ID)
		if err != nil {
			return nil, apiError(err)
		}
		out := apiMachineList{Items: []apiVM{}, UnavailableHosts: result.UnavailableHosts}
		for _, vm := range result.Machines {
			out.Items = append(out.Items, e.machineDTO(vm))
		}
		return &apiResult[apiMachineList]{Body: out}, nil
	})
	registerAPI(api, "POST", "/clouds/{cloud}/machines", "create-cloud-machine", "Queue cloud inventory, automatic placement and machine provisioning", func(ctx context.Context, in *apiCloudCreateInput) (*apiAccepted, error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		spec, err := in.Body.domain(in.Cloud, e.clouds)
		if err != nil {
			return nil, apiError(err)
		}
		return e.submitTask(ctx, in.IdempotencyKey, "machine.create", compute.InfrastructureCommand{CloudID: in.Cloud, CreateSpec: &spec})
	})
	registerAPI(api, "POST", "/clouds/{cloud}/volumes", "create-cloud-volume", "Queue a host-bound volume using a storage class", func(ctx context.Context, in *apiCloudVolumeInput) (*apiAccepted, error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		return e.submitTask(ctx, in.IdempotencyKey, "volume.create", compute.InfrastructureCommand{CloudID: in.Cloud, VolumeSpec: &in.Body})
	})
	registerAPI(api, "POST", "/clouds/{cloud}/machines/{vm}/adopt", "adopt-machine", "Explicitly adopt a machine without claiming its existing disks", func(ctx context.Context, in *apiCloudMachineInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.adopt", compute.InfrastructureCommand{CloudID: in.Cloud, VM: in.VM})
	})
	registerAPI(api, "POST", "/clouds/{cloud}/machines/{vm}/instance-type", "change-instance-type", "Change a stopped machine's instance type on its current host", func(ctx context.Context, in *apiChangeTypeInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.change-type", compute.InfrastructureCommand{CloudID: in.Cloud, VM: in.VM, InstanceType: in.Body.InstanceType})
	})

	type createDiskInput struct {
		SubmissionInput
		Cloud string `path:"cloud"`
		VM    string `path:"vm"`
		Body  compute.DataDiskSpec
	}
	registerAPI(api, "POST", "/clouds/{cloud}/machines/{vm}/disks", "create-machine-disk", "Create and attach a local disk through a storage class", func(ctx context.Context, in *createDiskInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.create-disk", compute.InfrastructureCommand{CloudID: in.Cloud, VM: in.VM, NewDisk: &in.Body})
	})
}
