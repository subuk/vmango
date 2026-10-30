package api

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"subuk/vmango/compute"
)

type apiGlobalListInput struct {
	Nodes []string `query:"node,explode"`
	Pool  string   `query:"pool"`
}

func (e *Environ) validateNodes(nodes []string) error {
	for _, node := range nodes {
		if _, err := e.nodes.Get(node, compute.NodeGetOptions{NoPins: true}); err != nil {
			return err
		}
	}
	return nil
}

func (e *Environ) registerGlobalLists(api huma.API) {
	registerAPI(api, "GET", "/vms", "list-all-vms", "List virtual machines across selected nodes", func(ctx context.Context, in *apiGlobalListInput) (*apiResult[[]apiVM], error) {
		if err := e.validateNodes(in.Nodes); err != nil {
			return nil, apiError(err)
		}
		items, err := e.vms.List(compute.VirtualMachineListOptions{NodeIds: in.Nodes})
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiVM{}
		for _, v := range items {
			out = append(out, e.machineDTO(v))
		}
		return &apiResult[[]apiVM]{Body: out}, nil
	})
	registerAPI(api, "GET", "/volumes", "list-all-volumes", "List visible volumes across selected nodes", func(ctx context.Context, in *apiGlobalListInput) (*apiResult[[]apiVolume], error) {
		if err := e.validateNodes(in.Nodes); err != nil {
			return nil, apiError(err)
		}
		opts := compute.VolumeListOptions{NodeIds: in.Nodes}
		if in.Pool != "" {
			opts.PoolNames = []string{in.Pool}
		}
		items, err := e.volumes.List(opts)
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiVolume{}
		for _, v := range items {
			if !v.Metadata.Hidden {
				out = append(out, e.volumeDTO(v))
			}
		}
		return &apiResult[[]apiVolume]{Body: out}, nil
	})
	registerAPI(api, "GET", "/networks", "list-all-networks", "List networks across selected nodes", func(ctx context.Context, in *apiGlobalListInput) (*apiResult[[]apiNetwork], error) {
		if err := e.validateNodes(in.Nodes); err != nil {
			return nil, apiError(err)
		}
		items, err := e.networks.List(compute.NetworkListOptions{NodeIds: in.Nodes})
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiNetwork{}
		for _, n := range items {
			out = append(out, apiNetwork{n.NodeId, n.Name})
		}
		return &apiResult[[]apiNetwork]{Body: out}, nil
	})
}
