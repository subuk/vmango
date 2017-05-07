package models

import (
	"fmt"
	"strings"
)

const (
	STATE_UNKNOWN  = iota
	STATE_RUNNING  = iota
	STATE_STOPPED  = iota
	STATE_STOPPING = iota
	STATE_STARTING = iota
)

type VirtualMachineList []*VirtualMachine

func (vms *VirtualMachineList) Active() *VirtualMachineList {
	filtered := VirtualMachineList{}
	for _, vm := range *vms {
		if vm.State == STATE_RUNNING {
			filtered = append(filtered, vm)
		}
	}
	return &filtered
}

func (vms *VirtualMachineList) Count() int {
	return len(*vms)
}

func (vms *VirtualMachineList) All() []*VirtualMachine {
	return *vms
}

func (vms *VirtualMachineList) Add(vm *VirtualMachine) {
	*vms = append(*vms, vm)
}

func (vms *VirtualMachineList) Find(name string) *VirtualMachine {
	for _, vm := range *vms {
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
	Id       string
	Name     string
	Plan     string
	Creator  string
	Userdata string `json:"-"`
	OS       string
	Arch     HWArch
	State    int `json:"-"`
	Memory   int
	Cpus     int
	ImageId  string
	Ip       *IP
	HWAddr   string
	VNCAddr  string
	RootDisk *VirtualMachineDisk
	SSHKeys  SSHKeyList
}

func (v *VirtualMachine) StateName() string {
	switch v.State {
	case STATE_RUNNING:
		return "running"
	case STATE_STOPPING:
		return "stopping"
	case STATE_STARTING:
		return "starting"
	case STATE_STOPPED:
		return "stopped"
	}
	return "unknown"
}

func (v *VirtualMachine) IsRunning() bool {
	return v.State == STATE_RUNNING
}

func (v *VirtualMachine) IsStopping() bool {
	return v.State == STATE_STOPPING
}

func (v *VirtualMachine) IsStarting() bool {
	return v.State == STATE_STARTING
}

func (v *VirtualMachine) MemoryMegabytes() int {
	return int(v.Memory / 1024 / 1024)
}

func (v *VirtualMachine) String() string {
	return fmt.Sprintf("<VirtualMachine %s>", v.Name)
}

func (v *VirtualMachine) HasUserdata() bool {
	return strings.TrimSpace(v.Userdata) != ""
}
