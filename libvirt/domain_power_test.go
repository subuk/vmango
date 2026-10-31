package libvirt

import (
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"testing"

	"github.com/rs/zerolog"
	vir "libvirt.org/go/libvirt"
	"libvirt.org/go/libvirtxml"
)

func TestDomainPowerPolicy(t *testing.T) {
	for _, arch := range []string{"x86_64", "i686", "aarch64", "ppc64le"} {
		for _, features := range []string{"", "<features/>", `<features><pae/><apic eoi="on"/><hyperv mode="custom"><relaxed state="on"/></hyperv></features>`} {
			t.Run(arch+features, func(t *testing.T) {
				config := &libvirtxml.Domain{}
				if err := config.Unmarshal(`<domain type="kvm"><name>external</name><os><type arch="` + arch + `">hvm</type></os>` + features + `<on_poweroff>restart</on_poweroff><on_reboot>destroy</on_reboot><on_crash>restart</on_crash><devices><disk type="file" device="disk"><source file="/existing/root.qcow2"/><target dev="vda" bus="virtio"/></disk></devices></domain>`); err != nil {
					t.Fatal(err)
				}
				beforeDevices := config.Devices
				beforeFeatures := config.Features
				applyDomainPowerPolicy(config)
				if config.OnPoweroff != "destroy" || config.OnReboot != "restart" || config.OnCrash != "destroy" {
					t.Fatalf("unexpected power policy: %+v", config)
				}
				if arch == "x86_64" || arch == "i686" {
					if config.Features == nil || config.Features.ACPI == nil || config.Features.APIC == nil {
						t.Fatal("missing x86 power management features")
					}
					if features != "" && features != "<features/>" && (config.Features.APIC.EOI != "on" || config.Features.PAE == nil || config.Features.HyperV.Relaxed.State != "on") {
						t.Fatal("existing platform features were replaced")
					}
				} else if config.Features != beforeFeatures {
					t.Fatal("non-x86 platform features changed")
				}
				if config.Devices != beforeDevices {
					t.Fatal("power policy replaced devices")
				}
				once, err := config.Marshal()
				if err != nil {
					t.Fatal(err)
				}
				applyDomainPowerPolicy(config)
				twice, err := config.Marshal()
				if err != nil || once != twice {
					t.Fatal("power policy is not idempotent", err)
				}
			})
		}
	}
}

func TestAdoptionNormalizesPersistentPowerSettings(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "stopped", true: "running"}[running], func(t *testing.T) {
			pool := NewConnectionPool(map[string]string{"n": "test:///default"}, []string{"n"}, zerolog.Nop())
			conn, err := pool.Acquire("n")
			if err != nil {
				t.Skipf("optional libvirt test driver unavailable: %v", err)
			}
			defer conn.Close()
			pool.Release("n")
			domain, err := conn.LookupDomainByName("test")
			if err != nil {
				t.Fatal(err)
			}
			defer domain.Free()
			id, err := domain.GetUUIDString()
			if err != nil {
				t.Fatal(err)
			}
			if !running {
				if err := domain.Destroy(); err != nil {
					t.Fatal(err)
				}
			}
			read := func(flags vir.DomainXMLFlags) *libvirtxml.Domain {
				t.Helper()
				xml, err := domain.GetXMLDesc(flags)
				if err != nil {
					t.Fatal(err)
				}
				config := &libvirtxml.Domain{}
				if err := config.Unmarshal(xml); err != nil {
					t.Fatal(err)
				}
				return config
			}
			flags := vir.DOMAIN_XML_INACTIVE | vir.DOMAIN_XML_SECURE
			config := read(flags)
			config.OS.Type.Arch = "x86_64"
			config.Features = nil
			config.OnPoweroff, config.OnReboot, config.OnCrash = "restart", "restart", "restart"
			config.Metadata = &libvirtxml.DomainMetadata{XML: `<owner xmlns="https://example.org/owner">external</owner>`}
			config.Devices.Graphics = []libvirtxml.DomainGraphic{{VNC: &libvirtxml.DomainGraphicVNC{Port: -1, AutoPort: "yes", Listen: "127.0.0.2", Passwd: "keep-secret"}}}
			xml, err := config.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			defined, err := conn.DomainDefineXML(xml)
			if err != nil {
				t.Fatal(err)
			}
			defined.Free()
			if err := domain.SetAutostart(true); err != nil {
				t.Fatal(err)
			}
			before := read(flags)
			liveBefore := read(vir.DOMAIN_XML_SECURE)
			repo := NewVirtualMachineRepository(pool, map[string]NodeSettings{"n": {}}, zerolog.Nop())
			metadata := &compute.MachineMetadata{Cloud: "c", Zone: "z", CPURequest: 2, MemoryRequestBytes: 1 << 30}
			if err := repo.Adopt(id, "n", metadata); err != nil {
				t.Fatal(err)
			}
			after := read(flags)
			applyDomainPowerPolicy(before)
			if err := writeSchedulerMetadata(before, metadata); err != nil {
				t.Fatal(err)
			}
			observed, err := readSchedulerMetadata(after)
			if err != nil || !reflect.DeepEqual(metadata, observed) || !strings.Contains(after.Metadata.XML, `<owner xmlns="https://example.org/owner">external</owner>`) {
				t.Fatal("adoption lost management or unrelated metadata", err)
			}
			// Libvirt reformats metadata whitespace on define.
			before.Metadata, after.Metadata = nil, nil
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("adoption changed unrelated configuration\nwant: %+v\ngot: %+v", before, after)
			}
			if active, err := domain.IsActive(); err != nil || active != running {
				t.Fatal("adoption changed power state", err)
			}
			if autostart, err := domain.GetAutostart(); err != nil || !autostart {
				t.Fatal("adoption changed autostart", err)
			}
			if running && !reflect.DeepEqual(liveBefore, read(vir.DOMAIN_XML_SECURE)) {
				t.Fatal("adoption changed running configuration")
			}
		})
	}
}
