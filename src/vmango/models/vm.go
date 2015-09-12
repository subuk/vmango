package models

import (
	"fmt"
)

const (
	STATE_RUNNING = iota
	STATE_STOPPED = iota
	STATE_UNKNOWN = iota
)

type VirtualMachine struct {
	Name      string `json:"name"`
	State     int    `json:"-"`
	Uuid      string `json:"-"`
	Memory    int    `json:"memory"`
	Cpus      int    `json:"cpus"`
	ImageName string `json:"image_name"`
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

func (v *VirtualMachine) MemoryMegabytes() int {
	return int(v.Memory / 1024)
}

func (v *VirtualMachine) String() string {
	return fmt.Sprintf("<VirtualMachine %s>", v.Name)
}
