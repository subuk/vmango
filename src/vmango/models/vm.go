package models

import (
	"encoding/json"
	"fmt"
)

const (
	STATE_RUNNING = iota
	STATE_STOPPED = iota
	STATE_UNKNOWN = iota
)

type VirtualMachineList struct {
	machines []*VirtualMachine `json:"Machines"`
}

func (vms *VirtualMachineList) MarshalJSON() ([]byte, error) {
	return json.Marshal(vms.machines)
}

func (vms *VirtualMachineList) Active() *VirtualMachineList {
	filtered := []*VirtualMachine{}
	for _, vm := range vms.machines {
		if vm.State == STATE_RUNNING {
			filtered = append(filtered, vm)
		}
	}
	vms.machines = filtered
	return vms
}

func (vms *VirtualMachineList) Count() int {
	return len(vms.machines)
}

func (vms *VirtualMachineList) All() []*VirtualMachine {
	return vms.machines
}

func (vms *VirtualMachineList) Add(vm *VirtualMachine) {
	vms.machines = append(vms.machines, vm)
}

func (vms *VirtualMachineList) Find(name string) *VirtualMachine {
	for _, vm := range vms.machines {
		if vm.Name == name {
			return vm
		}
	}
	return nil
}

type VirtualMachineDisk struct {
	Size   uint64
	Driver string
	Type   string
}

func (disk *VirtualMachineDisk) SizeGigabytes() int {
	return int(disk.Size / 1024 / 1024 / 1024)
}

type VirtualMachine struct {
	Name      string
	State     int    `json:"-"`
	Uuid      string `json:"-"`
	Memory    int
	Cpus      int
	ImageName string `json:"-"`
	Ip        *IP
	HWAddr    string
	VNCAddr   string
	Disk      *VirtualMachineDisk
	SSHKeys   []*SSHKey
}

func (v *VirtualMachine) StateName() string {
	switch v.State {
	case STATE_RUNNING:
		return "running"
	case STATE_STOPPED:
		return "stopped"
	}
	return "unknown"
}

func (v *VirtualMachine) IsRunning() bool {
	return v.State == STATE_RUNNING
}

func (v *VirtualMachine) MemoryMegabytes() int {
	return int(v.Memory / 1024 / 1024)
}

func (v *VirtualMachine) String() string {
	return fmt.Sprintf("<VirtualMachine %s>", v.Name)
}
