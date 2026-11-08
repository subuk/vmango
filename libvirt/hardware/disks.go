package hardware

import (
	"fmt"
	"slices"
)

var SCSIModels = []string{"virtio-scsi", "lsilogic", "lsisas1068", "vmpvscsi"}

func NormalizeDisks(d Disks) (Disks, error) {
	if d.Bus == "" {
		d.Bus = "virtio"
	}
	if !slices.Contains([]string{"virtio", "scsi", "ide", "sata"}, d.Bus) {
		return d, fmt.Errorf("bus %q is unsupported; use virtio, scsi, ide or sata (SCSI requires controller_model)", d.Bus)
	}
	if d.Bus == "scsi" {
		if !slices.Contains(SCSIModels, d.ControllerModel) {
			return d, fmt.Errorf("controller_model %q must explicitly select virtio-scsi, lsilogic, lsisas1068 or vmpvscsi", d.ControllerModel)
		}
	} else if d.ControllerModel != "" {
		return d, fmt.Errorf("controller_model is allowed only with bus=scsi")
	}
	return d, nil
}

// DiskPolicy and ConfigDrivePolicy preserve the exact meaning of older saved
// profiles. New configuration always carries a non-nil Disks policy.
func (p Profile) DiskPolicy() Disks { return Disks{Bus: p.DiskBus, ControllerModel: p.DiskController} }
func (p Profile) ConfigDrivePolicy() Disks {
	model := p.ConfigDrive.ControllerModel
	if p.Disks == nil && p.ConfigDrive.Bus == "scsi" {
		model = "virtio-scsi"
	}
	return Disks{Bus: p.ConfigDrive.Bus, ControllerModel: model}
}

func addStorageDependencies(plan *Plan) {
	ids := map[Disks]string{}
	counts := map[string]int{}
	dependency := func(d Disks) []string {
		if d.Bus == "virtio" {
			return nil
		}
		id, ok := ids[d]
		if !ok {
			id = fmt.Sprintf("%s-%d", d.Bus, counts[d.Bus])
			counts[d.Bus]++
			ids[d] = id
			model := d.ControllerModel
			if model == "" {
				model = d.Bus
			}
			plan.Devices = append(plan.Devices, Device{ID: id, Kind: Controller, Model: model})
		}
		return []string{id}
	}
	ordinary := dependency(plan.Profile.DiskPolicy())
	var config []string
	if plan.Profile.ConfigDrive.IsEnabled() {
		config = dependency(plan.Profile.ConfigDrivePolicy())
	}
	plan.Devices = append(plan.Devices, Device{ID: "ordinary-disks", Kind: DiskPolicy, Model: plan.Profile.DiskBus, Dependencies: ordinary})
	if plan.Profile.ConfigDrive.IsEnabled() {
		plan.Devices = append(plan.Devices, Device{ID: "config-drive", Kind: ConfigDriveDevice, Model: plan.Profile.ConfigDrive.DeviceType + "/" + plan.Profile.ConfigDrive.Bus, Dependencies: config, StateOwner: "vm"})
		plan.State = append(plan.State, StateContract{DeviceID: "config-drive", Owner: "vm", Snapshot: true})
	}
}

// DriveGeometry is the supported address space, not a guest performance claim.
// lsilogic uses libvirt unit as the SCSI target; other models use target/LUN.
type DriveGeometry struct {
	Channels, Targets, Units uint
	ReservedUnit             *uint
}

func Geometry(d Disks) (DriveGeometry, error) {
	switch d.Bus {
	case "ide":
		return DriveGeometry{Channels: 2, Targets: 1, Units: 2}, nil
	case "sata":
		return DriveGeometry{Channels: 1, Targets: 1, Units: 6}, nil
	case "scsi":
		switch d.ControllerModel {
		case "lsilogic":
			reserved := uint(7)
			return DriveGeometry{Channels: 1, Targets: 1, Units: 8, ReservedUnit: &reserved}, nil
		case "lsisas1068":
			return DriveGeometry{Channels: 1, Targets: 8, Units: 1}, nil
		case "vmpvscsi":
			return DriveGeometry{Channels: 1, Targets: 64, Units: 1}, nil
		case "virtio-scsi":
			return DriveGeometry{Channels: 1, Targets: 256, Units: 16384}, nil
		}
	}
	return DriveGeometry{}, fmt.Errorf("unsupported controller %s/%s", d.Bus, d.ControllerModel)
}

func (g DriveGeometry) Capacity() uint {
	count := g.Channels * g.Targets * g.Units
	if g.ReservedUnit != nil {
		count -= g.Channels * g.Targets
	}
	return count
}
func (g DriveGeometry) Valid(bus, target, unit uint) bool {
	return bus < g.Channels && target < g.Targets && unit < g.Units && (g.ReservedUnit == nil || unit != *g.ReservedUnit)
}

type StorageObservation struct {
	Controllers []StorageController `json:"controllers"`
	Disks       []StorageDisk       `json:"disks"`
}
type StorageController struct {
	Type  string `json:"type"`
	Model string `json:"model,omitempty"`
	Index uint   `json:"index"`
}
type StorageDisk struct {
	Target          string       `json:"target"`
	Alias           string       `json:"alias,omitempty"`
	Device          string       `json:"device"`
	Bus             string       `json:"bus"`
	ControllerModel string       `json:"controller_model,omitempty"`
	Address         *DiskAddress `json:"address,omitempty"`
}
type DiskAddress struct {
	Type       string `json:"type"`
	Controller uint   `json:"controller"`
	Bus        uint   `json:"bus"`
	Target     uint   `json:"target"`
	Unit       uint   `json:"unit"`
	Slot       uint   `json:"slot,omitempty"`
	Function   uint   `json:"function,omitempty"`
}
