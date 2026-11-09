package libvirt

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"runtime"
	"strings"
	"subuk/vmango/compute"
	"time"

	"github.com/google/uuid"
	x "libvirt.org/go/libvirtxml"
)

// QEMU's stream backend owns its Unix connection from startup to exit. This
// replaces the wrapper's inherited fd=3 contract; no shared service is managed.
// Socket-vmnet integration is intentionally untested in KRG-126.
func (repo *VirtualMachineRepository) attachSocketVMNet(c *x.Domain, nic *compute.VirtualMachineAttachedInterface) error {
	if nic.Model != "virtio" {
		return fmt.Errorf("socket-vmnet requires the profile's virtio NIC model")
	}
	id := "vmango-" + uuid.NewString()
	backend, err := json.Marshal(map[string]any{"type": "stream", "id": id, "server": false, "addr": map[string]string{"type": "unix", "path": nic.Binding.Socket}})
	if err != nil {
		return err
	}
	model := "virtio-net-device"
	if c.OS.Type.Arch == "x86_64" {
		model = "virtio-net-pci"
	}
	if c.QEMUCommandline == nil {
		c.QEMUCommandline = &x.DomainQEMUCommandline{}
	}
	for _, arg := range []string{"-netdev", string(backend), "-device", model + ",netdev=" + id + ",id=" + id + ",mac=" + nic.Mac} {
		c.QEMUCommandline.Args = append(c.QEMUCommandline.Args, x.DomainQEMUCommandlineArg{Value: arg})
	}
	return nil
}

func (repo *VirtualMachineRepository) validateSocketVMNet(node, path string) error {
	fail := func(message string) error { return &compute.ValidationError{Field: "network", Message: message} }
	uri, err := url.Parse(repo.pool.nodeUri[node])
	if err != nil || runtime.GOOS != "darwin" || uri.Scheme != "qemu" || uri.Host != "" || uri.Path != "/session" {
		return fail("socket-vmnet currently requires a local macOS qemu:///session connection so socket access can be checked as the QEMU user; remote/system connections are unsupported.")
	}
	conn, err := repo.pool.Acquire(node)
	if err != nil {
		return err
	}
	version, err := conn.GetVersion()
	repo.pool.Release(node)
	if err != nil {
		return err
	}
	if version < 7002000 {
		return fail("socket-vmnet requires QEMU 7.2 or later with the native stream Unix-socket backend.")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return fail(fmt.Sprintf("socket-vmnet socket %s is unavailable; configure and start the administrator-managed service on the hypervisor host.", path))
	}
	connection, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return fail(fmt.Sprintf("socket-vmnet socket %s is inaccessible to the QEMU session user; check service state and socket/directory permissions.", path))
	}
	return connection.Close()
}

func rawBackendType(raw string) string {
	if strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return parseQemuDeviceArg(raw)[""]
	}
	return strings.SplitN(raw, ",", 2)[0]
}
