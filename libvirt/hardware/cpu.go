package hardware

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// CPU is declarative guest presentation, independent of host placement.
// ModeExplicit distinguishes a new-VM default from an adoption constraint.
type CPU struct {
	Mode         string       `json:"mode" hcl:"mode"`
	ModeExplicit bool         `json:"mode_explicit" hcl:"-"`
	Model        string       `json:"model,omitempty" hcl:"model"`
	Vendor       string       `json:"vendor,omitempty" hcl:"vendor"`
	Features     []CPUFeature `json:"features,omitempty" hcl:"feature"`
}
type CPUFeature struct {
	Name   string `json:"name" hcl:",key"`
	Policy string `json:"policy" hcl:"policy"`
}
type NativePlacement struct {
	Mode         string `json:"mode" hcl:"mode"`
	ModeExplicit bool   `json:"mode_explicit" hcl:"-"`
	MemoryMode   string `json:"memory_mode,omitempty" hcl:"memory_mode"`
}
type HyperV struct {
	Mode        string     `json:"mode" hcl:"mode"`
	Relaxed     *bool      `json:"relaxed,omitempty" hcl:"relaxed"`
	VAPIC       *bool      `json:"vapic,omitempty" hcl:"vapic"`
	VPIndex     *bool      `json:"vpindex,omitempty" hcl:"vpindex"`
	Runtime     *bool      `json:"runtime,omitempty" hcl:"runtime"`
	Synic       *bool      `json:"synic,omitempty" hcl:"synic"`
	Reset       *bool      `json:"reset,omitempty" hcl:"reset"`
	Frequencies *bool      `json:"frequencies,omitempty" hcl:"frequencies"`
	TLBFlush    *bool      `json:"tlbflush,omitempty" hcl:"tlbflush"`
	IPI         *bool      `json:"ipi,omitempty" hcl:"ipi"`
	Spinlocks   *Spinlocks `json:"spinlocks,omitempty" hcl:"spinlocks"`
	STimer      *STimer    `json:"stimer,omitempty" hcl:"stimer"`
}
type Spinlocks struct {
	Enabled *bool  `json:"enabled,omitempty" hcl:"enabled"`
	Retries *int64 `json:"retries,omitempty" hcl:"retries"`
}
type STimer struct {
	Enabled *bool `json:"enabled,omitempty" hcl:"enabled"`
	Direct  *bool `json:"direct,omitempty" hcl:"direct"`
}
type KVM struct {
	Hidden      *bool `json:"hidden,omitempty" hcl:"hidden"`
	PollControl *bool `json:"poll_control,omitempty" hcl:"poll_control"`
	PVIPI       *bool `json:"pv_ipi,omitempty" hcl:"pv_ipi"`
}

func Enabled(v *bool) bool { return v != nil && *v }
func (h HyperV) Features() map[string]*bool {
	return map[string]*bool{"relaxed": h.Relaxed, "vapic": h.VAPIC, "vpindex": h.VPIndex, "runtime": h.Runtime, "synic": h.Synic, "reset": h.Reset, "frequencies": h.Frequencies, "tlbflush": h.TLBFlush, "ipi": h.IPI}
}

var cpuName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

func NormalizeCPU(p Profile) (Profile, error) {
	fail := func(field, reason string) (Profile, error) {
		return p, fmt.Errorf("hardware profile %q: %s %s", p.ID, field, reason)
	}
	// Deep-copy optional policy before supplying defaults. Nil remains nil in old plans.
	copyPolicy := func(in, out any) { raw, _ := json.Marshal(in); _ = json.Unmarshal(raw, out) }
	if p.CPU != nil {
		c := &CPU{}
		copyPolicy(p.CPU, c)
		p.CPU = c
		if c.Mode == "" {
			c.Mode = "maximum"
		}
		switch c.Mode {
		case "maximum", "host-passthrough", "host-model":
			if c.Model != "" || c.Vendor != "" {
				return fail("cpu.model/vendor", "requires custom mode")
			}
		case "custom":
			if !cpuName.MatchString(c.Model) {
				return fail("cpu.model", "is required for custom mode")
			}
		default:
			return fail("cpu.mode", "must be maximum, host-passthrough, host-model or custom")
		}
		if c.Vendor != "" && !cpuName.MatchString(c.Vendor) {
			return fail("cpu.vendor", "must be a backend CPU vendor name")
		}
		seen := map[string]bool{}
		for i, f := range c.Features {
			if !cpuName.MatchString(f.Name) || seen[f.Name] {
				return fail("cpu.feature."+f.Name, "is invalid or duplicated")
			}
			seen[f.Name] = true
			if f.Policy == "" {
				f.Policy = "require"
			}
			if f.Policy != "require" && f.Policy != "disable" {
				return fail("cpu.feature."+f.Name+".policy", "must be require or disable")
			}
			c.Features[i] = f
		}
	}
	if p.Placement != nil {
		v := *p.Placement
		p.Placement = &v
		if v.Mode == "" {
			p.Placement.Mode = "static"
		}
		switch p.Placement.Mode {
		case "static":
			if v.MemoryMode != "" {
				return fail("placement.memory_mode", "requires auto placement")
			}
		case "auto":
			if v.MemoryMode == "" {
				p.Placement.MemoryMode = "strict"
			}
			if p.Placement.MemoryMode != "strict" {
				return fail("placement.memory_mode", "must be strict")
			}
		default:
			return fail("placement.mode", "must be static or auto")
		}
	}
	if p.KVM != nil {
		k := &KVM{}
		copyPolicy(p.KVM, k)
		p.KVM = k
	}
	if p.HyperV != nil {
		h := &HyperV{}
		copyPolicy(p.HyperV, h)
		p.HyperV = h
		if h.Mode == "" {
			h.Mode = "custom"
		}
		if h.Mode != "custom" && h.Mode != "host-model" && h.Mode != "passthrough" {
			return fail("hyperv.mode", "must be custom, host-model or passthrough")
		}
		if h.Mode != "custom" {
			for _, v := range h.Features() {
				if v != nil {
					return fail("hyperv", "automatic modes do not accept custom overrides")
				}
			}
			if h.Spinlocks != nil || h.STimer != nil {
				return fail("hyperv", "automatic modes do not accept custom overrides")
			}
		}
		yes := true
		if s := h.Spinlocks; s != nil {
			if s.Enabled == nil {
				s.Enabled = &yes
			}
			if !*s.Enabled && s.Retries != nil {
				return fail("hyperv.spinlocks.retries", "requires enabled spinlocks")
			}
			if *s.Enabled {
				if s.Retries == nil {
					v := int64(4096)
					s.Retries = &v
				}
				if *s.Retries < 4095 || *s.Retries > 4294967295 {
					return fail("hyperv.spinlocks.retries", "must be 4095..4294967295")
				}
			}
		}
		if s := h.STimer; s != nil {
			if s.Enabled == nil {
				s.Enabled = &yes
			}
			if !*s.Enabled && s.Direct != nil {
				return fail("hyperv.stimer.direct", "requires enabled stimer")
			}
			if *s.Enabled && (!Enabled(h.VPIndex) || !Enabled(h.Synic)) {
				return fail("hyperv.stimer", "requires explicitly enabled vpindex and synic")
			}
		}
		if (Enabled(h.Synic) || Enabled(h.TLBFlush) || Enabled(h.IPI)) && !Enabled(h.VPIndex) {
			return fail("hyperv", "synic, tlbflush and ipi require vpindex")
		}
		clock := false
		disabled := false
		for _, t := range p.Timers {
			if t.Name == "hypervclock" {
				clock = Enabled(t.Present)
				disabled = t.Present != nil && !*t.Present
			}
		}
		if h.STimer != nil && Enabled(h.STimer.Enabled) && !clock {
			return fail("hyperv.stimer", "requires explicit timer hypervclock { present = true }")
		}
		if h.Mode != "custom" && disabled {
			return fail("hyperv.mode", "automatic enlightenments conflict with explicitly disabled hypervclock")
		}
	}
	return p, nil
}

// CPUObservation describes libvirt XML, not a guarantee about executed guest instructions.
type CPUObservation struct {
	Mode            string                `json:"mode,omitempty"`
	Model           string                `json:"model,omitempty"`
	Vendor          string                `json:"vendor,omitempty"`
	Features        []CPUFeature          `json:"features,omitempty"`
	Topology        *CPUTopology          `json:"topology,omitempty"`
	GuestNUMA       bool                  `json:"guest_numa"`
	Placement       string                `json:"placement,omitempty"`
	CPUSet          string                `json:"cpuset,omitempty"`
	MemoryPlacement string                `json:"memory_placement,omitempty"`
	MemoryMode      string                `json:"memory_mode,omitempty"`
	MemoryNodeset   string                `json:"memory_nodeset,omitempty"`
	HyperVMode      string                `json:"hyperv_mode,omitempty"`
	HyperV          []PresentationFeature `json:"hyperv,omitempty"`
	KVM             []PresentationFeature `json:"kvm,omitempty"`
}
type CPUTopology struct {
	Sockets  int `json:"sockets"`
	Dies     int `json:"dies"`
	Clusters int `json:"clusters"`
	Cores    int `json:"cores"`
	Threads  int `json:"threads"`
}
type PresentationFeature struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Value string `json:"value,omitempty"`
}
