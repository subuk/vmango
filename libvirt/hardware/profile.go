// Package hardware defines libvirt guest policy independently of hypervisor calls
// and XML translation. Persisted plans are interpreted only by this backend.
package hardware

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"slices"
)

const Version = 1
const MigrationGuide = "https://github.com/subuk/vmango/blob/master/docs/HARDWARE-PROFILES-MIGRATION.md"

type ConfigDrive struct {
	Enabled    *bool  `json:"enabled,omitempty"`
	Format     string `json:"format"`
	DeviceType string `json:"device_type"`
	Bus        string `json:"bus"`
}

func (c ConfigDrive) IsEnabled() bool { return c.Enabled == nil || *c.Enabled }

type Profile struct {
	SMBIOS         *SMBIOS     `json:"smbios,omitempty"`
	Clock          *Clock      `json:"clock,omitempty"`
	Timers         []Timer     `json:"timers,omitempty"`
	Machine        *Machine    `json:"machine,omitempty"`
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Firmware       string      `json:"firmware"`
	DiskBus        string      `json:"disk_bus"`
	DiskController string      `json:"disk_controller,omitempty"`
	NICModel       string      `json:"nic_model"`
	Graphics       string      `json:"graphics"`
	VideoModel     string      `json:"video_model"`
	GuestAgent     *bool       `json:"guest_agent,omitempty"`
	Hugepages      bool        `json:"hugepages"`
	ConfigDrive    ConfigDrive `json:"config_drive"`
}

func (p Profile) GuestAgentEnabled() bool { return p.GuestAgent == nil || *p.GuestAgent }

type DeviceKind string

const (
	Controller        DeviceKind = "controller"
	DiskPolicy        DeviceKind = "ordinary-disks"
	ConfigDriveDevice DeviceKind = "config-drive"
	GuestIntegration  DeviceKind = "guest-agent"
	Display           DeviceKind = "graphics"
)

// Device is a resolved dependency declaration. Owned state participates in the
// VM lifecycle; immutable shared dependencies have no state to delete/snapshot.
type Device struct {
	ID           string     `json:"id"`
	Kind         DeviceKind `json:"kind"`
	Model        string     `json:"model"`
	Dependencies []string   `json:"dependencies,omitempty"`
	StateOwner   string     `json:"state_owner,omitempty"`
}

// StateContract describes lifecycle obligations for future stateful extensions.
// The current config drive is VM-owned and is generated from provisioning input.
type StateContract struct {
	DeviceID string `json:"device_id"`
	Owner    string `json:"owner"`
	Snapshot bool   `json:"snapshot"`
}
type Plan struct {
	GuestSystem     *GuestSystemObservation `json:"guest_system,omitempty"`
	Version         int                     `json:"version"`
	Profile         Profile                 `json:"profile"`
	Devices         []Device                `json:"devices"`
	State           []StateContract         `json:"state,omitempty"`
	CPUThreads      int                     `json:"cpu_threads,omitempty"`
	Machine         string                  `json:"machine,omitempty"`
	DomainType      string                  `json:"domain_type,omitempty"`
	HugepageSizeKiB uint64                  `json:"hugepage_size_kib,omitempty"`
	Emulator        string                  `json:"emulator,omitempty"`
}
type policyEnvelope struct {
	Version int     `json:"version"`
	Profile Profile `json:"profile"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func Normalize(p Profile) (Profile, error) {
	fail := func(field string) (Profile, error) {
		return p, fmt.Errorf("hardware profile %q: invalid or incompatible %s", p.ID, field)
	}
	if !identifier.MatchString(p.ID) {
		return fail("identifier")
	}
	if p.Machine != nil {
		machine, err := NormalizeMachine(*p.Machine)
		if err != nil {
			return p, fmt.Errorf("hardware profile %q: %w", p.ID, err)
		}
		p.Machine = &machine
	}
	if p.Name == "" {
		p.Name = p.ID
	}
	if p.Firmware == "" {
		p.Firmware = "auto"
	}
	if !slices.Contains([]string{"auto", "bios", "efi"}, p.Firmware) {
		return fail("firmware")
	}
	switch p.DiskBus {
	case "", "virtio-blk":
		p.DiskBus = "virtio"
	case "virtio-scsi":
		p.DiskBus = "scsi"
	}
	if !slices.Contains([]string{"virtio", "scsi", "ide"}, p.DiskBus) {
		return fail("disk_bus")
	}
	if p.DiskBus == "scsi" && p.DiskController == "" {
		p.DiskController = "virtio-scsi"
	}
	if p.DiskController != "" && (p.DiskController != "virtio-scsi" || p.DiskBus != "scsi") {
		return fail("disk_controller")
	}
	if p.NICModel == "" {
		p.NICModel = "virtio"
	}
	if !slices.Contains([]string{"virtio", "e1000", "rtl8139"}, p.NICModel) {
		return fail("nic_model")
	}
	if p.Graphics == "" {
		p.Graphics = "vnc"
	}
	if !slices.Contains([]string{"none", "vnc", "spice"}, p.Graphics) {
		return fail("graphics")
	}
	if p.VideoModel == "" {
		p.VideoModel = "none"
	}
	if !slices.Contains([]string{"none", "qxl", "cirrus", "virtio"}, p.VideoModel) || p.Graphics == "none" && p.VideoModel != "none" {
		return fail("video_model")
	}
	if p.GuestAgent != nil {
		value := *p.GuestAgent
		p.GuestAgent = &value
	}
	if p.ConfigDrive.Enabled != nil {
		value := *p.ConfigDrive.Enabled
		p.ConfigDrive.Enabled = &value
	}
	if p.GuestAgent == nil {
		value := true
		p.GuestAgent = &value
	}
	if p.ConfigDrive.Enabled == nil {
		value := true
		p.ConfigDrive.Enabled = &value
	}
	if p.ConfigDrive.Format == "" {
		p.ConfigDrive.Format = "nocloud"
	}
	if !slices.Contains([]string{"nocloud", "openstack"}, p.ConfigDrive.Format) {
		return fail("config_drive.format")
	}
	if p.ConfigDrive.DeviceType == "" {
		p.ConfigDrive.DeviceType = "disk"
	}
	if p.ConfigDrive.Bus == "" {
		p.ConfigDrive.Bus = "virtio"
	}
	switch p.ConfigDrive.Bus {
	case "virtio-blk":
		p.ConfigDrive.Bus = "virtio"
	case "virtio-scsi":
		p.ConfigDrive.Bus = "scsi"
	}
	if !slices.Contains([]string{"disk", "cdrom"}, p.ConfigDrive.DeviceType) || !slices.Contains([]string{"virtio", "scsi", "ide"}, p.ConfigDrive.Bus) || p.ConfigDrive.DeviceType == "cdrom" && p.ConfigDrive.Bus == "virtio" {
		return fail("config_drive device_type/bus combination (virtio supports disks only)")
	}
	return NormalizeGuestSystem(p)
}
func Encode(p Profile) (json.RawMessage, error) {
	p, err := Normalize(p)
	if err != nil {
		return nil, err
	}
	return json.Marshal(policyEnvelope{Version: Version, Profile: p})
}
func DecodePolicy(raw json.RawMessage) (Profile, error) {
	var e policyEnvelope
	if err := json.Unmarshal(raw, &e); err != nil {
		return Profile{}, fmt.Errorf("invalid libvirt hardware policy: %w", err)
	}
	if e.Version != Version {
		return Profile{}, fmt.Errorf("unsupported libvirt hardware policy version %d", e.Version)
	}
	return Normalize(e.Profile)
}
func Resolve(p Profile, emulator string) (Plan, error) {
	p, err := Normalize(p)
	if err != nil {
		return Plan{}, err
	}
	result := Plan{Version: Version, Profile: p, Emulator: emulator, Devices: []Device{}}
	if p.DiskBus == "scsi" || p.ConfigDrive.IsEnabled() && p.ConfigDrive.Bus == "scsi" {
		result.Devices = append(result.Devices, Device{ID: "scsi-0", Kind: Controller, Model: "virtio-scsi"})
	}
	if p.DiskBus == "ide" || p.ConfigDrive.IsEnabled() && p.ConfigDrive.Bus == "ide" {
		result.Devices = append(result.Devices, Device{ID: "ide-0", Kind: Controller, Model: "ide"})
	}
	deps := func(bus string) []string {
		if bus == "scsi" || bus == "ide" {
			return []string{bus + "-0"}
		}
		return nil
	}
	result.Devices = append(result.Devices, Device{ID: "ordinary-disks", Kind: DiskPolicy, Model: p.DiskBus, Dependencies: deps(p.DiskBus)})
	if p.ConfigDrive.IsEnabled() {
		result.Devices = append(result.Devices, Device{ID: "config-drive", Kind: ConfigDriveDevice, Model: p.ConfigDrive.DeviceType + "/" + p.ConfigDrive.Bus, Dependencies: deps(p.ConfigDrive.Bus), StateOwner: "vm"})
		result.State = append(result.State, StateContract{DeviceID: "config-drive", Owner: "vm", Snapshot: true})
	}
	if p.GuestAgentEnabled() {
		result.Devices = append(result.Devices, Device{ID: "virtio-serial-0", Kind: Controller, Model: "virtio-serial"})
		result.Devices = append(result.Devices, Device{ID: "guest-agent", Kind: GuestIntegration, Model: "org.qemu.guest_agent.0", Dependencies: []string{"virtio-serial-0"}})
	}
	if p.Graphics != "none" {
		result.Devices = append(result.Devices, Device{ID: "graphics", Kind: Display, Model: p.Graphics})
	}
	return result, ValidateDevices(result.Devices)
}
func ValidateDevices(devices []Device) error {
	ids := map[string]bool{}
	for _, d := range devices {
		if d.ID == "" || ids[d.ID] {
			return fmt.Errorf("duplicate or empty device identifier %q", d.ID)
		}
		ids[d.ID] = true
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("cyclic device dependency at %s", id)
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		for _, d := range devices {
			if d.ID != id {
				continue
			}
			for _, dep := range d.Dependencies {
				if !ids[dep] {
					return fmt.Errorf("device %s requires missing dependency %s", id, dep)
				}
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		state[id] = 2
		return nil
	}
	for id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}
func DecodePlan(raw json.RawMessage) (Plan, error) {
	var p Plan
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("missing or invalid applied hardware policy; adopt or re-adopt the VM: %w", err)
	}
	if p.Version != Version {
		return p, fmt.Errorf("unsupported applied hardware policy version; re-adopt the VM")
	}
	normalized, err := Normalize(p.Profile)
	if err != nil {
		return p, err
	}
	if !reflect.DeepEqual(p.Profile, normalized) {
		return p, fmt.Errorf("applied hardware policy is incomplete; explicitly re-adopt the VM")
	}
	expected, err := Resolve(normalized, p.Emulator)
	if err != nil {
		return p, err
	}
	if !reflect.DeepEqual(p.Devices, expected.Devices) || !reflect.DeepEqual(p.State, expected.State) {
		return p, fmt.Errorf("applied device dependencies or state ownership are incomplete or incompatible; explicitly re-adopt the VM")
	}
	if p.CPUThreads < 0 || p.Profile.Hugepages && p.HugepageSizeKiB <= 4 || !p.Profile.Hugepages && p.HugepageSizeKiB != 0 {
		return p, fmt.Errorf("applied host bindings are incomplete or incompatible; explicitly re-adopt the VM")
	}
	return p, nil
}
