package libvirt

import (
	"fmt"
	xmlmodel "libvirt.org/go/libvirtxml"
	"reflect"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
	"testing"
)

func TestMachineResolutionExactContextAndCandidates(t *testing.T) {
	for _, target := range []string{"pc-q35-8.2", "pc-q35-9.2"} {
		for _, requested := range []string{"auto", "q35", target} {
			p, _ := hardware.Normalize(hardware.Profile{ID: "p", Machine: &hardware.Machine{Type: requested, Acceleration: "kvm"}})
			calls := [][4]string{}
			query := func(emulator, arch, machine, domain string) (*xmlmodel.DomainCaps, error) {
				calls = append(calls, [4]string{emulator, arch, machine, domain})
				if arch != "x86_64" || domain != "kvm" || emulator != "/custom/qemu" {
					return nil, fmt.Errorf("wrong context")
				}
				if machine != "" && machine != "q35" && machine != target {
					return nil, fmt.Errorf("unsupported machine %s", machine)
				}
				return &xmlmodel.DomainCaps{Path: emulator, Arch: arch, Domain: domain, Machine: target}, nil
			}
			c, err := resolveMachineCapabilities(p, "x86_64", "/custom/qemu", &xmlmodel.Caps{}, query, nil)
			if err != nil || c.Machine != target || len(calls) != 2 || calls[1][2] != target {
				t.Fatalf("%v: %+v %v", calls, c, err)
			}
		}
	}
}
func TestMachineResolutionNoSoftwareFallbackOrSubstitution(t *testing.T) {
	p, _ := hardware.Normalize(hardware.Profile{ID: "p", Machine: &hardware.Machine{}})
	domains := []string{}
	query := func(emulator, arch, machine, domain string) (*xmlmodel.DomainCaps, error) {
		domains = append(domains, domain)
		return nil, fmt.Errorf("unavailable")
	}
	if _, err := resolveMachineCapabilities(p, "x86_64", "", &xmlmodel.Caps{}, query, nil); err == nil {
		t.Fatal("missing hardware accepted")
	}
	if !reflect.DeepEqual(domains, []string{"kvm", "hvf"}) {
		t.Fatal("software fallback", domains)
	}
	for _, domain := range []string{"kvm", "hvf", "qemu"} {
		p.Machine.Acceleration = domain
		domains = nil
		if _, err := resolveMachineCapabilities(p, "x86_64", "", &xmlmodel.Caps{}, query, nil); err == nil || !reflect.DeepEqual(domains, []string{domain}) {
			t.Fatal(domains, err)
		}
	}
	p.Machine.Acceleration = "kvm"
	wrong := func(_, arch, _, _ string) (*xmlmodel.DomainCaps, error) {
		return &xmlmodel.DomainCaps{Path: "/wrong", Arch: arch, Machine: "pc-q35-9.2", Domain: "qemu"}, nil
	}
	if _, err := resolveMachineCapabilities(p, "x86_64", "/explicit", &xmlmodel.Caps{}, wrong, nil); err == nil {
		t.Fatal("substitution accepted")
	}
}
func TestMachineAliasesRespectDriverContext(t *testing.T) {
	var host xmlmodel.Caps
	if err := host.Unmarshal(`<capabilities><guest><os_type>hvm</os_type><arch name="x86_64"><emulator>/default</emulator><machine canonical="pc-8">pc</machine><domain type="qemu"/><domain type="kvm"><emulator>/kvm</emulator><machine canonical="pc-9">pc</machine></domain></arch></guest></capabilities>`); err != nil {
		t.Fatal(err)
	}
	if got := machineAliases(&host, "x86_64", "kvm", "/kvm")["pc"]; got != "pc-9" {
		t.Fatal(got)
	}
	if got := machineAliases(&host, "x86_64", "qemu", "/default")["pc"]; got != "pc-8" {
		t.Fatal(got)
	}
	if len(machineAliases(&host, "aarch64", "kvm", "/kvm")) != 0 || len(machineAliases(&host, "x86_64", "kvm", "/custom")) != 0 {
		t.Fatal("mapping leaked across context")
	}
}
func TestMachineResolutionValidatesWholeProfile(t *testing.T) {
	p, _ := hardware.Normalize(hardware.Profile{ID: "p", Machine: &hardware.Machine{Acceleration: "kvm"}})
	query := func(_, arch, _, domain string) (*xmlmodel.DomainCaps, error) {
		return &xmlmodel.DomainCaps{Path: "/qemu", Arch: arch, Machine: "pc-q35-9.2", Domain: domain}, nil
	}
	checked := false
	_, err := resolveMachineCapabilities(p, "x86_64", "", &xmlmodel.Caps{}, query, func(*xmlmodel.DomainCaps) error { checked = true; return fmt.Errorf("firmware unsupported") })
	if err == nil || !checked {
		t.Fatal("machine presence bypassed profile validation")
	}
}

func TestUnknownAndUnavailableExactMachineFailWithoutReselection(t *testing.T) {
	for _, requested := range []string{"unknown", "pc-q35-8.2"} {
		p, _ := hardware.Normalize(hardware.Profile{ID: "p", Machine: &hardware.Machine{Type: requested, Acceleration: "kvm"}})
		calls := 0
		_, err := resolveMachineCapabilities(p, "x86_64", "/saved", &xmlmodel.Caps{}, func(emulator, arch, machine, domain string) (*xmlmodel.DomainCaps, error) {
			calls++
			if machine != requested || emulator != "/saved" || domain != "kvm" {
				t.Fatal("substituted request")
			}
			return nil, fmt.Errorf("requested machine no longer supported")
		}, nil)
		if err == nil || calls != 1 {
			t.Fatalf("reselected unavailable exact machine: calls=%d, %v", calls, err)
		}
	}
}

func TestCreationRejectsMissingMaximumCPUInExactCombination(t *testing.T) {
	p, _ := hardware.Normalize(hardware.Profile{ID: "headless", Graphics: "none", GuestAgent: boolPointer(false), ConfigDrive: hardware.ConfigDrive{Enabled: boolPointer(false)}})
	var caps xmlmodel.DomainCaps
	if err := caps.Unmarshal(`<domainCapabilities><path>/qemu</path><arch>x86_64</arch><domain>kvm</domain><machine>pc-q35-9.2</machine><cpu><mode name="maximum" supported="no"/></cpu><devices><disk supported="yes"><enum name="bus"><value>virtio</value></enum></disk></devices></domainCapabilities>`); err != nil {
		t.Fatal(err)
	}
	vm := &compute.VirtualMachine{VCpus: 1}
	if err := validateCreationCapabilities(vm, p, &caps); err == nil {
		t.Fatal("CPU policy weakened")
	}
	caps.CPU.Modes[0].Supported = "yes"
	if err := validateCreationCapabilities(vm, p, &caps); err != nil {
		t.Fatal(err)
	}
}
