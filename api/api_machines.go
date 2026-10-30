package api

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"subuk/vmango/compute"
)

type apiPatchVM struct {
	Autostart      *bool   `json:"autostart,omitempty"`
	GuestAgent     *bool   `json:"guest_agent,omitempty"`
	Graphics       *string `json:"graphics,omitempty" enum:"none,vnc,spice"`
	GraphicsListen *string `json:"graphics_listen,omitempty" doc:"Backend listen address; Vmango still proxies console access."`
	VideoModel     *string `json:"video_model,omitempty" enum:"none,cirrus,qxl"`
}
type apiPatchVMInput struct {
	SubmissionInput
	Node string `path:"node"`
	VM   string `path:"vm"`
	Body apiPatchVM
}
type apiDeleteVMInput struct {
	SubmissionInput
	Node          string `path:"node"`
	VM            string `path:"vm"`
	DeleteVolumes bool   `query:"delete_volumes" default:"false" doc:"Explicit opt-in to deleting attached disks."`
}
type apiActionInput struct {
	SubmissionInput
	Node string `path:"node"`
	VM   string `path:"vm"`
	Body struct {
		Action string `json:"action" enum:"start,reboot,poweroff" doc:"poweroff is libvirt Destroy, a hard power-off."`
	}
}
type apiAttachDiskInput struct {
	SubmissionInput
	Node string `path:"node"`
	VM   string `path:"vm"`
	Body apiDisk
}
type apiDetachDiskInput struct {
	SubmissionInput
	Node   string `path:"node"`
	VM     string `path:"vm"`
	Volume string `path:"volume" doc:"Volume ID, not the guest device name."`
}
type apiAttachInterfaceInput struct {
	SubmissionInput
	Node string `path:"node"`
	VM   string `path:"vm"`
	Body apiInterface
}
type apiDetachInterfaceInput struct {
	SubmissionInput
	Node string `path:"node"`
	VM   string `path:"vm"`
	MAC  string `path:"mac"`
}

func (e *Environ) apiGetVM(id, node string) (*apiResult[apiVM], error) {
	v, err := e.vms.Get(id, node)
	if err != nil {
		return nil, apiError(err)
	}
	return &apiResult[apiVM]{Body: e.machineDTO(v)}, nil
}

func (e *Environ) registerVMOperations(api huma.API) {
	const path = "/nodes/{node}/vms"
	registerAPI(api, "GET", path, "list-vms", "List virtual machines on a node", func(ctx context.Context, in *apiNodeInput) (*apiResult[[]apiVM], error) {
		if _, err := e.nodes.Get(in.Node, compute.NodeGetOptions{NoPins: true}); err != nil {
			return nil, apiError(err)
		}
		vs, err := e.vms.List(compute.VirtualMachineListOptions{NodeIds: []string{in.Node}})
		if err != nil {
			return nil, apiError(err)
		}
		out := []apiVM{}
		for _, v := range vs {
			out = append(out, e.machineDTO(v))
		}
		return &apiResult[[]apiVM]{Body: out}, nil
	})
	registerAPI(api, "GET", path+"/{vm}", "get-vm", "Get virtual machine", func(ctx context.Context, in *apiVMInput) (*apiResult[apiVM], error) {
		return e.apiGetVM(in.VM, in.Node)
	})
	registerAPI(api, "PATCH", path+"/{vm}", "update-vm", "Queue a persistent configuration update; omitted fields are preserved", func(ctx context.Context, in *apiPatchVMInput) (*apiAccepted, error) {
		patch := compute.MachinePatch{Autostart: in.Body.Autostart, GuestAgent: in.Body.GuestAgent, Graphics: in.Body.Graphics, GraphicsListen: in.Body.GraphicsListen, VideoModel: in.Body.VideoModel}
		return e.submitTask(ctx, in.IdempotencyKey, "machine.update", compute.InfrastructureCommand{Node: in.Node, VM: in.VM, Patch: &patch})
	})
	registerAPI(api, "DELETE", path+"/{vm}", "delete-vm", "Queue VM deletion; attached disk deletion requires delete_volumes=true", func(ctx context.Context, in *apiDeleteVMInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.delete", compute.InfrastructureCommand{Node: in.Node, VM: in.VM, DeleteVolumes: in.DeleteVolumes})
	})
	registerAPI(api, "POST", path+"/{vm}/actions", "vm-action", "Queue a start, reboot or hard power-off", func(ctx context.Context, in *apiActionInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine."+in.Body.Action, compute.InfrastructureCommand{Node: in.Node, VM: in.VM})
	})
	registerAPI(api, "POST", path+"/{vm}/disks", "attach-disk", "Queue disk attachment", func(ctx context.Context, in *apiAttachDiskInput) (*apiAccepted, error) {
		disk, err := in.Body.domain()
		if err != nil {
			return nil, apiError(err)
		}
		return e.submitTask(ctx, in.IdempotencyKey, "machine.attach-disk", compute.InfrastructureCommand{Node: in.Node, VM: in.VM, Disk: disk})
	})
	registerAPI(api, "DELETE", path+"/{vm}/disks/{volume}", "detach-disk", "Queue disk detachment without deletion", func(ctx context.Context, in *apiDetachDiskInput) (*apiAccepted, error) {
		path, err := volumePath(in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		return e.submitTask(ctx, in.IdempotencyKey, "machine.detach-disk", compute.InfrastructureCommand{Node: in.Node, VM: in.VM, Path: path})
	})
	registerAPI(api, "POST", path+"/{vm}/interfaces", "attach-interface", "Queue network interface attachment", func(ctx context.Context, in *apiAttachInterfaceInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.attach-interface", compute.InfrastructureCommand{Node: in.Node, VM: in.VM, Interface: in.Body.domain()})
	})
	registerAPI(api, "DELETE", path+"/{vm}/interfaces/{mac}", "detach-interface", "Queue network interface detachment", func(ctx context.Context, in *apiDetachInterfaceInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.detach-interface", compute.InfrastructureCommand{Node: in.Node, VM: in.VM, MAC: in.MAC})
	})
}
