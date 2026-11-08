package libvirt

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"

	vir "libvirt.org/go/libvirt"
	x "libvirt.org/go/libvirtxml"
)

func featureState(v bool) *x.DomainFeatureState {
	return &x.DomainFeatureState{State: map[bool]string{true: "on", false: "off"}[v]}
}
func cpuXML(p *hardware.CPU) *x.DomainCPU {
	c := &x.DomainCPU{Mode: "maximum"}
	if p == nil {
		return c
	}
	c.Mode = p.Mode
	if p.Mode == "custom" {
		c.Match = "exact"
		c.Check = "full"
		c.Model = &x.DomainCPUModel{Value: p.Model, Fallback: "forbid"}
		c.Vendor = p.Vendor
	}
	for _, f := range p.Features {
		c.Features = append(c.Features, x.DomainCPUFeature{Name: f.Name, Policy: f.Policy})
	}
	return c
}
func hypervXML(h *hardware.HyperV) *x.DomainFeatureHyperV {
	if h == nil {
		return nil
	}
	v := &x.DomainFeatureHyperV{Mode: h.Mode}
	if h.Mode != "custom" {
		return v
	}
	v.Relaxed = featureState(hardware.Enabled(h.Relaxed))
	v.VAPIC = featureState(hardware.Enabled(h.VAPIC))
	v.VPIndex = featureState(hardware.Enabled(h.VPIndex))
	v.Runtime = featureState(hardware.Enabled(h.Runtime))
	v.Synic = featureState(hardware.Enabled(h.Synic))
	v.Reset = featureState(hardware.Enabled(h.Reset))
	v.Frequencies = featureState(hardware.Enabled(h.Frequencies))
	v.IPI = featureState(hardware.Enabled(h.IPI))
	v.TLBFlush = &x.DomainFeatureHyperVTLBFlush{DomainFeatureState: *featureState(hardware.Enabled(h.TLBFlush))}
	v.Spinlocks = &x.DomainFeatureHyperVSpinlocks{DomainFeatureState: *featureState(h.Spinlocks != nil && hardware.Enabled(h.Spinlocks.Enabled))}
	if h.Spinlocks != nil && h.Spinlocks.Retries != nil {
		v.Spinlocks.Retries = uint(*h.Spinlocks.Retries)
	}
	v.STimer = &x.DomainFeatureHyperVSTimer{DomainFeatureState: *featureState(h.STimer != nil && hardware.Enabled(h.STimer.Enabled))}
	if h.STimer != nil && hardware.Enabled(h.STimer.Enabled) {
		v.STimer.Direct = featureState(hardware.Enabled(h.STimer.Direct))
	}
	return v
}
func applyCPUPolicy(c *x.Domain, p hardware.Profile) {
	c.CPU = cpuXML(p.CPU)
	if p.HyperV != nil || p.KVM != nil {
		if c.Features == nil {
			c.Features = &x.DomainFeatureList{}
		}
	}
	if p.HyperV != nil {
		c.Features.HyperV = hypervXML(p.HyperV)
	}
	if k := p.KVM; k != nil && (k.Hidden != nil || k.PollControl != nil || k.PVIPI != nil) {
		v := &x.DomainFeatureKVM{}
		c.Features.KVM = v
		if k.Hidden != nil {
			v.Hidden = featureState(*k.Hidden)
		}
		if k.PollControl != nil {
			v.PollControl = featureState(*k.PollControl)
		}
		if k.PVIPI != nil {
			v.PVIPI = featureState(*k.PVIPI)
		}
	}
	if p.Placement != nil && p.Placement.Mode == "auto" {
		if c.VCPU == nil {
			c.VCPU = &x.DomainVCPU{}
		}
		c.VCPU.Placement = "auto"
		c.NUMATune = &x.DomainNUMATune{Memory: &x.DomainNUMATuneMemory{Mode: "strict", Placement: "auto"}}
	}
}
func observeCPU(c *x.Domain) hardware.CPUObservation {
	o := hardware.CPUObservation{}
	if v := c.CPU; v != nil {
		o.Mode = v.Mode
		o.Vendor = v.Vendor
		if v.Model != nil {
			o.Model = v.Model.Value
		}
		for _, f := range v.Features {
			o.Features = append(o.Features, hardware.CPUFeature{Name: f.Name, Policy: f.Policy})
		}
		if t := v.Topology; t != nil {
			o.Topology = &hardware.CPUTopology{Sockets: t.Sockets, Dies: t.Dies, Clusters: t.Clusters, Cores: t.Cores, Threads: t.Threads}
		}
		o.GuestNUMA = v.Numa != nil
	}
	if c.VCPU != nil {
		o.Placement = c.VCPU.Placement
		o.CPUSet = c.VCPU.CPUSet
	}
	if c.NUMATune != nil && c.NUMATune.Memory != nil {
		m := c.NUMATune.Memory
		o.MemoryPlacement = m.Placement
		o.MemoryMode = m.Mode
		o.MemoryNodeset = m.Nodeset
	}
	if c.Features != nil {
		if c.Features.HyperV != nil {
			o.HyperVMode = c.Features.HyperV.Mode
			o.HyperV = observePresentation(c.Features.HyperV)
		}
		if c.Features.KVM != nil {
			o.KVM = observePresentation(c.Features.KVM)
		}
	}
	return o
}

// Parse the typed XML structure, retaining extra observed native features without
// promoting them into configurable arbitrary-QEMU properties.
func observePresentation(v any) []hardware.PresentationFeature {
	raw, _ := xml.Marshal(v)
	var root struct {
		Children []struct {
			XMLName  xml.Name
			State    string `xml:"state,attr"`
			Value    string `xml:"value,attr"`
			Retries  string `xml:"retries,attr"`
			Children []struct {
				XMLName xml.Name
				State   string `xml:"state,attr"`
			} `xml:",any"`
		} `xml:",any"`
	}
	_ = xml.Unmarshal(raw, &root)
	out := []hardware.PresentationFeature{}
	for _, c := range root.Children {
		value := c.Value
		if c.Retries != "" {
			value = c.Retries
		}
		out = append(out, hardware.PresentationFeature{Name: c.XMLName.Local, State: c.State, Value: value})
		for _, child := range c.Children {
			out = append(out, hardware.PresentationFeature{Name: c.XMLName.Local + "." + child.XMLName.Local, State: child.State})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func validateCPUAdoption(c *x.Domain, p hardware.Profile) error {
	fail := func(field string) error {
		return &compute.ValidationError{Field: "hardware_profile", Message: "Existing " + field + " does not match the requested CPU/presentation/placement policy; adoption never rewrites it."}
	}
	if cpu := p.CPU; cpu != nil {
		if cpu.ModeExplicit && (c.CPU == nil || c.CPU.Mode != cpu.Mode) {
			return fail("CPU mode")
		}
		if cpu.Model != "" && (c.CPU == nil || c.CPU.Model == nil || c.CPU.Model.Value != cpu.Model || c.CPU.Model.Fallback != "forbid") {
			return fail("exact CPU model/fallback")
		}
		if cpu.Vendor != "" && (c.CPU == nil || c.CPU.Vendor != cpu.Vendor) {
			return fail("CPU vendor")
		}
		for _, want := range cpu.Features {
			found := false
			if c.CPU != nil {
				for _, f := range c.CPU.Features {
					if f.Name == want.Name && f.Policy == want.Policy {
						found = true
					}
				}
			}
			if !found {
				return fail("CPU feature " + want.Name)
			}
		}
	}
	if pl := p.Placement; pl != nil && (pl.ModeExplicit || pl.Mode == "auto") {
		if pl.Mode == "auto" {
			if c.VCPU == nil || c.VCPU.Placement != "auto" || c.VCPU.CPUSet != "" || c.CPUTune != nil && (len(c.CPUTune.VCPUPin) > 0 || c.CPUTune.EmulatorPin != nil) || c.NUMATune == nil || c.NUMATune.Memory == nil || c.NUMATune.Memory.Placement != "auto" || c.NUMATune.Memory.Mode != "strict" || c.NUMATune.Memory.Nodeset != "" || len(c.NUMATune.MemNodes) > 0 {
				return fail("native auto placement/affinity")
			}
		} else if c.VCPU != nil && (c.VCPU.Placement == "auto" || c.VCPU.CPUSet != "") || c.CPUTune != nil && (len(c.CPUTune.VCPUPin) > 0 || c.CPUTune.EmulatorPin != nil) || c.NUMATune != nil {
			return fail("unrestricted static placement")
		}
	}
	if p.HyperV != nil {
		if c.Features == nil || c.Features.HyperV == nil {
			return fail("Hyper-V mode")
		}
		got := c.Features.HyperV
		mode := got.Mode
		if mode == "" {
			mode = "custom"
		}
		if mode != p.HyperV.Mode {
			return fail("Hyper-V mode")
		}
		if mode == "custom" {
			want := observePresentation(hypervXML(p.HyperV))
			actual := map[string]hardware.PresentationFeature{}
			for _, f := range observePresentation(got) {
				actual[f.Name] = f
			}
			for _, f := range want {
				a, ok := actual[f.Name]
				if f.State == "off" && (!ok || a.State == "off") {
					continue
				}
				if !ok || a.State != f.State || a.Value != f.Value {
					return fail("Hyper-V " + f.Name)
				}
				delete(actual, f.Name)
			}
			for _, f := range actual {
				if f.State == "on" {
					return fail("unrequested Hyper-V " + f.Name)
				}
			}
		}
	}
	if k := p.KVM; k != nil {
		actual := map[string]string{}
		if c.Features != nil && c.Features.KVM != nil {
			for _, f := range observePresentation(c.Features.KVM) {
				actual[f.Name] = f.State
			}
		}
		for name, v := range map[string]*bool{"hidden": k.Hidden, "poll-control": k.PollControl, "pv-ipi": k.PVIPI} {
			if v != nil {
				got := actual[name]
				if name == "hidden" && got == "" {
					got = "off"
				}
				if got != featureState(*v).State {
					return fail("KVM " + name)
				}
			}
		}
	}
	return nil
}

func validateCPUCaps(p hardware.Profile, caps *x.DomainCaps, vcpus int, version uint32) error {
	fail := func(reason string) error {
		return &compute.ValidationError{Field: "hardware_profile", Message: fmt.Sprintf("Hardware profile %s on %s/%s/%s: %s", p.ID, caps.Arch, caps.Machine, caps.Domain, reason)}
	}
	if caps.VCPU == nil || caps.VCPU.Max == 0 {
		return fail("vCPU limit is unknown")
	}
	if vcpus < 1 || uint(vcpus) > caps.VCPU.Max {
		return fail("vCPU count exceeds backend limit")
	}
	if p.CPU != nil {
		if caps.CPU == nil {
			return fail("CPU capability information is unavailable")
		}
		var mode *x.DomainCapsCPUMode
		for i := range caps.CPU.Modes {
			if caps.CPU.Modes[i].Name == p.CPU.Mode {
				mode = &caps.CPU.Modes[i]
			}
		}
		if mode == nil || mode.Supported != "yes" {
			return fail("CPU mode " + p.CPU.Mode + " is unsupported or unverified")
		}
		if p.CPU.Mode == "custom" {
			found := false
			for _, m := range mode.Models {
				if m.Name == p.CPU.Model {
					found = m.Usable == "yes"
					if p.CPU.Vendor != "" && m.Vendor != "" && m.Vendor != p.CPU.Vendor {
						return fail("CPU model/vendor mismatch")
					}
				}
			}
			if !found {
				return fail("exact CPU model " + p.CPU.Model + " is not reported usable")
			}
		}
	}
	x86 := caps.Arch == "x86_64" || caps.Arch == "i686"
	if p.HyperV != nil {
		if !x86 || caps.Domain != "kvm" {
			return fail("Hyper-V presentation requires verified x86/KVM support")
		}
		if p.HyperV.Mode == "host-model" && version < 11009000 {
			return fail("Hyper-V host-model requires libvirt >= 11.9.0")
		}
	}
	if p.KVM != nil && (p.KVM.Hidden != nil || p.KVM.PollControl != nil || p.KVM.PVIPI != nil) && (!x86 || caps.Domain != "kvm") {
		return fail("explicit KVM presentation requires verified x86/KVM support")
	}
	if p.Placement != nil && p.Placement.Mode == "auto" && caps.Domain != "kvm" {
		return fail("native auto placement requires a Linux KVM host with numad")
	}
	return nil
}

func (repo *VirtualMachineRepository) validateCPUCapabilities(conn *vir.Connect, p hardware.Profile, caps *x.DomainCaps, vcpus int, node string, existing ...*x.Domain) error {
	// Optional fields in older plans are not new constraints or a topology migration.
	if len(existing) == 0 && p.CPU == nil && p.HyperV == nil && p.KVM == nil && p.Placement == nil {
		return nil
	}
	if len(existing) > 0 && p.CPU != nil && !p.CPU.ModeExplicit {
		if len(p.CPU.Features) == 0 {
			p.CPU = nil
		} else if existing[0].CPU != nil {
			copy := *p.CPU
			copy.Mode = existing[0].CPU.Mode
			if copy.Mode == "custom" && existing[0].CPU.Model != nil {
				copy.Model = existing[0].CPU.Model.Value
			}
			p.CPU = &copy
		}
	}
	version, err := conn.GetLibVersion()
	if err != nil {
		return err
	}
	if err = validateCPUCaps(p, caps, vcpus, version); err != nil {
		return err
	}
	if p.Placement != nil && p.Placement.Mode == "auto" {
		if err = checkNativePlacement(repo.pool.nodeUri[node]); err != nil {
			return err
		}
	}
	c := &x.Domain{Type: caps.Domain, Name: "vmango-cpu-validation", UUID: "00000000-0000-4000-8000-000000000001", Memory: &x.DomainMemory{Value: 512, Unit: "MiB"}, VCPU: &x.DomainVCPU{Value: uint(vcpus)}, OS: &x.DomainOS{Type: &x.DomainOSType{Type: "hvm", Arch: caps.Arch, Machine: caps.Machine}}, Devices: &x.DomainDeviceList{Emulator: caps.Path}}
	if p.Firmware != "" && p.Firmware != "auto" {
		c.OS.Firmware = p.Firmware
	}
	applyGuestSystem(c, p)
	applyCPUPolicy(c, p)
	c.CPU.Topology = &x.DomainCPUTopology{Sockets: 1, Cores: vcpus, Threads: 1}
	if len(existing) > 0 {
		raw, _ := json.Marshal(existing[0])
		var preserved x.Domain
		_ = json.Unmarshal(raw, &preserved)
		if preserved.Memory != nil {
			c.Memory = preserved.Memory
			c.CurrentMemory = preserved.CurrentMemory
		}
		c.CPU = preserved.CPU
		c.Features = preserved.Features
		c.CPUTune = preserved.CPUTune
		c.NUMATune = preserved.NUMATune
		c.VCPUs = preserved.VCPUs
		if preserved.VCPU != nil {
			c.VCPU = preserved.VCPU
			c.VCPU.Value = uint(vcpus)
		}
		if c.CPU != nil && c.CPU.Topology != nil && topologyFactor(c.CPU.Topology) == 1 {
			c.CPU.Topology.Cores = vcpus
		}
	}
	if err = validateCPUFeatures(conn, p, caps); err != nil {
		return err
	}
	raw, err := c.Marshal()
	if err != nil {
		return err
	}
	_, err = conn.DomainXMLToNative("qemu-argv", raw, 0)
	if err != nil {
		return fmt.Errorf("hardware profile %s: CPU/topology/presentation/placement native capability validation failed: %w", p.ID, err)
	}
	return nil
}
func validateCPUFeatures(conn *vir.Connect, p hardware.Profile, caps *x.DomainCaps) error {
	if p.CPU == nil || p.CPU.Vendor == "" && len(p.CPU.Features) == 0 {
		return nil
	}
	c := cpuXML(p.CPU)
	// CompareHypervisorCPU validates feature names and requirements against the
	// exact emulator. Host-derived modes use its advertised host-model baseline.
	if p.CPU.Mode != "custom" {
		c.Model = nil
		c.Mode = "custom"
		c.Match = "exact"
		c.Check = "full"
		for _, m := range caps.CPU.Modes {
			if m.Name == "host-model" && m.Supported == "yes" && len(m.Models) == 1 {
				c.Model = &x.DomainCPUModel{Value: m.Models[0].Name, Fallback: "forbid"}
				c.Vendor = m.Vendor
				for _, f := range m.Features {
					overridden := false
					for _, want := range c.Features {
						overridden = overridden || want.Name == f.Name
					}
					if !overridden {
						c.Features = append(c.Features, x.DomainCPUFeature{Name: f.Name, Policy: f.Policy})
					}
				}
			}
		}
		if c.Model == nil {
			return fmt.Errorf("CPU feature support is unverified for %s/%s: backend has no comparable host model", caps.Arch, caps.Domain)
		}
	}
	raw, err := c.Marshal()
	if err != nil {
		return err
	}
	result, err := conn.CompareHypervisorCPU(caps.Path, caps.Arch, caps.Machine, caps.Domain, raw, vir.CONNECT_COMPARE_CPU_FAIL_INCOMPATIBLE)
	if err != nil {
		return fmt.Errorf("CPU model/vendor/features are unsupported or unverified: %w", err)
	}
	if result != vir.CPU_COMPARE_IDENTICAL && result != vir.CPU_COMPARE_SUPERSET {
		return fmt.Errorf("CPU requirements are incompatible with backend (%s)", strconv.Itoa(int(result)))
	}
	return nil
}
