package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"subuk/vmango/compute"

	"github.com/danielgtaylor/huma/v2"
)

type apiAdministrationCloudInput struct {
	Cloud string `path:"cloud"`
}
type apiAdministrationHostInput struct {
	Cloud string `path:"cloud"`
	Node  string `path:"node"`
}
type apiAdministrationArchInput struct {
	Cloud string `path:"cloud"`
	Node  string `path:"node"`
	Arch  string `query:"arch" enum:"x86_64,aarch64"`
}
type apiAdministrationImagesInput struct {
	Cloud string `path:"cloud"`
	Arch  string `query:"arch" enum:"x86_64,aarch64"`
}
type apiAdministrationImageInput struct {
	Cloud string `path:"cloud"`
	Image string `path:"image"`
}
type apiAdministrationHostList struct {
	Items            []apiNode                               `json:"items" nullable:"false"`
	UnavailableHosts []compute.AdministrationUnavailableHost `json:"unavailable_hosts" nullable:"false"`
}

func administrationError(err error) error {
	var observation *compute.AdministrationObservationError
	if errors.As(err, &observation) {
		return &huma.ErrorModel{Type: "about:blank", Title: http.StatusText(503), Status: 503, Detail: observation.Diagnostic}
	}
	return apiError(err)
}
func (e *Environ) registerAdministration(api huma.API) {
	registerAPI(api, "GET", "/administration/configuration", "get-administration-configuration", "Read the accepted installation configuration", func(ctx context.Context, in *apiEmpty) (*apiResult[compute.AdministrationInstallation], error) {
		if e.administration == nil {
			return nil, apiError(compute.ErrNotFound)
		}
		return &apiResult[compute.AdministrationInstallation]{Body: e.administration.Installation()}, nil
	})
	registerAPI(api, "GET", "/administration/clouds", "list-administration-clouds", "List configured clouds without backend discovery", func(ctx context.Context, in *apiEmpty) (*apiResult[[]compute.AdministrationCloud], error) {
		out := []compute.AdministrationCloud{}
		if e.administration != nil {
			out = e.administration.Clouds()
		}
		return &apiResult[[]compute.AdministrationCloud]{Body: out}, nil
	})
	cloudDetail := func(ctx context.Context, in *apiAdministrationCloudInput) (*apiResult[compute.AdministrationCloud], error) {
		c, err := e.administration.Cloud(in.Cloud)
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[compute.AdministrationCloud]{Body: c}, nil
	}
	registerAPI(api, "GET", "/administration/clouds/{cloud}", "get-administration-cloud", "Read a configured cloud overview", cloudDetail)
	registerAPI(api, "GET", "/administration/clouds/{cloud}/configuration", "get-administration-cloud-configuration", "Read the full accepted cloud configuration", cloudDetail)
	registerAPI(api, "GET", "/administration/clouds/{cloud}/configuration/hosts", "list-administration-host-configuration", "List configured hosts without contacting them", func(ctx context.Context, in *apiAdministrationCloudInput) (*apiResult[[]compute.AdministrationHostReference], error) {
		h, err := e.administration.Hosts(in.Cloud)
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[[]compute.AdministrationHostReference]{Body: h}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/configuration/hosts/{node}", "get-administration-host-configuration", "Read accepted host settings and bindings", func(ctx context.Context, in *apiAdministrationHostInput) (*apiResult[compute.AdministrationHost], error) {
		h, err := e.administration.HostConfiguration(in.Cloud, in.Node)
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[compute.AdministrationHost]{Body: h}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/images", "list-administration-images", "List cloud-owned image definitions independently of source availability", func(ctx context.Context, in *apiAdministrationImagesInput) (*apiResult[[]compute.AdministrationImage], error) {
		i, err := e.administration.Images(in.Cloud, in.Arch)
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[[]compute.AdministrationImage]{Body: i}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/images/{image}", "get-administration-image", "Read a safe technical image definition", func(ctx context.Context, in *apiAdministrationImageInput) (*apiResult[compute.AdministrationImage], error) {
		i, err := e.administration.Image(in.Cloud, in.Image)
		if err != nil {
			return nil, apiError(err)
		}
		return &apiResult[compute.AdministrationImage]{Body: i}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/images/{image}/availability", "get-administration-image-availability", "Observe image sources without preparing them", func(ctx context.Context, in *apiAdministrationImageInput) (*apiResult[compute.AdministrationImageAvailability], error) {
		i, err := e.administration.ImageAvailability(ctx, in.Cloud, in.Image)
		if err != nil {
			return nil, administrationError(err)
		}
		return &apiResult[compute.AdministrationImageAvailability]{Body: i}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/hosts", "list-administration-hosts", "Observe hosts with explicit partial results", func(ctx context.Context, in *apiAdministrationCloudInput) (*apiResult[apiAdministrationHostList], error) {
		h, err := e.administration.LiveHosts(ctx, in.Cloud)
		if err != nil {
			return nil, administrationError(err)
		}
		out := apiAdministrationHostList{Items: []apiNode{}, UnavailableHosts: h.UnavailableHosts}
		for _, n := range h.Items {
			out.Items = append(out.Items, nodeDTO(n))
		}
		return &apiResult[apiAdministrationHostList]{Body: out}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/hosts/{node}", "get-administration-host", "Read live host diagnostics and topology", func(ctx context.Context, in *apiAdministrationHostInput) (*apiResult[apiNode], error) {
		h, err := e.administration.LiveHost(ctx, in.Cloud, in.Node)
		if err != nil {
			return nil, administrationError(err)
		}
		return &apiResult[apiNode]{Body: nodeDTO(h)}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/hosts/{node}/networks", "list-administration-host-networks", "Observe backend networks", func(ctx context.Context, in *apiAdministrationHostInput) (*apiResult[[]apiNetwork], error) {
		items, err := e.administration.LiveNetworks(ctx, in.Cloud, in.Node)
		if err != nil {
			return nil, administrationError(err)
		}
		out := []apiNetwork{}
		for _, n := range items {
			out = append(out, apiNetwork{n.NodeId, n.Name})
		}
		return &apiResult[[]apiNetwork]{Body: out}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/hosts/{node}/pools", "list-administration-host-pools", "Observe backend storage pools", func(ctx context.Context, in *apiAdministrationHostInput) (*apiResult[[]apiPool], error) {
		items, err := e.administration.LivePools(ctx, in.Cloud, in.Node)
		if err != nil {
			return nil, administrationError(err)
		}
		out := []apiPool{}
		for _, p := range items {
			out = append(out, apiPool{p.NodeId, p.Name, p.Size.Bytes(), p.Used.Bytes(), p.Free.Bytes()})
		}
		return &apiResult[[]apiPool]{Body: out}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/hosts/{node}/capabilities", "get-administration-host-capabilities", "Observe backend launch capabilities", func(ctx context.Context, in *apiAdministrationArchInput) (*apiResult[apiCapabilities], error) {
		c, err := e.administration.LiveCapabilities(ctx, in.Cloud, in.Node, compute.NewArch(in.Arch))
		if err != nil {
			return nil, administrationError(err)
		}
		out := apiCapabilities{Arch: c.Arch.String(), MaxVCPUs: c.MaxVCPUs, Firmwares: append([]string{}, c.Firmwares...), DiskBuses: []string{}, NICModels: append([]string{}, c.NICModels...), NetworkModels: c.NetworkModels, DiskControllers: append([]string{}, c.DiskControllers...), Graphics: []string{}, VideoModels: []string{}}
		for _, v := range c.DiskBuses {
			out.DiskBuses = append(out.DiskBuses, v.String())
		}
		for _, v := range c.Graphics {
			out.Graphics = append(out.Graphics, v.String())
		}
		for _, v := range c.VideoModels {
			out.VideoModels = append(out.VideoModels, v.String())
		}
		return &apiResult[apiCapabilities]{Body: out}, nil
	})
	registerAPI(api, "GET", "/administration/clouds/{cloud}/hosts/{node}/catalog", "get-administration-host-catalog", "Observe applicable logical choices for a host", func(ctx context.Context, in *apiAdministrationHostInput) (*apiResult[compute.HostCatalog], error) {
		c, err := e.administration.LiveCatalog(ctx, in.Cloud, in.Node)
		if err != nil {
			return nil, administrationError(err)
		}
		return &apiResult[compute.HostCatalog]{Body: c}, nil
	})
}

func sortNodeTopology(d *apiNode) {
	sort.Slice(d.CPUs, func(i, j int) bool { return d.CPUs[i].ID < d.CPUs[j].ID })
	sort.Slice(d.NUMA, func(i, j int) bool { return d.NUMA[i].ID < d.NUMA[j].ID })
}
