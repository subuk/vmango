package libvirt

import (
	"encoding/json"
	"fmt"
	libvirtxml "libvirt.org/go/libvirtxml"
	"net"
	"net/url"
	"strings"
	"subuk/vmango/compute"
)

type creationBinding struct{ GraphicsListen string }

func (repo *VirtualMachineRepository) SnapshotCreationBinding(node string) (json.RawMessage, error) {
	settings, ok := repo.settings[node]
	if !ok {
		return nil, compute.ErrUnknownNode
	}
	if err := ValidateGraphicsListen(settings.GraphicsListen); err != nil {
		return nil, err
	}
	return json.Marshal(creationBinding{GraphicsListen: settings.GraphicsListen})
}
func (repo *VirtualMachineRepository) ApplyCreationBinding(vm *compute.VirtualMachine, raw json.RawMessage) error {
	var binding creationBinding
	if len(raw) == 0 {
		return fmt.Errorf("missing accepted graphics listener binding")
	}
	if err := json.Unmarshal(raw, &binding); err != nil {
		return err
	}
	if err := ValidateGraphicsListen(binding.GraphicsListen); err != nil {
		return err
	}
	if vm.Graphic.Type != compute.GraphicTypeNone {
		vm.Graphic.Listen = binding.GraphicsListen
	}
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

// Wildcards refer to the hypervisor's loopback for local/SSH connections.
// A non-SSH remote connection needs the hypervisor's externally reachable host.
func graphicsDialHost(listen string, uri *url.URL) (string, error) {
	if err := ValidateGraphicsListen(listen); err != nil {
		return "", err
	}
	ip := net.ParseIP(listen)
	ssh := strings.Contains(uri.Scheme, "ssh")
	if listen == "" || ip.IsUnspecified() {
		if !ssh && uri.Hostname() != "" {
			return uri.Hostname(), nil
		}
		if ip != nil && ip.To4() == nil {
			return "::1", nil
		}
		return "127.0.0.1", nil
	}
	if !ssh && uri.Hostname() != "" && ip.IsLoopback() {
		return "", fmt.Errorf("remote loopback graphics listener requires an SSH connection")
	}
	return listen, nil
}

// host is a validated IP literal; neither host nor port contains shell syntax.
func graphicsForwardCommand(host, port string) string {
	target := net.JoinHostPort(host, port)
	return "if (command -v socat) >/dev/null 2>&1; then socat - TCP:" + target + "; else nc " + host + " " + port + "; fi"
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
