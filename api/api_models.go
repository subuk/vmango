package api

import (
	"encoding/base64"
	"fmt"
	"sort"
	"subuk/vmango/compute"
)

type apiDisk struct {
	VolumeID string `json:"volume_id" minLength:"1" doc:"Opaque host-local volume identifier returned by cloud discovery."`
	Alias    string `json:"alias,omitempty"`
	Device   string `json:"device,omitempty" enum:"disk,cdrom" default:"disk"`
	Bus      string `json:"bus,omitempty" enum:"virtio,scsi,ide" default:"virtio"`
}

type apiInterface struct {
	Network string `json:"network" minLength:"1"`
	MAC     string `json:"mac,omitempty"`
}

type apiReadInterface struct {
	Network     string   `json:"network"`
	MAC         string   `json:"mac,omitempty"`
	Model       string   `json:"model,omitempty"`
	VLAN        uint     `json:"vlan,omitempty"`
	IPAddresses []string `json:"ip_addresses" nullable:"false"`
}
type apiVCPUPin struct {
	VCPU     uint   `json:"vcpu"`
	HostCPUs []uint `json:"host_cpus" nullable:"false"`
}
type apiCPUPinning struct {
	_        struct{}     `nullable:"true"`
	Emulator []uint       `json:"emulator" nullable:"false"`
	VCPUs    []apiVCPUPin `json:"vcpus" nullable:"false"`
}
type apiConfiguredKey struct {
	Fingerprint string `json:"fingerprint"`
	Comment     string `json:"comment"`
}

type apiVM struct {
	Cloud              string `json:"cloud"`
	Zone               string `json:"zone"`
	InstanceType       string `json:"instance_type"`
	Image              string `json:"image"`
	Managed            bool   `json:"managed"`
	CPURequest         int    `json:"cpu_request"`
	MemoryRequestBytes uint64 `json:"memory_request_bytes"`

	Node           string              `json:"node"`
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	Arch           string              `json:"arch"`
	State          string              `json:"state" enum:"unknown,stopped,running"`
	VCPUs          int                 `json:"vcpus"`
	MemoryBytes    uint64              `json:"memory_bytes"`
	Firmware       string              `json:"firmware"`
	DiskController string              `json:"disk_controller"`
	GuestAgent     bool                `json:"guest_agent"`
	Autostart      bool                `json:"autostart"`
	Hugepages      bool                `json:"hugepages"`
	Graphics       string              `json:"graphics"`
	VideoModel     string              `json:"video_model"`
	Disks          []apiDisk           `json:"disks" nullable:"false"`
	Interfaces     []apiReadInterface  `json:"interfaces" nullable:"false"`
	GraphicsListen string              `json:"graphics_listen"`
	CPUPinning     *apiCPUPinning      `json:"cpu_pinning"`
	ConfiguredKeys *[]apiConfiguredKey `json:"configured_keys" doc:"Null means unavailable; an empty array means a known empty key configuration."`
	IPAddresses    []string            `json:"ip_addresses" nullable:"false"`
}

func vmDTO(vm *compute.VirtualMachine) apiVM {
	v := apiVM{Node: vm.NodeId, ID: vm.Id, Name: vm.Name, Arch: vm.Arch.String(), State: vm.State.String(), VCPUs: vm.VCpus, MemoryBytes: vm.Memory.Bytes(), Firmware: vm.Firmware, DiskController: vm.DiskController, GuestAgent: vm.GuestAgent, Autostart: vm.Autostart, Hugepages: vm.Hugepages, Graphics: vm.Graphic.Type.String(), GraphicsListen: vm.Graphic.Listen, VideoModel: vm.VideoModel.String(), Disks: []apiDisk{}, Interfaces: []apiReadInterface{}, IPAddresses: vm.IpAddressList()}
	if vm.Cpupin != nil {
		v.CPUPinning = &apiCPUPinning{Emulator: append([]uint{}, vm.Cpupin.Emulator...), VCPUs: []apiVCPUPin{}}
		for id, cpus := range vm.Cpupin.Vcpus {
			v.CPUPinning.VCPUs = append(v.CPUPinning.VCPUs, apiVCPUPin{id, append([]uint{}, cpus...)})
		}
		sort.Slice(v.CPUPinning.VCPUs, func(i, j int) bool { return v.CPUPinning.VCPUs[i].VCPU < v.CPUPinning.VCPUs[j].VCPU })
	}
	if vm.Config != nil {
		keys := []apiConfiguredKey{}
		for _, k := range vm.Config.Keys {
			keys = append(keys, apiConfiguredKey{k.Fingerprint, k.Comment})
		}
		v.ConfiguredKeys = &keys
	}
	for _, d := range vm.Volumes {
		v.Disks = append(v.Disks, apiDisk{volumeID(d.Path), d.Alias, d.DeviceType.String(), d.DeviceBus.String()})
	}
	for _, n := range vm.Interfaces {
		logical := n.LogicalNetwork
		if logical == "" {
			logical = n.NetworkName
		}
		v.Interfaces = append(v.Interfaces, apiReadInterface{logical, n.Mac, n.Model, n.AccessVlan, append([]string{}, n.IpAddressList...)})
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
	return &compute.VirtualMachineAttachedInterface{NetworkName: n.Network, Mac: n.MAC}
}

type apiVolume struct {
	Cloud        string `json:"cloud"`
	Zone         string `json:"zone"`
	StorageClass string `json:"storage_class"`
	Owned        bool   `json:"owned"`

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
	return apiVolume{ID: volumeID(v.Path), Node: v.NodeId, Name: v.Name, Pool: v.Pool, SizeBytes: v.Size.Bytes(), Format: v.Format.String(), AttachedTo: v.AttachedTo, Protected: v.Metadata.Protected}
}

type apiImageDefaults struct {
	Firmware       string `json:"firmware,omitempty" enum:"auto,bios,efi"`
	DiskBus        string `json:"disk_bus,omitempty" enum:"virtio,scsi,ide"`
	DiskController string `json:"disk_controller,omitempty" enum:"virtio-scsi"`
	NICModel       string `json:"nic_model,omitempty" enum:"virtio,e1000,rtl8139"`
	Graphics       string `json:"graphics" enum:"none,vnc,spice"`
	VideoModel     string `json:"video_model" enum:"none,cirrus,qxl"`
	GuestAgent     *bool  `json:"guest_agent,omitempty"`
	Initialization string `json:"initialization,omitempty" enum:"none,nocloud,openstack"`
}

func (e *Environ) machineDTO(vm *compute.VirtualMachine) apiVM {
	d := vmDTO(vm)
	d.Managed = e.clouds == nil || vm.Metadata != nil
	if vm.Metadata != nil {
		m := vm.Metadata
		d.Cloud = m.Cloud
		d.Zone = m.Zone
		d.InstanceType = m.InstanceType
		d.Image = m.Image
		d.CPURequest = m.CPURequest
		d.MemoryRequestBytes = m.MemoryRequestBytes
	} else {
		d.CPURequest = vm.VCpus
		d.MemoryRequestBytes = vm.Memory.Bytes()
	}
	if e.clouds != nil && d.Cloud == "" {
		if c, h, err := e.clouds.ForNode(vm.NodeId); err == nil {
			d.Cloud = c.ID
			d.Zone = h.Zone
		}
	}
	return d
}
func (e *Environ) volumeDTO(v *compute.Volume) apiVolume {
	d := volumeDTO(v)
	if e.clouds == nil {
		d.Owned = true
		return d
	}
	d.Owned = e.volumes.Owned(v)
	if o, ok := e.volumes.OwnershipFor(v); ok {
		d.Cloud = o.Cloud
		d.StorageClass = o.StorageClass
	}
	if c, h, err := e.clouds.ForNode(v.NodeId); err == nil {
		if d.Cloud == "" {
			d.Cloud = c.ID
		}
		d.Zone = h.Zone
	}
	return d
}
