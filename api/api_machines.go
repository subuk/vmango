package api

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"subuk/vmango/compute"
)

type apiDeleteVMInput struct {
	Cloud string `path:"cloud"`
	SubmissionInput
	Node          string `path:"node"`
	VM            string `path:"vm"`
	DeleteVolumes bool   `query:"delete_volumes" default:"false" doc:"Explicit opt-in to deleting attached disks."`
}
type apiActionInput struct {
	Cloud string `path:"cloud"`
	SubmissionInput
	Node string `path:"node"`
	VM   string `path:"vm"`
	Body struct {
		Action string `json:"action" enum:"start,reboot,poweroff" doc:"poweroff is libvirt Destroy, a hard power-off."`
	}
}
type apiAttachDiskInput struct {
	Cloud string `path:"cloud"`
	SubmissionInput
	Node string `path:"node"`
	VM   string `path:"vm"`
	Body apiDisk
}
type apiDetachDiskInput struct {
	Cloud string `path:"cloud"`
	SubmissionInput
	Node   string `path:"node"`
	VM     string `path:"vm"`
	Volume string `path:"volume" doc:"Volume ID, not the guest device name."`
}
type apiAttachInterfaceInput struct {
	Cloud string `path:"cloud"`
	SubmissionInput
	Node string `path:"node"`
	VM   string `path:"vm"`
	Body apiInterface
}
type apiDetachInterfaceInput struct {
	Cloud string `path:"cloud"`
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
	const path = "/clouds/{cloud}/machines/{node}/{vm}"
	registerAPI(api, "GET", path, "get-vm", "Get virtual machine", func(ctx context.Context, in *apiVMInput) (*apiResult[apiVM], error) {
		return e.apiGetVM(in.VM, in.Node)
	})
	registerAPI(api, "DELETE", path, "delete-vm", "Queue VM deletion; attached disk deletion requires delete_volumes=true", func(ctx context.Context, in *apiDeleteVMInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.delete", compute.InfrastructureCommand{CloudID: in.Cloud, Node: in.Node, VM: in.VM, DeleteVolumes: in.DeleteVolumes})
	})
	registerAPI(api, "POST", path+"/actions", "vm-action", "Queue a start, reboot or hard power-off", func(ctx context.Context, in *apiActionInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine."+in.Body.Action, compute.InfrastructureCommand{CloudID: in.Cloud, Node: in.Node, VM: in.VM})
	})
	registerAPI(api, "POST", path+"/disks/attach", "attach-disk", "Queue disk attachment", func(ctx context.Context, in *apiAttachDiskInput) (*apiAccepted, error) {
		disk, err := in.Body.domain()
		if err != nil {
			return nil, apiError(err)
		}
		return e.submitTask(ctx, in.IdempotencyKey, "machine.attach-disk", compute.InfrastructureCommand{CloudID: in.Cloud, Node: in.Node, VM: in.VM, Disk: disk})
	})
	registerAPI(api, "DELETE", path+"/disks/{volume}", "detach-disk", "Queue disk detachment without deletion", func(ctx context.Context, in *apiDetachDiskInput) (*apiAccepted, error) {
		path, err := volumePath(in.Volume)
		if err != nil {
			return nil, apiError(err)
		}
		return e.submitTask(ctx, in.IdempotencyKey, "machine.detach-disk", compute.InfrastructureCommand{CloudID: in.Cloud, Node: in.Node, VM: in.VM, Path: path})
	})
	registerAPI(api, "POST", path+"/interfaces", "attach-interface", "Queue network interface attachment", func(ctx context.Context, in *apiAttachInterfaceInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.attach-interface", compute.InfrastructureCommand{CloudID: in.Cloud, Node: in.Node, VM: in.VM, Interface: in.Body.domain()})
	})
	registerAPI(api, "DELETE", path+"/interfaces/{mac}", "detach-interface", "Queue network interface detachment", func(ctx context.Context, in *apiDetachInterfaceInput) (*apiAccepted, error) {
		return e.submitTask(ctx, in.IdempotencyKey, "machine.detach-interface", compute.InfrastructureCommand{CloudID: in.Cloud, Node: in.Node, VM: in.VM, MAC: in.MAC})
	})
}
