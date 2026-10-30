package libvirt

import (
	libvirtxml "libvirt.org/go/libvirtxml"
	"net/url"
	"strings"
	"subuk/vmango/compute"
	"testing"
)

func TestCreationBindingPinsListener(t *testing.T) {
	repo := &VirtualMachineRepository{settings: map[string]NodeSettings{"host": {GraphicsListen: "::1"}}}
	binding, err := repo.SnapshotCreationBinding("host")
	if err != nil {
		t.Fatal(err)
	}
	repo.settings["host"] = NodeSettings{GraphicsListen: "0.0.0.0"}
	vm := &compute.VirtualMachine{Graphic: compute.VirtualMachineGraphic{Type: compute.GraphicTypeVnc}}
	if err := repo.ApplyCreationBinding(vm, binding); err != nil || vm.Graphic.Listen != "::1" {
		t.Fatalf("binding drift: %+v %v", vm, err)
	}
	if err := repo.ApplyCreationBinding(vm, nil); err == nil {
		t.Fatal("missing binding silently read current config")
	}
	for _, address := range []string{"host; touch /tmp/no", "localhost", "127.0.0.1:5900"} {
		if ValidateGraphicsListen(address) == nil {
			t.Fatalf("unsafe listener accepted: %s", address)
		}
	}
}
func TestConsoleListenerTargets(t *testing.T) {
	for _, tt := range []struct{ uri, listen, want string }{
		{"qemu:///session", "", "127.0.0.1"},
		{"qemu:///system", "0.0.0.0", "127.0.0.1"},
		{"qemu:///system", "::", "::1"},
		{"qemu+ssh://user@host/system", "0.0.0.0", "127.0.0.1"},
		{"qemu+ssh://user@host/system", "::1", "::1"},
		{"qemu+ssh://user@host/system", "192.0.2.8", "192.0.2.8"},
		{"qemu+tls://host/system", "0.0.0.0", "host"},
		{"qemu+tls://[2001:db8::1]/system", "::", "2001:db8::1"},
	} {
		u, _ := url.Parse(tt.uri)
		got, err := graphicsDialHost(tt.listen, u)
		if err != nil || got != tt.want {
			t.Fatalf("%s listen=%s: %s %v", tt.uri, tt.listen, got, err)
		}
	}
	cmd := graphicsForwardCommand("::1", "5900")
	if !strings.Contains(cmd, "TCP:[::1]:5900") || !strings.Contains(cmd, "nc ::1 5900") {
		t.Fatal(cmd)
	}
}

func TestRemoteLoopbackCannotDialVmangoHost(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "127.0.0.2", "::1"} {
		u, _ := url.Parse("qemu+tls://hypervisor/system")
		if _, err := graphicsDialHost(address, u); err == nil {
			t.Fatalf("remote listener %s could dial local machine", address)
		}
	}
}
func TestNestedGraphicsListenerAddress(t *testing.T) {
	for _, xml := range []string{
		`<graphics type="vnc" port="5901"><listen type="address" address="::1"/></graphics>`,
		`<graphics type="vnc" port="5901"><listen type="network" network="console" address="::1"/></graphics>`,
	} {
		var graphic libvirtxml.DomainGraphic
		if err := graphic.Unmarshal(xml); err != nil {
			t.Fatal(err)
		}
		if got := graphicsListenerAddress(graphic.VNC.Listen, graphic.VNC.Listeners); got != "::1" {
			t.Fatalf("nested binding lost: %s", got)
		}
	}
}
