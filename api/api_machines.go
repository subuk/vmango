package api

import (
	"context"
	"github.com/danielgtaylor/huma/v2"
	"subuk/vmango/compute"
)

type apiNewDisk struct {
	Name      string `json:"name" minLength:"1"`
	Pool      string `json:"pool" minLength:"1"`
	Format    string `json:"format,omitempty" enum:"raw,qcow2" default:"qcow2"`
	SizeBytes uint64 `json:"size_bytes" minimum:"1"`
	Alias     string `json:"alias,omitempty"`
	Device    string `json:"device,omitempty" enum:"disk,cdrom" default:"disk"`
	Bus       string `json:"bus,omitempty" enum:"virtio,scsi,ide" default:"virtio"`
}
type apiCloneDisk struct {
	ImageID   string `json:"image_id" minLength:"1"`
	Name      string `json:"name" minLength:"1"`
	Pool      string `json:"pool" minLength:"1"`
	Format    string `json:"format,omitempty" enum:"raw,qcow2"`
	SizeBytes uint64 `json:"size_bytes,omitempty" doc:"Omit to use the larger of source capacity and image minimum."`
	Alias     string `json:"alias,omitempty"`
	Bus       string `json:"bus,omitempty" enum:"virtio,scsi,ide"`
}
type apiCloudInit struct {
	Hostname string   `json:"hostname,omitempty"`
	UserData string   `json:"user_data,omitempty" maxLength:"1048576" doc:"Literal cloud-init user-data, not base64."`
	Keys     []string `json:"keys,omitempty" doc:"SSH key fingerprints from /api/v1/keys."`
}
type apiCreateVM struct {
	ID          string           `json:"id" pattern:"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$" example:"demo"`
	Arch        string           `json:"arch,omitempty" enum:"x86_64,aarch64" doc:"Omit to inherit image architecture, or node architecture without images."`
	VCPUs       int              `json:"vcpus" minimum:"1" maximum:"65535" example:"2"`
	MemoryBytes uint64           `json:"memory_bytes" minimum:"1" example:"2147483648"`
	Start       bool             `json:"start,omitempty"`
	Autostart   bool             `json:"autostart,omitempty"`
	Hugepages   bool             `json:"hugepages,omitempty"`
	Graphics    string           `json:"graphics,omitempty" enum:"none,vnc,spice" default:"vnc"`
	VideoModel  string           `json:"video_model,omitempty" enum:"none,cirrus,qxl" default:"none"`
	Defaults    apiImageDefaults `json:"defaults,omitempty"`
	CloudInit   *apiCloudInit    `json:"cloud_init,omitempty" doc:"Image initialization defaults apply even when omitted. Use defaults.initialization=none to disable."`
	CloneDisks  []apiCloneDisk   `json:"clone_disks,omitempty"`
	NewDisks    []apiNewDisk     `json:"new_disks,omitempty"`
	Disks       []apiDisk        `json:"disks,omitempty"`
	Interfaces  []apiInterface   `json:"interfaces,omitempty"`
}
type apiCreateVMInput struct {
	SubmissionInput
	Node string `path:"node"`
	Body apiCreateVM
}
type apiPatchVM struct {
	VCPUs          *int    `json:"vcpus,omitempty" minimum:"1" maximum:"65535"`
	MemoryBytes    *uint64 `json:"memory_bytes,omitempty" minimum:"1"`
	Autostart      *bool   `json:"autostart,omitempty"`
	GuestAgent     *bool   `json:"guest_agent,omitempty"`
	Hugepages      *bool   `json:"hugepages,omitempty"`
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
	return &apiResult[apiVM]{Body: vmDTO(v)}, nil
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
			out = append(out, vmDTO(v))
		}
		return &apiResult[[]apiVM]{Body: out}, nil
	})
	registerAPI(api, "GET", path+"/{vm}", "get-vm", "Get virtual machine", func(ctx context.Context, in *apiVMInput) (*apiResult[apiVM], error) {
		return e.apiGetVM(in.VM, in.Node)
	})
	registerAPI(api, "POST", path, "create-vm", "Create VM, provision cloud-init and optionally start", func(ctx context.Context, in *apiCreateVMInput) (*apiAccepted, error) {
		b := in.Body
		vm := &compute.VirtualMachine{GuestAgent: true, Config: &compute.VirtualMachineConfig{Hostname: b.ID}, Id: b.ID, NodeId: in.Node, Arch: compute.NewArch(b.Arch), VCpus: b.VCPUs, Memory: compute.NewSize(b.MemoryBytes, compute.SizeUnitB), Autostart: b.Autostart, Hugepages: b.Hugepages, Graphic: compute.VirtualMachineGraphic{Type: compute.NewGraphicType(b.Graphics)}, VideoModel: compute.NewVideoModel(b.VideoModel)}
		if b.CloudInit != nil {
			vm.Config = &compute.VirtualMachineConfig{Hostname: b.CloudInit.Hostname, Userdata: []byte(b.CloudInit.UserData)}
			if vm.Config.Hostname == "" {
				vm.Config.Hostname = vm.Id
			}

		}
		for _, d := range b.Disks {
			v, err := d.domain()
			if err != nil {
				return nil, apiError(err)
			}
			vm.Volumes = append(vm.Volumes, v)
		}
		for _, n := range b.Interfaces {
			vm.Interfaces = append(vm.Interfaces, n.domain())
		}
		clones := []compute.VirtualMachineManagerClonedVolumeParams{}
		for _, d := range b.CloneDisks {
			clones = append(clones, compute.VirtualMachineManagerClonedVolumeParams{ImageID: d.ImageID, NewName: d.Name, NewPool: d.Pool, NewFormat: compute.NewVolumeFormat(d.Format), NewSize: compute.NewSize(d.SizeBytes, compute.SizeUnitB), Alias: d.Alias, DeviceType: compute.DeviceTypeDisk, DeviceBus: compute.NewDeviceBus(d.Bus)})
		}
		disks := []compute.VirtualMachineManagerCreatedVolumeParams{}
		for _, d := range b.NewDisks {
			disks = append(disks, compute.VirtualMachineManagerCreatedVolumeParams{Name: d.Name, Pool: d.Pool, Format: compute.NewVolumeFormat(d.Format), Size: compute.NewSize(d.SizeBytes, compute.SizeUnitB), Alias: d.Alias, DeviceType: compute.NewDeviceType(d.Device), DeviceBus: compute.NewDeviceBus(d.Bus)})
		}
		command := compute.InfrastructureCommand{Node: in.Node, VM: vm.Id, Machine: vm, Clones: clones, NewVolumes: disks, Start: b.Start, Defaults: b.Defaults.domain()}
		if b.CloudInit != nil {
			command.KeyIDs = b.CloudInit.Keys
		}
		return e.submitTask(ctx, in.IdempotencyKey, "machine.create", command)
	})
	registerAPI(api, "PATCH", path+"/{vm}", "update-vm", "Queue a persistent configuration update; omitted fields are preserved", func(ctx context.Context, in *apiPatchVMInput) (*apiAccepted, error) {
		patch := compute.MachinePatch(in.Body)
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
