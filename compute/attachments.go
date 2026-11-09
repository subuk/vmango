package compute

import (
	"fmt"
	"reflect"
	"sort"
)

const AttachmentVersion = 1
const PurposeConfigDrive = "config-drive"

// AttachmentMetadata records applied identity and ownership, never catalog guesses.
type AttachmentMetadata struct {
	Version     int                 `json:"version"`
	ConfigDrive string              `json:"config_drive"` // Empty explicitly means no config drive.
	Disks       []DiskAttachment    `json:"disks"`
	Networks    []NetworkAttachment `json:"networks"`
}
type DiskAttachment struct {
	ID      string     `json:"id"`
	Source  string     `json:"source"`
	Device  DeviceType `json:"device"`
	Bus     DeviceBus  `json:"bus"`
	Format  string     `json:"format"`
	Purpose string     `json:"purpose"`
	Managed bool       `json:"managed"`
}
type NetworkAttachment struct {
	ID             string         `json:"id"`
	MAC            string         `json:"mac"`
	Model          string         `json:"model"`
	LogicalNetwork string         `json:"logical_network"`
	Binding        NetworkBinding `json:"binding"`
	Observation    string         `json:"observation"`
	Managed        bool           `json:"managed"`
}

// MachineAttachmentRepository validates explicit adoption selections without writes.
type MachineAttachmentRepository interface {
	PrepareAdoptionAttachments(*VirtualMachine, *AttachmentMetadata) error
}

func DiskRecord(d *VirtualMachineAttachedVolume) DiskAttachment {
	purpose := d.Purpose
	if purpose == "" {
		purpose = "disk"
	}
	return DiskAttachment{d.DeviceID, d.Path, d.DeviceType, d.DeviceBus, d.Format, purpose, d.Managed}
}

func ValidateAttachments(vm *VirtualMachine, m *AttachmentMetadata) error {
	invalid := func() error {
		return &AdoptionRequiredError{Message: "Attachment metadata is missing, unsupported or differs from actual devices; explicitly adopt or re-adopt the VM without changing its hardware."}
	}
	if m == nil || m.Version != AttachmentVersion || len(m.Disks) != len(vm.Volumes) || len(m.Networks) != len(vm.Interfaces) {
		return invalid()
	}
	seen := map[string]bool{}
	configFound := m.ConfigDrive == ""
	for _, r := range m.Disks {
		if r.ID == "" || seen["disk:"+r.ID] || r.Source == "" {
			return invalid()
		}
		seen["disk:"+r.ID] = true
		found := false
		for _, d := range vm.Volumes {
			if d.DeviceID == r.ID && d.Path == r.Source && d.DeviceType == r.Device && d.DeviceBus == r.Bus && d.Format == r.Format {
				found = true
			}
		}
		if !found || (r.Purpose != "disk" && r.Purpose != PurposeConfigDrive) || (r.Purpose == PurposeConfigDrive) != (m.ConfigDrive == r.ID) {
			return invalid()
		}
		if r.ID == m.ConfigDrive {
			configFound = true
		}
	}
	if !configFound {
		return invalid()
	}
	for _, r := range m.Networks {
		if r.ID == "" || seen["nic:"+r.ID] || r.LogicalNetwork == "" || r.Binding.Validate() != nil {
			return invalid()
		}
		seen["nic:"+r.ID] = true
		found := false
		for _, n := range vm.Interfaces {
			if n.DeviceID == r.ID && n.Mac == r.MAC && n.Model == r.Model && n.Observation == r.Observation {
				found = true
			}
		}
		if !found {
			return invalid()
		}
	}
	return nil
}

func ApplyAttachmentMetadata(vm *VirtualMachine, m *AttachmentMetadata) error {
	if err := ValidateAttachments(vm, m); err != nil {
		return err
	}
	for _, r := range m.Disks {
		for _, d := range vm.Volumes {
			if d.DeviceID == r.ID {
				d.Purpose, d.Managed = r.Purpose, r.Managed
			}
		}
	}
	for _, r := range m.Networks {
		for _, n := range vm.Interfaces {
			if n.DeviceID == r.ID {
				b := r.Binding
				n.Binding, n.LogicalNetwork, n.Managed = &b, r.LogicalNetwork, r.Managed
			}
		}
	}
	return nil
}

// AdoptionAttachments matches current infrastructure only during explicit adoption.
// Existing valid ownership records survive re-adoption; newly designated disks are external.
func AdoptionAttachments(vm *VirtualMachine, host CloudHost, configDrive *string, choices map[string]string) (*AttachmentMetadata, error) {
	if configDrive == nil {
		return nil, invalid("config_drive", "Select an attached config drive or explicitly choose No config drive.")
	}
	m := &AttachmentMetadata{Version: AttachmentVersion, ConfigDrive: *configDrive, Disks: []DiskAttachment{}, Networks: []NetworkAttachment{}}
	configFound := *configDrive == ""
	for _, d := range vm.Volumes {
		r := DiskRecord(d)
		r.Purpose = "disk"
		if d.DeviceID == *configDrive {
			r.Purpose = PurposeConfigDrive
			configFound = true
		}
		m.Disks = append(m.Disks, r)
	}
	if !configFound {
		return nil, invalid("config_drive", "The selected config drive is not attached; reload the machine and select an existing disk.")
	}
	for _, n := range vm.Interfaces {
		matches := []string{}
		// An inherited socket FD does not reveal its service path in domain XML.
		// Only explicit administrator confirmation can supply that binding.
		confirmService := n.RawBackend && n.Binding != nil && n.Binding.Kind() == NetworkSocketVMNet && n.Binding.Socket == ""
		for id, b := range host.Networks {
			if n.Binding != nil {
				a := *n.Binding
				a.Type = a.Kind()
				b.Type = b.Kind()
				if reflect.DeepEqual(a, b) || confirmService && b.Kind() == NetworkSocketVMNet {
					matches = append(matches, id)
				}
			}
		}
		sort.Strings(matches)
		selected := choices[n.DeviceID]
		if selected == "" && len(matches) == 1 && !confirmService {
			selected = matches[0]
		}
		valid := false
		for _, id := range matches {
			valid = valid || id == selected
		}
		if !valid {
			return nil, invalid("networks", fmt.Sprintf("Interface %s (%s): configure a matching network binding on host %s; select explicitly if multiple logical networks match (%v).", n.DeviceID, n.Mac, host.Node, matches))
		}
		b := host.Networks[selected]
		b.Type = b.Kind()
		m.Networks = append(m.Networks, NetworkAttachment{n.DeviceID, n.Mac, n.Model, selected, b, n.Observation, true})
	}
	if err := ValidateAttachments(vm, m); err != nil {
		return nil, err
	}
	return m, nil
}
