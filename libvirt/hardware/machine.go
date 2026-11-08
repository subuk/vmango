package hardware

import (
	"fmt"
	"slices"
	"strings"
)

// Machine retains explicit constraints separately from new-VM defaults. A nil
// Profile.Machine denotes a supported policy written before machine settings.
type Machine struct {
	Type                 string `json:"type"`
	Acceleration         string `json:"acceleration"`
	TypeExplicit         bool   `json:"type_explicit"`
	AccelerationExplicit bool   `json:"acceleration_explicit"`
}

type PlatformBinding struct {
	Machine    string `json:"machine"`
	DomainType string `json:"domain_type"`
	Emulator   string `json:"emulator"`
}

func NormalizeMachine(m Machine) (Machine, error) {
	if m.TypeExplicit && m.Type == "" {
		return m, fmt.Errorf("machine.type must not be empty; use auto for the backend default")
	}
	if m.AccelerationExplicit && m.Acceleration == "" {
		return m, fmt.Errorf("machine.acceleration must not be empty; use hardware, kvm, hvf or qemu")
	}
	if m.Type == "" {
		m.Type = "auto"
	}
	if m.Acceleration == "" {
		m.Acceleration = "hardware"
	}
	if !identifier.MatchString(m.Type) {
		return m, fmt.Errorf("machine.type %q must be a backend machine name or auto", m.Type)
	}
	if !slices.Contains([]string{"hardware", "kvm", "hvf", "qemu"}, m.Acceleration) {
		return m, fmt.Errorf("machine.acceleration %q must be hardware, kvm, hvf or qemu", m.Acceleration)
	}
	return m, nil
}

// DomainTypes never offers software execution for a hardware requirement.
func (m Machine) DomainTypes() []string {
	if m.Acceleration == "hardware" {
		return []string{"kvm", "hvf"}
	}
	return []string{m.Acceleration}
}

func (p Plan) Platform() PlatformBinding {
	return PlatformBinding{Machine: p.Machine, DomainType: p.DomainType, Emulator: p.Emulator}
}

// ValidateSavedPlatform compares exact accepted bindings without applying the
// current profile defaults or resolving the original alias again.
func ValidateSavedPlatform(saved, observed PlatformBinding) error {
	for _, f := range []struct{ name, saved, observed string }{
		{"machine", saved.Machine, observed.Machine}, {"acceleration", saved.DomainType, observed.DomainType}, {"emulator", saved.Emulator, observed.Emulator},
	} {
		if f.saved != "" && f.saved != f.observed {
			return fmt.Errorf("saved %s %q is unavailable or changed to %q; automatic substitution is forbidden", f.name, f.saved, f.observed)
		}
	}
	return nil
}

func ValidateAdoptionMachine(policy *Machine, existing PlatformBinding, requestedMachine, configuredEmulator string) error {
	if configuredEmulator != "" && configuredEmulator != existing.Emulator {
		return fmt.Errorf("host emulator %q conflicts with existing emulator %q; adoption cannot replace its path", configuredEmulator, existing.Emulator)
	}
	if policy == nil {
		return nil
	}
	if policy.AccelerationExplicit && !slices.Contains(policy.DomainTypes(), existing.DomainType) {
		return fmt.Errorf("machine.acceleration %q conflicts with existing domain type %q", policy.Acceleration, existing.DomainType)
	}
	if policy.TypeExplicit && policy.Type != "auto" && requestedMachine != existing.Machine {
		return fmt.Errorf("machine.type %q resolves to %q but the existing machine is %q; select its exact compatible version for adoption", policy.Type, requestedMachine, existing.Machine)
	}
	return nil
}

// CanonicalMachine follows only backend-advertised alias mappings. It neither
// invents machine families nor sorts version strings.
func CanonicalMachine(name string, aliases map[string]string) (string, error) {
	seen := map[string]bool{}
	for aliases[name] != "" && aliases[name] != name {
		if seen[name] {
			return "", fmt.Errorf("cyclic backend machine alias %q", name)
		}
		seen[name] = true
		name = aliases[name]
	}
	if name == "" || name == "auto" || strings.TrimSpace(name) != name {
		return "", fmt.Errorf("backend did not resolve a concrete machine")
	}
	return name, nil
}

// PlatformCapabilities is the backend policy's input, independent of XML and
// hypervisor calls. Query errors include validation of the complete profile.
type PlatformCapabilities struct {
	PlatformBinding
	Arch    string
	Aliases map[string]string
}

type PlatformQuery func(PlatformBinding) (PlatformCapabilities, error)

func ResolvePlatform(p Profile, arch, emulator string, query PlatformQuery) (PlatformBinding, error) {
	domains, machine := []string{""}, ""
	if p.Machine != nil {
		domains = p.Machine.DomainTypes()
		if p.Machine.Type != "auto" {
			machine = p.Machine.Type
		}
	}
	failures := []string{}
	for _, domain := range domains {
		caps, err := query(PlatformBinding{Machine: machine, DomainType: domain, Emulator: emulator})
		if err == nil && (caps.Arch != arch || domain != "" && caps.DomainType != domain || emulator != "" && caps.Emulator != emulator || caps.Emulator == "") {
			err = fmt.Errorf("backend returned a different architecture, acceleration or emulator")
		}
		if err == nil {
			var concrete string
			concrete, err = CanonicalMachine(caps.Machine, caps.Aliases)
			if err == nil && machine != "" {
				// The exact domain query is authoritative for custom emulator aliases.
				// Driver mappings, when advertised for the same context, must agree.
				if _, known := caps.Aliases[machine]; known {
					requested, e := CanonicalMachine(machine, caps.Aliases)
					if e != nil {
						err = e
					} else if requested != concrete {
						err = fmt.Errorf("machine.type %q resolved inconsistently (%q versus %q)", machine, requested, concrete)
					}
				}
			}
			if err == nil {
				saved := PlatformBinding{Machine: concrete, DomainType: caps.DomainType, Emulator: caps.Emulator}
				caps, err = query(saved)
				if err == nil && caps.Arch != arch {
					err = fmt.Errorf("saved guest architecture %q is unavailable", arch)
				}
				if err == nil {
					err = ValidateSavedPlatform(saved, caps.PlatformBinding)
				}
				if err == nil {
					return saved, nil
				}
			}
		}
		failures = append(failures, fmt.Sprintf("%s: %v", domain, err))
	}
	return PlatformBinding{}, fmt.Errorf("Hardware profile %s machine/acceleration/emulator combination is unsupported: %s", p.ID, strings.Join(failures, "; "))
}
