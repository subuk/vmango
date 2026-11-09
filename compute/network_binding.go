package compute

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	NetworkLibvirt     = "libvirt-network"
	NetworkBridge      = "bridge"
	NetworkUser        = "qemu-user"
	NetworkSocketVMNet = "socket-vmnet"
)

// NetworkBinding describes host infrastructure, independently of guest NIC policy.
type NetworkBinding struct {
	Type    string `json:"type"`
	Network string `json:"network,omitempty"`
	Bridge  string `json:"bridge,omitempty"`
	Socket  string `json:"socket,omitempty"`
}

func (b NetworkBinding) Kind() string {
	if b.Type == "" {
		return NetworkLibvirt
	}
	return b.Type
}

func (b NetworkBinding) Target() string {
	switch b.Kind() {
	case NetworkBridge:
		return b.Bridge
	case NetworkSocketVMNet:
		return b.Socket
	case NetworkUser:
		return ""
	default:
		return b.Network
	}
}

func (b NetworkBinding) Validate() error {
	valid := false
	switch b.Kind() {
	case NetworkLibvirt:
		valid = b.Network != "" && b.Bridge == "" && b.Socket == ""
	case NetworkBridge:
		valid = b.Bridge != "" && b.Network == "" && b.Socket == "" && !strings.ContainsAny(b.Bridge, "/\t\r\n ")
	case NetworkUser:
		valid = b.Network == "" && b.Bridge == "" && b.Socket == ""
	case NetworkSocketVMNet:
		valid = filepath.IsAbs(b.Socket) && !strings.ContainsAny(b.Socket, "\x00\r\n") && b.Network == "" && b.Bridge == ""
	}
	if !valid {
		return fmt.Errorf("%w: binding type %q requires only its matching field (libvirt-network: network; bridge: bridge; qemu-user: none; socket-vmnet: absolute socket path)", ErrInvalid, b.Kind())
	}
	return nil
}

func (b NetworkBinding) Interface(logical string) *VirtualMachineAttachedInterface {
	b.Type = b.Kind()
	return &VirtualMachineAttachedInterface{Binding: &b, LogicalNetwork: logical, NetworkName: b.Target(), Bridge: b.Kind() == NetworkBridge}
}

func (nic *VirtualMachineAttachedInterface) BindingKind() string {
	if nic.Binding != nil {
		return nic.Binding.Kind()
	}
	if nic.Bridge {
		return NetworkBridge
	}
	return NetworkLibvirt
}
