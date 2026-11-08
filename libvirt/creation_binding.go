package libvirt

import (
	"encoding/json"
	"fmt"
	libvirtxml "libvirt.org/go/libvirtxml"
	"net"
	"net/url"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
)

type creationBinding struct {
	FirmwareProfiles []hardware.FirmwareHostBinding
	GraphicsListen   string
	Emulator         string
}

func (repo *VirtualMachineRepository) SnapshotCreationBinding(node string) (json.RawMessage, error) {
	settings, ok := repo.settings[node]
	if !ok {
		return nil, compute.ErrUnknownNode
	}
	return json.Marshal(creationBinding{GraphicsListen: "127.0.0.1", Emulator: settings.Emulator, FirmwareProfiles: settings.FirmwareProfiles})
}
func (repo *VirtualMachineRepository) ApplyCreationBinding(vm *compute.VirtualMachine, raw json.RawMessage) error {
	var binding creationBinding
	if len(raw) == 0 {
		return fmt.Errorf("missing accepted graphics listener binding")
	}
	if err := json.Unmarshal(raw, &binding); err != nil {
		return err
	}
	vm.Graphic.Listen = ""
	if vm.Graphic.Type != compute.GraphicTypeNone {
		vm.Graphic.Listen = "127.0.0.1"
	}
	vm.Graphic.BindingResolved = true
	return nil
}

// ValidateGraphicsListen accepts literal addresses so forwarding never embeds
// arbitrary configuration or domain XML text in a remote shell command.
func ValidateGraphicsListen(address string) error {
	if address != "" && net.ParseIP(address) == nil {
		return fmt.Errorf("graphics_listen must be empty or an IP address")
	}
	return nil
}

// Existing listeners are preserved. A wildcard is dialed through host loopback;
// every remote transport reaches that address through SSH, never direct VNC.
func graphicsDialHost(listen string, uri *url.URL) (string, error) {
	if err := ValidateGraphicsListen(listen); err != nil {
		return "", err
	}
	ip := net.ParseIP(listen)
	if listen == "" || ip.IsUnspecified() {
		if ip != nil && ip.To4() == nil {
			return "::1", nil
		}
		return "127.0.0.1", nil
	}
	return listen, nil
}

func graphicsListenerAddress(legacy string, listeners []libvirtxml.DomainGraphicListener) string {
	for _, listener := range listeners {
		if listener.Address != nil && listener.Address.Address != "" {
			return listener.Address.Address
		}
		if listener.Network != nil && listener.Network.Address != "" {
			return listener.Network.Address
		}
	}
	return legacy
}
