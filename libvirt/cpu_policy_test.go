package libvirt

import (
	"encoding/json"
	x "libvirt.org/go/libvirtxml"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
	"testing"
)

func TestCPUCreationXMLAndPreservingResize(t *testing.T) {
	yes, no := true, false
	p, err := hardware.Normalize(hardware.Profile{ID: "cpu", CPU: &hardware.CPU{Mode: "custom", ModeExplicit: true, Model: "model-from-backend", Vendor: "Intel", Features: []hardware.CPUFeature{{Name: "aes", Policy: "require"}, {Name: "avx2", Policy: "disable"}}}, Placement: &hardware.NativePlacement{Mode: "auto"}, HyperV: &hardware.HyperV{Relaxed: &yes, VPIndex: &yes, Synic: &yes, STimer: &hardware.STimer{}, Spinlocks: &hardware.Spinlocks{}}, KVM: &hardware.KVM{Hidden: &no, PollControl: &yes}, Timers: []hardware.Timer{{Name: "hypervclock", Present: &yes}}})
	if err != nil {
		t.Fatal(err)
	}
	c := &x.Domain{Devices: &x.DomainDeviceList{}}
	applyCPUPolicy(c, p)
	applyDomainResources(c, &compute.VirtualMachine{VCpus: 8, Memory: compute.NewSize(2, compute.SizeUnitG)}, &x.Caps{Host: x.CapsHost{CPU: &x.CapsHostCPU{Topology: &x.CapsHostCPUTopology{Threads: 2}}}})
	raw, _ := c.Marshal()
	for _, want := range []string{`fallback="forbid"`, `match="exact"`, `<vendor>Intel</vendor>`, `cores="8"`, `threads="1"`, `sockets="1"`, `placement="auto"`, `mode="strict"`, `retries="4096"`, `<hidden state="off"`, `<poll-control state="on"`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("missing %s: %s", want, raw)
		}
	}
	if c.CPU.Numa != nil {
		t.Fatal("invented guest NUMA")
	}
	if err = validateCPUAdoption(c, p); err != nil {
		t.Fatal(err)
	}
	beforeCPU := *c.CPU
	beforeCPU.Topology = nil
	features, _ := json.Marshal(c.Features)
	placement, _ := json.Marshal(c.NUMATune)
	if err = validateResourceTopology(c, 16); err != nil {
		t.Fatal(err)
	}
	applyDomainPolicy(c, &compute.VirtualMachine{VCpus: 16, Memory: compute.NewSize(4, compute.SizeUnitG)}, &x.Caps{}, true)
	afterCPU := *c.CPU
	afterCPU.Topology = nil
	a, _ := json.Marshal(beforeCPU)
	b, _ := json.Marshal(afterCPU)
	featuresAfter, _ := json.Marshal(c.Features)
	placementAfter, _ := json.Marshal(c.NUMATune)
	if string(a) != string(b) || string(features) != string(featuresAfter) || string(placement) != string(placementAfter) || c.CPU.Topology.Cores != 16 || c.VCPU.Placement != "auto" {
		t.Fatal("resize changed policy")
	}
	c.CPUTune = &x.DomainCPUTune{VCPUPin: []x.DomainCPUTuneVCPUPin{{VCPU: 0, CPUSet: "1"}}}
	if err = validateCPUAdoption(c, p); err == nil {
		t.Fatal("auto adoption discarded affinity")
	}
	if err = validateResourceTopology(c, 8); err == nil {
		t.Fatal("resize discarded affinity")
	}
	if err = validateResourceTopology(c, 16); err != nil {
		t.Fatal("RAM-only rejected", err)
	}
}
func TestCPUAdoptionOmittedConstraintsAndObservedExtraFeatures(t *testing.T) {
	var c x.Domain
	_ = c.Unmarshal(`<domain><vcpu placement="static" cpuset="1-3">4</vcpu><cpu mode="host-passthrough"><topology sockets="2" cores="1" threads="2"/><numa><cell id="0" cpus="0-3" memory="4096"/></numa></cpu><features><hyperv mode="passthrough"><evmcs state="on"/></hyperv><kvm><hint-dedicated state="on"/></kvm></features></domain>`)
	before, _ := c.Marshal()
	p, _ := hardware.Normalize(hardware.Profile{ID: "legacy", CPU: &hardware.CPU{}})
	if err := validateCPUAdoption(&c, p); err != nil {
		t.Fatal(err)
	}
	after, _ := c.Marshal()
	if before != after {
		t.Fatal("adoption mutated hardware")
	}
	observed := observeCPU(&c)
	if observed.CPUSet != "1-3" || len(observed.KVM) != 1 || observed.KVM[0].Name != "hint-dedicated" || len(observed.HyperV) != 1 || observed.HyperV[0].Name != "evmcs" {
		t.Fatalf("extra observed state lost: %+v", observed)
	}
	if err := validateResourceTopology(&c, 8); err == nil {
		t.Fatal("legacy topology restructured")
	}
	if err := validateResourceTopology(&c, 4); err != nil {
		t.Fatal(err)
	}
	p.CPU.Mode = "custom"
	p.CPU.ModeExplicit = true
	p.CPU.Model = "different"
	if err := validateCPUAdoption(&c, p); err == nil {
		t.Fatal("incompatible adoption accepted")
	}
}
func TestCPUCapsRejectUnknownWithoutFallback(t *testing.T) {
	caps := &x.DomainCaps{Arch: "x86_64", Domain: "kvm", Machine: "q35", VCPU: &x.DomainCapsVCPU{Max: 8}, CPU: &x.DomainCapsCPU{Modes: []x.DomainCapsCPUMode{{Name: "custom", Supported: "yes", Models: []x.DomainCapsCPUModel{{Name: "usable", Usable: "yes", Vendor: "Intel"}, {Name: "unknown", Usable: "unknown"}}}, {Name: "maximum", Supported: "yes"}}}}
	p := hardware.Profile{ID: "test", CPU: &hardware.CPU{Mode: "custom", Model: "usable", Vendor: "Intel"}}
	if e := validateCPUCaps(p, caps, 8, 8000000); e != nil {
		t.Fatal(e)
	}
	for _, model := range []string{"missing", "unknown"} {
		p.CPU.Model = model
		if e := validateCPUCaps(p, caps, 2, 8000000); e == nil {
			t.Fatal("unverified model accepted")
		}
	}
	p.CPU = &hardware.CPU{Mode: "maximum"}
	if e := validateCPUCaps(p, caps, 9, 8000000); e == nil {
		t.Fatal("oversized topology accepted")
	}
	p.HyperV = &hardware.HyperV{Mode: "host-model"}
	if e := validateCPUCaps(p, caps, 2, 11008000); e == nil {
		t.Fatal("unsupported Hyper-V mode accepted")
	}
	if e := validateCPUCaps(p, caps, 2, 11009000); e != nil {
		t.Fatal(e)
	}
	caps.Domain = "hvf"
	caps.Arch = "aarch64"
	if e := validateCPUCaps(p, caps, 2, 12000000); e == nil {
		t.Fatal("Hyper-V on ARM/HVF accepted")
	}
	p.HyperV = nil
	p.Placement = &hardware.NativePlacement{Mode: "auto"}
	if e := validateCPUCaps(p, caps, 2, 12000000); e == nil {
		t.Fatal("numad on HVF accepted")
	}
}
func TestHostTopologyCountsOnlinePhysicalIdentities(t *testing.T) {
	var caps x.Caps
	_ = caps.Unmarshal(`<capabilities><host><cpu><topology sockets="2" dies="2" cores="2" threads="2"/></cpu><topology><cells num="1"><cell id="0"><cpus num="5"><cpu id="0" socket_id="0" die_id="0" core_id="0"/><cpu id="2" socket_id="0" die_id="0" core_id="0"/><cpu id="3" socket_id="0" die_id="1" core_id="0"/><cpu id="4" socket_id="1" die_id="0" core_id="0"/><cpu id="5" socket_id="1" die_id="1" core_id="0"/></cpus></cell></cells></topology></host></capabilities>`)
	cpus := hostCPUTopology(&caps, map[int]bool{0: true, 1: false, 2: true, 3: true, 4: true, 5: false}, true)
	node := compute.Node{Cpus: cpus}
	if cores, err := node.PhysicalCoreCount(); err != nil || cores != 3 {
		t.Fatal(cores, err)
	}
	caps.Host.NUMA.Cells.Cells[0].CPUS.CPUs[4].SocketID = nil
	node.Cpus = hostCPUTopology(&caps, map[int]bool{0: true, 1: false, 2: true, 3: true, 4: true, 5: false}, true)
	if count, err := node.PhysicalCoreCount(); err != nil || count != 3 {
		t.Fatal("offline missing topology affected online capacity", count, err)
	}
	caps.Host.NUMA.Cells.Cells[0].CPUS.CPUs[0].DieID = nil
	node.Cpus = hostCPUTopology(&caps, map[int]bool{0: true, 1: false, 2: true, 3: true, 4: true, 5: false}, true)
	if _, err := node.PhysicalCoreCount(); err == nil {
		t.Fatal("ambiguous die identity accepted")
	}
	node.Cpus = hostCPUTopology(&caps, nil, false)
	if _, err := node.PhysicalCoreCount(); err == nil {
		t.Fatal("missing online information accepted")
	}
}

func TestDarwinNativeCountsRequireCompleteMatchingTopology(t *testing.T) {
	var c x.Caps
	_ = c.Unmarshal(`<capabilities><host><cpu><topology sockets="1" dies="1" clusters="1" cores="2" threads="1"/></cpu><topology><cells num="1"><cell id="0"><cpus num="2"><cpu id="0" socket_id="0" core_id="0"/><cpu id="1" socket_id="0" core_id="1"/></cpus></cell></cells></topology></host></capabilities>`)
	if _, ok := localDarwinOnlineMap(&c, 2, 2); !ok {
		t.Fatal("matching native counts rejected")
	}
	for _, pair := range [][2]int{{1, 2}, {1, 1}, {0, 0}, {3, 3}} {
		if _, ok := localDarwinOnlineMap(&c, pair[0], pair[1]); ok {
			t.Fatal("ambiguous native counts accepted", pair)
		}
	}
}
