package models

import (
	"fmt"
)

type VirtualMachine struct {
	Name   string
	State  uint8
	Uuid   string
	Memory uint64
}

func (v *VirtualMachine) StateName() string {
	switch v.State {
	case 1:
		return "running"
	case 5:
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
