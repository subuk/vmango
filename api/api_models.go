package api

import (
	"encoding/json"
	"sort"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
)

type apiDisk struct {
	VolumeID string `json:"volume_id" minLength:"1" doc:"Compound host/path volume identifier returned by cloud discovery."`
	Alias    string `json:"alias,omitempty"`
	Device   string `json:"device,omitempty" enum:"disk,cdrom" default:"disk"`
	Bus      string `json:"bus,omitempty" enum:"virtio,scsi,ide"`
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

type apiLibvirtMachineHardware struct {
	Firmware          *hardware.FirmwareBinding        `json:"firmware,omitempty"`
	CPU               *hardware.CPUObservation         `json:"cpu,omitempty"`
	SimpleCPUTopology bool                             `json:"simple_cpu_topology"`
	GuestSystem       *hardware.GuestSystemObservation `json:"guest_system,omitempty"`
	Applied           hardware.PlatformBinding         `json:"applied"`
	Profile           hardware.Profile                 `json:"profile"`
	Version           int                              `json:"version"`
	Devices           []hardware.Device                `json:"devices" nullable:"false"`
}
type apiVM struct {
	LibvirtObserved    *hardware.ObservedHardware `json:"libvirt_observed,omitempty"`
	LibvirtHardware    *apiLibvirtMachineHardware `json:"libvirt_hardware,omitempty"`
	AdoptionRequired   bool                       `json:"adoption_required,omitempty"`
	NamespaceID        compute.NamespaceID        `json:"namespace_id,omitempty"`
	BackendName        string                     `json:"backend_name,omitempty"`
	Cloud              string                     `json:"cloud"`
	Zone               string                     `json:"zone"`
	InstanceType       string                     `json:"instance_type"`
	Image              string                     `json:"image"`
	Managed            bool                       `json:"managed"`
	MemoryRequestBytes uint64                     `json:"memory_request_bytes"`

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

// Detail DTOs are separate from lightweight list rows: supplemental choices are
// discovered only for detail reads, and null denotes unavailable discovery.
type apiMachineCatalog struct {
	_              struct{}               `nullable:"true"`
	Networks       []compute.CatalogEntry `json:"networks" nullable:"false"`
	StorageClasses []compute.StorageClass `json:"storage_classes" nullable:"false"`
}
type APIVMFields = apiVM
type APIVolumeFields = apiVolume

type apiVMDetail struct {
	APIVMFields
	Catalog *apiMachineCatalog `json:"catalog"`
}
type apiVolumeCatalog struct {
	_              struct{}               `nullable:"true"`
	StorageClasses []compute.StorageClass `json:"storage_classes" nullable:"false"`
}
type apiVolumeDetail struct {
	APIVolumeFields
	Catalog *apiVolumeCatalog `json:"catalog"`
}

func vmDTO(vm *compute.VirtualMachine) apiVM {
	v := apiVM{Node: vm.NodeId, ID: vm.Id, Name: vm.Name, Arch: vm.Arch.String(), State: vm.State.String(), VCPUs: vm.VCpus, MemoryBytes: vm.Memory.Bytes(), Firmware: vm.Firmware, DiskController: vm.DiskController, GuestAgent: vm.GuestAgent, Autostart: vm.Autostart, Hugepages: vm.Hugepages, Graphics: vm.Graphic.Type.String(), GraphicsListen: vm.Graphic.Listen, VideoModel: vm.VideoModel.String(), Disks: []apiDisk{}, Interfaces: []apiReadInterface{}, IPAddresses: vm.IpAddressList()}
	if plan, err := hardware.DecodePlan(vm.AppliedPolicy); err == nil {
		v.LibvirtHardware = &apiLibvirtMachineHardware{Firmware: plan.Firmware, Profile: plan.Profile, Version: plan.Version, Devices: plan.Devices, Applied: plan.Platform(), GuestSystem: plan.GuestSystem, CPU: plan.CPU, SimpleCPUTopology: plan.SimpleCPUTopology}
	}
	if len(vm.ObservedBackend) > 0 {
		var observed hardware.ObservedHardware
		if json.Unmarshal(vm.ObservedBackend, &observed) == nil {
			v.LibvirtObserved = &observed
		}
	}
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
		v.Disks = append(v.Disks, apiDisk{volumeID(vm.NodeId, d.Path), d.Alias, d.DeviceType.String(), d.DeviceBus.String()})
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

func volumeID(node, path string) string { return (compute.VolumeAddress{Host: node, Path: path}).ID() }
func (d apiDisk) domain(path string) *compute.VirtualMachineAttachedVolume {
	return &compute.VirtualMachineAttachedVolume{Path: path, Alias: d.Alias, DeviceType: compute.NewDeviceType(d.Device), DeviceBus: compute.NewDeviceBus(d.Bus)}
}
func (n apiInterface) domain() *compute.VirtualMachineAttachedInterface {
	return &compute.VirtualMachineAttachedInterface{NetworkName: n.Network, Mac: n.MAC}
}

type apiVolume struct {
	NamespaceID  compute.NamespaceID `json:"namespace_id,omitempty"`
	BackendName  string              `json:"backend_name,omitempty"`
	Cloud        string              `json:"cloud"`
	Zone         string              `json:"zone"`
	StorageClass string              `json:"storage_class"`
	Owned        bool                `json:"owned"`

	ID         string            `json:"id"`
	Node       string            `json:"node"`
	Name       string            `json:"name"`
	Pool       string            `json:"pool"`
	SizeBytes  uint64            `json:"size_bytes"`
	Format     string            `json:"format"`
	AttachedTo string            `json:"attached_to,omitempty"`
	Protected  bool              `json:"protected"`
	CloneMode  compute.CloneMode `json:"clone_mode" enum:"copy,overlay" doc:"Observed disk dependency, independent of current storage class policy."`
}

func volumeDTO(v *compute.Volume) apiVolume {
	mode := compute.CloneModeCopy
	if v.BackingStore != nil {
		mode = compute.CloneModeOverlay
	}
	return apiVolume{CloneMode: mode, ID: volumeID(v.NodeId, v.Path), Node: v.NodeId, Name: v.Name, Pool: v.Pool, SizeBytes: v.Size.Bytes(), Format: v.Format.String(), AttachedTo: v.AttachedTo, Protected: v.Metadata.Protected}
}

type apiImageDefaults struct {
	Firmware       string `json:"firmware,omitempty" enum:"auto,bios,efi"`
	DiskBus        string `json:"disk_bus,omitempty" enum:"virtio,scsi,ide"`
	DiskController string `json:"disk_controller,omitempty" enum:"virtio-scsi"`
	NICModel       string `json:"nic_model,omitempty" enum:"virtio,e1000,rtl8139"`
	Graphics       string `json:"graphics" enum:"none,vnc,spice"`
	VideoModel     string `json:"video_model" enum:"none,cirrus,qxl,virtio"`
	GuestAgent     *bool  `json:"guest_agent,omitempty"`
	Initialization string `json:"initialization,omitempty" enum:"none,nocloud,openstack"`
}

func (e *Environ) machineDTO(vm *compute.VirtualMachine) apiVM {
	d := vmDTO(vm)
	d.NamespaceID, d.BackendName = vm.NamespaceID, vm.Name
	if vm.Label != "" {
		d.Name = vm.Label
	}
	d.Managed = e.clouds == nil || vm.Metadata != nil
	if e.vms != nil {
		if backend, ok := e.vms.VirtualMachineRepository.(compute.BackendMachineRepository); ok && backend.ValidateAppliedPolicy(vm) != nil {
			d.Managed = false
		}
	}
	d.AdoptionRequired = !d.Managed
	if vm.Metadata != nil {
		m := vm.Metadata
		d.Cloud = m.Cloud
		d.Zone = m.Zone
		d.InstanceType = m.InstanceType
		d.Image = m.Image
		d.MemoryRequestBytes = m.MemoryRequestBytes
	} else {
		d.MemoryRequestBytes = vm.Memory.Bytes()
	}
	// Resource routes follow the host's current cloud and zone; allocation
	// metadata remains an applied snapshot and may predate catalog changes.
	if e.clouds != nil {
		if c, h, err := e.clouds.ForNode(vm.NodeId); err == nil {
			d.Cloud = c.ID
			d.Zone = h.Zone
		}
	}
	return d
}
func (e *Environ) volumeDTO(v *compute.Volume) apiVolume {
	d := volumeDTO(v)
	d.NamespaceID, d.BackendName = v.NamespaceID, v.Name
	if v.Label != "" {
		d.Name = v.Label
	}
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
		d.Cloud = c.ID
		d.Zone = h.Zone
	}
	return d
}
