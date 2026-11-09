package libvirt

import (
	"encoding/xml"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
	"testing"

	libvirtxml "libvirt.org/go/libvirtxml"
)

// Exercise the XML given to libvirt, without a daemon, connection or running VM.
func TestNewDomainConfigPlatformFeatures(t *testing.T) {
	for _, tt := range []struct {
		name, arch, machine, domain, firmware string
		wantPCFeatures                        bool
	}{
		{"x86 legacy BIOS", "x86_64", "pc-i440fx-rhel7.6.0", "kvm", "", true},
		{"x86 32-bit", "i686", "pc", "kvm", "", true},
		{"x86 Q35 UEFI", "x86_64", "q35", "kvm", "efi", true},
		{"ARM HVF", "aarch64", "virt", "hvf", "efi", false},
		{"ARM KVM", "aarch64", "virt", "kvm", "efi", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			vm := &compute.VirtualMachine{Id: "test-vm", Name: "test-vm", Arch: compute.NewArch(tt.arch), Firmware: tt.firmware}
			caps := &libvirtxml.DomainCaps{Arch: tt.arch, Machine: tt.machine, Domain: tt.domain, Path: "/test/qemu"}
			config := newDomainConfig(vm, caps)
			data, err := config.Marshal()
			if err != nil {
				t.Fatal(err)
			}
			// Decode independently of libvirtxml to check the actual XML elements.
			var domain struct {
				Features struct {
					ACPI *struct{} `xml:"acpi"`
					APIC *struct{} `xml:"apic"`
				} `xml:"features"`
				OS struct {
					Type struct {
						Arch string `xml:"arch,attr"`
					} `xml:"type"`
				} `xml:"os"`
			}
			if err := xml.Unmarshal([]byte(data), &domain); err != nil {
				t.Fatal(err)
			}
			if domain.OS.Type.Arch != tt.arch {
				t.Fatalf("guest architecture = %q, want %q", domain.OS.Type.Arch, tt.arch)
			}
			if got := domain.Features.ACPI != nil; got != tt.wantPCFeatures {
				t.Errorf("ACPI present = %t, want %t\n%s", got, tt.wantPCFeatures, data)
			}
			if got := domain.Features.APIC != nil; got != tt.wantPCFeatures {
				t.Errorf("APIC present = %t, want %t\n%s", got, tt.wantPCFeatures, data)
			}
		})
	}
}

func TestGraphicalDomainInputComesOnlyFromProfile(t *testing.T) {
	for _, arch := range []string{"aarch64", "x86_64"} {
		for _, graphics := range []compute.GraphicType{compute.GraphicTypeNone, compute.GraphicTypeVnc, compute.GraphicTypeSpice} {
			t.Run(arch+"/"+graphics.String(), func(t *testing.T) {
				vm := &compute.VirtualMachine{Graphic: compute.VirtualMachineGraphic{Type: graphics}}
				config := newDomainConfig(vm, &libvirtxml.DomainCaps{Arch: arch})
				if len(config.Devices.Inputs) != 0 || len(config.Devices.Controllers) != 0 {
					t.Fatal("legacy XML fallback injected input")
				}
				vm.AppliedPolicy = testPlan(t, hardware.Profile{ID: "input", Presentation: &hardware.Presentation{Type: graphics.String()}})
				config = newDomainConfig(vm, &libvirtxml.DomainCaps{Arch: arch})
				wantInput := graphics != compute.GraphicTypeNone
				if !wantInput {
					if len(config.Devices.Inputs) != 0 || len(config.Devices.Controllers) > 1 || len(config.Devices.Controllers) == 1 && config.Devices.Controllers[0].Model != "none" {
						t.Fatal("unexpected input devices")
					}
					return
				}
				data, err := config.Marshal()
				if err != nil {
					t.Fatal(err)
				}
				var domain struct {
					Devices struct {
						Inputs []struct {
							Type string `xml:"type,attr"`
							Bus  string `xml:"bus,attr"`
						} `xml:"input"`
						Controllers []struct {
							Type  string `xml:"type,attr"`
							Model string `xml:"model,attr"`
						} `xml:"controller"`
					} `xml:"devices"`
				}
				if err := xml.Unmarshal([]byte(data), &domain); err != nil {
					t.Fatal(err)
				}
				if len(domain.Devices.Controllers) != 1 || domain.Devices.Controllers[0].Type != "usb" || domain.Devices.Controllers[0].Model != "qemu-xhci" {
					t.Fatalf("missing USB controller: %s", data)
				}
				if len(domain.Devices.Inputs) != 2 || domain.Devices.Inputs[0].Type != "keyboard" || domain.Devices.Inputs[1].Type != "tablet" || domain.Devices.Inputs[0].Bus != "usb" || domain.Devices.Inputs[1].Bus != "usb" {
					t.Fatalf("missing USB input: %s", data)
				}
			})
		}
	}
}
