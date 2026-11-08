package libvirt

import (
	"fmt"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"

	vir "libvirt.org/go/libvirt"
	xmlmodel "libvirt.org/go/libvirtxml"
)

type domainCapabilityQuery func(emulator, arch, machine, domain string) (*xmlmodel.DomainCaps, error)

func queryDomainCapabilities(conn *vir.Connect) domainCapabilityQuery {
	return func(emulator, arch, machine, domain string) (*xmlmodel.DomainCaps, error) {
		raw, err := conn.GetDomainCapabilities(emulator, arch, machine, domain, 0)
		if err != nil {
			return nil, err
		}
		caps := &xmlmodel.DomainCaps{}
		if err = caps.Unmarshal(raw); err != nil {
			return nil, err
		}
		return caps, nil
	}
}

// Driver mappings apply only to their advertised architecture, domain type and
// emulator. A custom emulator is resolved by its exact domain-capability query.
func machineAliases(caps *xmlmodel.Caps, arch, domain, emulator string) map[string]string {
	aliases := map[string]string{}
	for _, guest := range caps.Guests {
		if guest.OSType != "hvm" || guest.Arch.Name != arch {
			continue
		}
		for _, d := range guest.Arch.Domains {
			if d.Type != domain {
				continue
			}
			path := d.Emulator
			if path == "" {
				path = guest.Arch.Emulator
			}
			if path != emulator {
				continue
			}
			machines := d.Machines
			if len(machines) == 0 {
				machines = guest.Arch.Machines
			}
			for _, m := range machines {
				aliases[m.Name] = m.Canonical
			}
		}
	}
	return aliases
}

func platformFromCaps(c *xmlmodel.DomainCaps) hardware.PlatformBinding {
	return hardware.PlatformBinding{Machine: c.Machine, DomainType: c.Domain, Emulator: c.Path}
}

// Translate backend XML into the policy resolver's capability contract. The
// resolver itself does not depend on libvirt calls or XML representation.
func resolveMachineCapabilities(p hardware.Profile, arch, emulator string, host *xmlmodel.Caps, query domainCapabilityQuery, validate func(*xmlmodel.DomainCaps) error) (*xmlmodel.DomainCaps, error) {
	var resolved *xmlmodel.DomainCaps
	_, err := hardware.ResolvePlatform(p, arch, emulator, func(request hardware.PlatformBinding) (hardware.PlatformCapabilities, error) {
		caps, err := query(request.Emulator, arch, request.Machine, request.DomainType)
		if err != nil {
			return hardware.PlatformCapabilities{}, err
		}
		if validate != nil {
			if err = validate(caps); err != nil {
				return hardware.PlatformCapabilities{}, err
			}
		}
		resolved = caps
		return hardware.PlatformCapabilities{Arch: caps.Arch, PlatformBinding: platformFromCaps(caps), Aliases: machineAliases(host, arch, caps.Domain, caps.Path)}, nil
	})
	if err != nil {
		return nil, &compute.ValidationError{Field: "hardware_profile", Message: err.Error()}
	}
	return resolved, nil
}

func validateMachineCapabilities(saved hardware.PlatformBinding, arch string, caps *xmlmodel.DomainCaps) error {
	if caps.Arch != arch {
		return fmt.Errorf("saved guest architecture %q is unavailable (backend returned %q)", arch, caps.Arch)
	}
	return hardware.ValidateSavedPlatform(saved, platformFromCaps(caps))
}

func domainPlatform(c *xmlmodel.Domain) hardware.PlatformBinding {
	p := hardware.PlatformBinding{DomainType: c.Type}
	if c.Devices != nil {
		p.Emulator = c.Devices.Emulator
	}
	if c.OS != nil && c.OS.Type != nil {
		p.Machine = c.OS.Type.Machine
	}
	return p
}

func (repo *VirtualMachineRepository) validateAdoptionMachine(vm *compute.VirtualMachine, plan hardware.Plan, observed ...*xmlmodel.Domain) error {
	conn, err := repo.pool.Acquire(vm.NodeId)
	if err != nil {
		return err
	}
	defer repo.pool.Release(vm.NodeId)
	query := queryDomainCapabilities(conn)
	existing := plan.Platform()
	if existing.Machine == "" || existing.DomainType == "" || existing.Emulator == "" {
		return fmt.Errorf("adoption requires observable existing machine, domain type and emulator bindings")
	}
	actual, err := query(existing.Emulator, vm.Arch.String(), existing.Machine, existing.DomainType)
	if err != nil {
		return fmt.Errorf("existing adoption platform is unavailable: %w", err)
	}
	// An ambiguous old alias must not be guessed to mean today's canonical target.
	if err = validateMachineCapabilities(existing, vm.Arch.String(), actual); err != nil {
		return err
	}
	requestedMachine := existing.Machine
	if p := plan.Profile.Machine; p != nil && p.TypeExplicit && p.Type != "auto" {
		raw, e := conn.GetCapabilities()
		if e != nil {
			return e
		}
		var host xmlmodel.Caps
		if e = host.Unmarshal(raw); e != nil {
			return e
		}
		requested := plan.Profile
		machine := *p
		// Omitted acceleration preserves the existing backend on adoption.
		machine.Acceleration = existing.DomainType
		requested.Machine = &machine
		caps, e := resolveMachineCapabilities(requested, vm.Arch.String(), existing.Emulator, &host, query, nil)
		if e != nil {
			return e
		}
		requestedMachine = caps.Machine
	}
	if err = hardware.ValidateAdoptionMachine(plan.Profile.Machine, existing, requestedMachine, repo.settings[vm.NodeId].Emulator); err != nil {
		return err
	}
	capabilityProfile := plan.Profile
	if capabilityProfile.Presentation != nil {
		capabilityProfile.Graphics = "none"
		capabilityProfile.VideoModel = "none"
	}
	if err := validateProfileCapabilities(capabilityProfile, imageCapabilities(actual)); err != nil {
		return err
	}
	if err := repo.validateCPUCapabilities(conn, plan.Profile, actual, vm.VCpus, vm.NodeId, observed...); err != nil {
		return err
	}
	return validateGuestSystemCapabilities(conn, plan.Profile, actual)
}

func validateCreationCapabilities(vm *compute.VirtualMachine, profile hardware.Profile, caps *xmlmodel.DomainCaps) error {
	if caps.VCPU != nil && uint(vm.VCpus) > caps.VCPU.Max {
		return fmt.Errorf("CPU count exceeds the selected machine's limit")
	}
	if err := validateProfileCapabilities(profile, imageCapabilities(caps)); err != nil {
		return err
	}
	// The foundation emits maximum CPU mode. Do not assume the same CPU mode
	// works after switching acceleration or machine platform.
	if caps.CPU != nil {
		for _, mode := range caps.CPU.Modes {
			if mode.Name == "maximum" && mode.Supported == "yes" {
				return nil
			}
		}
		return fmt.Errorf("the selected combination does not support the maximum CPU mode")
	}
	return fmt.Errorf("the selected combination does not advertise CPU mode support")
}
