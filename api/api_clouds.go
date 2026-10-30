package api

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"subuk/vmango/compute"
)

type apiCloudImage struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Arch               string `json:"arch"`
	MinimumMemoryBytes uint64 `json:"minimum_memory_bytes"`
	MinimumDiskBytes   uint64 `json:"minimum_disk_bytes"`
}
type apiPlacementCapabilities struct {
	Attributes []string `json:"attributes" nullable:"false"`
	Modes      []string `json:"modes" nullable:"false"`
	Operators  []string `json:"operators" nullable:"false"`
}
type apiCloud struct {
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
type apiCloudInput struct {
	Cloud string `path:"cloud"`
}

type apiCloudVolumesInput struct {
	Cloud string `path:"cloud"`
	Usage string `query:"usage" enum:"attachable"`
}
type apiCloudCreateInput struct {
	SubmissionInput
	Cloud string `path:"cloud"`
	Body  compute.MachineCreateSpec
}
type apiCloudVolumeInput struct {
	SubmissionInput
	Cloud string `path:"cloud"`
	Body  compute.CloudVolumeSpec
}
type apiCloudMachineInput struct {
	SubmissionInput
	Cloud string `path:"cloud"`
	Node  string `path:"node"`
	VM    string `path:"vm"`
}
type apiChangeTypeInput struct {
	SubmissionInput
	Cloud string `path:"cloud"`
	Node  string `path:"node"`
	VM    string `path:"vm"`
	Body  struct {
		InstanceType string `json:"instance_type" minLength:"1"`
	}
}
type apiHostChoicesInput struct {
	Node string `path:"node"`
}
type apiHostChoices struct {
	Cloud          string                 `json:"cloud"`
	Zone           string                 `json:"zone"`
	Networks       []compute.CatalogEntry `json:"networks" nullable:"false"`
	StorageClasses []compute.CatalogEntry `json:"storage_classes" nullable:"false"`
}

func (e *Environ) cloudDTO(c compute.Cloud) (apiCloud, error) {
	d := apiCloud{ID: c.ID, Name: c.Name, Zones: c.Zones, InstanceTypes: c.InstanceTypes, Networks: c.Networks, StorageClasses: c.StorageClasses, Images: []apiCloudImage{}, Placement: apiPlacementCapabilities{Attributes: c.PlacementAttributes(), Modes: []string{"required", "preferred"}, Operators: []string{"equals"}}}
	for _, id := range c.Images {
		i, err := e.images.Get(id)
		if err != nil {
			return d, err
		}
		d.Images = append(d.Images, apiCloudImage{ID: i.ID, Name: i.Name, Arch: i.Arch.String(), MinimumMemoryBytes: i.MinimumMemoryBytes, MinimumDiskBytes: i.MinimumDiskBytes})
	}
	var err error
	d.StorageAvailability, err = e.clouds.StorageAvailability(c)
	if err != nil {
		return d, err
	}
	d.Offers, err = e.clouds.Offers(c)
	return d, err
}
func (e *Environ) registerClouds(api huma.API) {
	registerAPI(api, "GET", "/clouds/{cloud}/volumes", "list-cloud-volumes", "List cloud volumes, optionally excluding attached disks and image sources", func(ctx context.Context, in *apiCloudVolumesInput) (*apiResult[[]apiVolume], error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		volumes, err := e.clouds.ListVolumes(in.Cloud, in.Usage == "attachable")
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiVolume{}
		for _, v := range volumes {
			if !v.Metadata.Hidden {
				out = append(out, e.volumeDTO(v))
			}
		}
		return &apiResult[[]apiVolume]{Body: out}, nil
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
	registerAPI(api, "GET", "/clouds/{cloud}/machines", "list-cloud-machines", "List managed and unmanaged machines in a cloud", func(ctx context.Context, in *apiCloudInput) (*apiResult[[]apiVM], error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		c, err := e.clouds.Get(in.Cloud)
		if err != nil {
			return nil, apiError(err)
		}
		nodes := []string{}
		for _, h := range c.Hosts {
			nodes = append(nodes, h.Node)
		}
		vms, err := e.vms.List(compute.VirtualMachineListOptions{NodeIds: nodes, Strict: true})
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiVM{}
		for _, vm := range vms {
			out = append(out, e.machineDTO(vm))
		}
		return &apiResult[[]apiVM]{Body: out}, nil
	})
	registerAPI(api, "POST", "/clouds/{cloud}/machines", "create-cloud-machine", "Queue cloud inventory, automatic placement and machine provisioning", func(ctx context.Context, in *apiCloudCreateInput) (*apiAccepted, error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		for n := range in.Body.ExistingDisks {
			path, err := volumePath(in.Body.ExistingDisks[n].Path)
			if err != nil {
				return nil, apiError(err)
			}
			in.Body.ExistingDisks[n].Path = path
		}
		return e.submitTask(ctx, in.IdempotencyKey, "machine.create", compute.InfrastructureCommand{CloudID: in.Cloud, CreateSpec: &in.Body})
	})
	registerAPI(api, "POST", "/clouds/{cloud}/volumes", "create-cloud-volume", "Queue a host-bound volume using a storage class", func(ctx context.Context, in *apiCloudVolumeInput) (*apiAccepted, error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		return e.submitTask(ctx, in.IdempotencyKey, "volume.create", compute.InfrastructureCommand{CloudID: in.Cloud, VolumeSpec: &in.Body})
	})
	registerAPI(api, "POST", "/clouds/{cloud}/machines/{node}/{vm}/adopt", "adopt-machine", "Explicitly adopt a machine without claiming its existing disks", func(ctx context.Context, in *apiCloudMachineInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.adopt", compute.InfrastructureCommand{CloudID: in.Cloud, Node: in.Node, VM: in.VM})
	})
	registerAPI(api, "POST", "/clouds/{cloud}/machines/{node}/{vm}/instance-type", "change-instance-type", "Change a stopped machine's instance type on its current host", func(ctx context.Context, in *apiChangeTypeInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.change-type", compute.InfrastructureCommand{CloudID: in.Cloud, Node: in.Node, VM: in.VM, InstanceType: in.Body.InstanceType})
	})

	type createDiskInput struct {
		SubmissionInput
		Cloud string `path:"cloud"`
		Node  string `path:"node"`
		VM    string `path:"vm"`
		Body  compute.DataDiskSpec
	}
	registerAPI(api, "POST", "/clouds/{cloud}/machines/{node}/{vm}/disks", "create-machine-disk", "Create and attach a local disk through a storage class", func(ctx context.Context, in *createDiskInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.create-disk", compute.InfrastructureCommand{CloudID: in.Cloud, Node: in.Node, VM: in.VM, NewDisk: &in.Body})
	})
	registerAPI(api, "GET", "/nodes/{node}/catalog", "get-host-catalog", "Discover logical storage classes and networks supported on a host", func(ctx context.Context, in *apiHostChoicesInput) (*apiResult[apiHostChoices], error) {
		if e.clouds == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		c, h, err := e.clouds.ForNode(in.Node)
		if err != nil {
			return nil, apiError(err)
		}
		out := apiHostChoices{Cloud: c.ID, Zone: h.Zone, Networks: []compute.CatalogEntry{}, StorageClasses: []compute.CatalogEntry{}}
		for _, n := range c.Networks {
			if b, ok := h.Networks[n.ID]; ok {
				if e.vms.ValidateNetwork(in.Node, &compute.VirtualMachineAttachedInterface{NetworkName: b.Network, Bridge: b.Bridge, Model: b.Model, AccessVlan: b.VLAN}) == nil {
					out.Networks = append(out.Networks, n)
				}
			}
		}
		pools, err := e.volpools.List(compute.VolumePoolListOptions{NodeIds: []string{in.Node}})
		if err != nil {
			return nil, apiError(err)
		}
		availablePools := map[string]bool{}
		for _, p := range pools {
			availablePools[p.Name] = true
		}
		for _, s := range c.StorageClasses {
			if b, ok := h.Storage[s.ID]; ok && availablePools[b.Pool] {
				out.StorageClasses = append(out.StorageClasses, s)
			}
		}
		return &apiResult[apiHostChoices]{Body: out}, nil
	})
}
