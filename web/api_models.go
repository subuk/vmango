package web

import (
	"encoding/base64"
	"fmt"
	"subuk/vmango/compute"
)

type apiDisk struct {
	VolumeID string `json:"volume_id" minLength:"1" doc:"Opaque node-scoped volume identifier returned by discovery."`
	Alias    string `json:"alias,omitempty"`
	Device   string `json:"device,omitempty" enum:"disk,cdrom" default:"disk"`
	Bus      string `json:"bus,omitempty" enum:"virtio,scsi,ide" default:"virtio"`
}

type apiInterface struct {
	Network string `json:"network" minLength:"1"`
	MAC     string `json:"mac,omitempty"`
	Model   string `json:"model,omitempty" enum:"virtio,e1000,rtl8139"`
	VLAN    uint   `json:"vlan,omitempty" maximum:"4094"`
}

type apiVM struct {
	Node           string         `json:"node"`
	ID             string         `json:"id"`
	Arch           string         `json:"arch"`
	State          string         `json:"state" enum:"unknown,stopped,running"`
	VCPUs          int            `json:"vcpus"`
	MemoryBytes    uint64         `json:"memory_bytes"`
	Firmware       string         `json:"firmware"`
	DiskController string         `json:"disk_controller"`
	GuestAgent     bool           `json:"guest_agent"`
	Autostart      bool           `json:"autostart"`
	Hugepages      bool           `json:"hugepages"`
	Graphics       string         `json:"graphics"`
	VideoModel     string         `json:"video_model"`
	Disks          []apiDisk      `json:"disks"`
	Interfaces     []apiInterface `json:"interfaces"`
	IPAddresses    []string       `json:"ip_addresses"`
}

func vmDTO(vm *compute.VirtualMachine) apiVM {
	v := apiVM{Node: vm.NodeId, ID: vm.Id, Arch: vm.Arch.String(), State: vm.State.String(), VCPUs: vm.VCpus, MemoryBytes: vm.Memory.Bytes(), Firmware: vm.Firmware, DiskController: vm.DiskController, GuestAgent: vm.GuestAgent, Autostart: vm.Autostart, Hugepages: vm.Hugepages, Graphics: vm.Graphic.Type.String(), VideoModel: vm.VideoModel.String(), Disks: []apiDisk{}, Interfaces: []apiInterface{}, IPAddresses: vm.IpAddressList()}
	for _, d := range vm.Volumes {
		v.Disks = append(v.Disks, apiDisk{volumeID(d.Path), d.Alias, d.DeviceType.String(), d.DeviceBus.String()})
	}
	for _, n := range vm.Interfaces {
		v.Interfaces = append(v.Interfaces, apiInterface{n.NetworkName, n.Mac, n.Model, n.AccessVlan})
	}
	return v
}

func volumeID(path string) string { return base64.RawURLEncoding.EncodeToString([]byte(path)) }
func volumePath(id string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(b) == 0 || len(b) > 4096 {
		return "", fmt.Errorf("%w: invalid volume identifier", compute.ErrInvalid)
	}
	return string(b), nil
}
func (d apiDisk) domain() (*compute.VirtualMachineAttachedVolume, error) {
	path, err := volumePath(d.VolumeID)
	if err != nil {
		return nil, err
	}
	return &compute.VirtualMachineAttachedVolume{Path: path, Alias: d.Alias, DeviceType: compute.NewDeviceType(d.Device), DeviceBus: compute.NewDeviceBus(d.Bus)}, nil
}
func (n apiInterface) domain() *compute.VirtualMachineAttachedInterface {
	return &compute.VirtualMachineAttachedInterface{NetworkName: n.Network, Mac: n.MAC, Model: n.Model, AccessVlan: n.VLAN}
}

type apiVolume struct {
	ID         string `json:"id"`
	Node       string `json:"node"`
	Name       string `json:"name"`
	Pool       string `json:"pool"`
	SizeBytes  uint64 `json:"size_bytes"`
	Format     string `json:"format"`
	AttachedTo string `json:"attached_to,omitempty"`
	Protected  bool   `json:"protected"`
}

func volumeDTO(v *compute.Volume) apiVolume {
	return apiVolume{volumeID(v.Path), v.NodeId, v.Name, v.Pool, v.Size.Bytes(), v.Format.String(), v.AttachedTo, v.Metadata.Protected}
}

type apiImageDefaults struct {
	Firmware       string `json:"firmware,omitempty" enum:"auto,bios,efi"`
	DiskBus        string `json:"disk_bus,omitempty" enum:"virtio,scsi,ide"`
	DiskController string `json:"disk_controller,omitempty" enum:"virtio-scsi"`
	NICModel       string `json:"nic_model,omitempty" enum:"virtio,e1000,rtl8139"`
	GuestAgent     *bool  `json:"guest_agent,omitempty"`
	Initialization string `json:"initialization,omitempty" enum:"none,nocloud,openstack"`
}

func (d apiImageDefaults) domain() compute.ImageDefaults {
	return compute.ImageDefaults{Firmware: d.Firmware, DiskBus: compute.NewDeviceBus(d.DiskBus), DiskController: d.DiskController, NICModel: d.NICModel, GuestAgent: d.GuestAgent, Initialization: d.Initialization}
}
