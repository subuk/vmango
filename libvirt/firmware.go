package libvirt

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	vir "libvirt.org/go/libvirt"
	x "libvirt.org/go/libvirtxml"
	"reflect"
	"strings"
	"subuk/vmango/compute"
	"subuk/vmango/libvirt/hardware"
	"subuk/vmango/tasks"
)

func firmwareRequirements(os *x.DomainOS, f hardware.Firmware) {
	if f.Type != "auto" {
		os.Firmware = f.Type
	}
	if f.Type == "efi" {
		os.Loader = &x.DomainLoader{Type: "pflash", Readonly: "yes"}
		if f.SecureBoot != nil {
			value := "no"
			if *f.SecureBoot {
				value = "yes"
			}
			os.Loader.Secure = value
			os.FirmwareInfo = &x.DomainOSFirmwareInfo{Features: []x.DomainOSFirmwareFeature{{Name: "secure-boot", Enabled: value}}}
			if *f.SecureBoot && f.EnrolledKeys != nil {
				keys := "no"
				if *f.EnrolledKeys {
					keys = "yes"
				}
				os.FirmwareInfo.Features = append(os.FirmwareInfo.Features, x.DomainOSFirmwareFeature{Name: "enrolled-keys", Enabled: keys})
			}
		}
	}
}
func applyFirmware(c *x.Domain, b *hardware.FirmwareBinding) {
	if b == nil {
		return
	}
	c.OS.Firmware = "" // Concrete loader/template bindings prevent reselection.
	c.OS.FirmwareInfo = nil
	c.OS.Loader = nil
	c.OS.NVRam = nil
	c.OS.VarStore = nil
	if b.Loader == "" {
		return
	}
	c.OS.Loader = &x.DomainLoader{Path: b.Loader, Type: b.LoaderType, Readonly: b.Readonly, Secure: b.Secure}
	if b.LoaderFormat == "qcow2" {
		c.OS.Loader.Format = "qcow2"
	}
	if b.Type == "efi" {

		c.OS.NVRam = &x.DomainNVRam{NVRam: b.NVRAM, Template: b.NVRAMTemplate}
		if b.NVRAMFormat == "qcow2" {
			c.OS.NVRam.Format = "qcow2"
			c.OS.NVRam.TemplateFormat = "qcow2"
		}
		if b.Secure == "yes" {
			if c.Features == nil {
				c.Features = &x.DomainFeatureList{}
			}
			c.Features.SMM = &x.DomainFeatureSMM{State: "on"}
		}
	}
}
func observeFirmware(c *x.Domain) hardware.FirmwareBinding {
	b := hardware.FirmwareBinding{Type: "bios"}
	if c.OS == nil {
		return b
	}
	if c.OS.VarStore != nil {
		b.Unsupported = "varstore"
	}
	if c.OS.Firmware == "efi" {
		b.Type = "efi"
	}
	if l := c.OS.Loader; l != nil {
		if l.Stateless == "yes" {
			b.Unsupported = "stateless"
		}
		b.Loader, b.LoaderType, b.Readonly, b.Secure = l.Path, l.Type, l.Readonly, l.Secure
		b.LoaderFormat = l.Format
		if b.LoaderFormat == "" {
			b.LoaderFormat = "raw"
		}
		if l.Type == "pflash" {
			b.Type = "efi"
		}
	}
	if n := c.OS.NVRam; n != nil {
		if n.Source != nil && n.Source.File == nil {
			b.Unsupported = "non-file NVRAM"
		}
		b.NVRAM = n.NVRam
		b.NVRAMTemplate = n.Template
		b.NVRAMFormat = n.Format
		if b.NVRAMFormat == "" {
			b.NVRAMFormat = "raw"
		}
		if n.TemplateFormat != "" && n.TemplateFormat != b.NVRAMFormat {
			b.Unsupported = "template/private format mismatch"
		}
		if n.Source != nil && n.Source.File != nil {
			b.NVRAM = n.Source.File.File
		}
	}
	if c.OS.FirmwareInfo != nil {
		for _, f := range c.OS.FirmwareInfo.Features {
			v := f.Enabled == "yes"
			switch f.Name {
			case "secure-boot":
				b.SecureBoot = &v
			case "enrolled-keys":
				b.EnrolledKeys = &v
			}
		}
	}
	return b
}
func supportedFirmwareXML(c *x.Domain) error {
	if c.OS == nil {
		return nil
	}
	if c.OS.VarStore != nil || c.OS.Loader != nil && c.OS.Loader.Stateless == "yes" {
		return fmt.Errorf("varstore and stateless UEFI are unsupported")
	}
	if n := c.OS.NVRam; n != nil && n.Source != nil && n.Source.File == nil {
		return fmt.Errorf("only file-backed NVRAM is supported")
	}
	b := observeFirmware(c)
	if b.Unsupported != "" {
		return fmt.Errorf("unsupported firmware state: %s", b.Unsupported)
	}
	if b.Type == "efi" && (b.LoaderType != "pflash" || b.Readonly != "yes" || b.NVRAMTemplate == "" || b.NVRAM == "") {
		return fmt.Errorf("UEFI requires read-only pflash CODE, saved VARS template and private file NVRAM")
	}
	return nil
}

func firmwareProbe(caps *x.DomainCaps) *x.Domain {
	return &x.Domain{Type: caps.Domain, Name: "vmango-firmware-validation", UUID: "00000000-0000-4000-8000-000000000002", Memory: &x.DomainMemory{Unit: "MiB", Value: 512}, VCPU: &x.DomainVCPU{Value: 1}, OS: &x.DomainOS{Type: &x.DomainOSType{Type: "hvm", Arch: caps.Arch, Machine: caps.Machine}}, CPU: &x.DomainCPU{Mode: "maximum"}, Devices: &x.DomainDeviceList{Emulator: caps.Path}}
}

func (repo *VirtualMachineRepository) resolveFirmware(conn *vir.Connect, p hardware.Profile, bindings []hardware.FirmwareHostBinding, caps *x.DomainCaps, node string) (result *hardware.FirmwareBinding, err error) {
	defer func() {
		if err != nil {
			err = &compute.ValidationError{Field: "hardware_profile", Message: "Firmware requirements: " + err.Error()}
		}
	}()
	// Supported historical policy has no new defaults imposed upon it.
	if p.FirmwarePolicy == nil {
		return nil, nil
	}
	f := *p.FirmwarePolicy
	if f.SecureBoot != nil && *f.SecureBoot && caps.Arch != "x86_64" {
		return nil, fmt.Errorf("Secure Boot with supported pflash requires x86_64; ARM varstore is outside this delivery")
	}
	version, err := conn.GetLibVersion()
	if err != nil {
		return nil, err
	}
	c := firmwareProbe(caps)
	applyDomainPowerPolicy(c)
	firmwareRequirements(c.OS, f)
	var b hardware.FirmwareBinding
	uri := repo.pool.nodeUri[node]
	if f.Profile != "" {
		var host *hardware.FirmwareHostBinding
		for i := range bindings {
			if bindings[i].ID == f.Profile {
				host = &bindings[i]
				break
			}
		}
		if host == nil {
			return nil, fmt.Errorf("host %s has no binding for firmware_profile %s; automatic fallback is forbidden", node, f.Profile)
		}
		if err := hardware.ValidateFirmwareHost(f, *host); err != nil {
			return nil, err
		}
		b = hardware.FirmwareBinding{Type: f.Type, Loader: host.Loader, LoaderType: "rom", Readonly: "yes", LoaderFormat: f.LoaderFormat, NVRAMTemplate: host.NVRAMTemplate, NVRAMFormat: f.NVRAMFormat, SecureBoot: f.SecureBoot, EnrolledKeys: f.EnrolledKeys, Evidence: "administrator-artifact-contract"}
		if f.Type == "efi" {
			b.LoaderType = "pflash"
			b.Secure = "no"
			if *f.SecureBoot {
				b.Secure = "yes"
			}
		}
		descriptors, e := hostFirmwareDescriptors(uri)
		if e != nil {
			return nil, e
		}
		for _, d := range descriptors {
			if d.Mapping.Executable.Filename == b.Loader && (f.Type != "efi" || d.Mapping.NVRAMTemplate.Filename == b.NVRAMTemplate) {
				if err := hardware.ValidateFirmwareDescriptor(d, f, caps.Arch, caps.Machine); err != nil {
					return nil, err
				}
				b.Evidence = "host-firmware-descriptor"
				break
			}
		}
		applyFirmware(c, &b)
	} else {
		raw, e := c.Marshal()
		if e != nil {
			return nil, e
		}
		argv, e := conn.DomainXMLToNative("qemu-argv", raw, 0)
		if e != nil {
			return nil, fmt.Errorf("firmware selection cannot satisfy requirements: %w", e)
		}
		if f.Type == "auto" && !strings.Contains(argv, "pflash") && !strings.Contains(argv, "uefi-vars") {
			return &hardware.FirmwareBinding{Type: "bios", Evidence: "platform-default"}, nil
		}
		descriptors, e := hostFirmwareDescriptors(uri)
		if e != nil {
			return nil, e
		}
		found := false
		for _, d := range descriptors {
			if hardware.ValidateFirmwareDescriptor(d, f, caps.Arch, caps.Machine) != nil {
				continue
			}
			encoded, _ := json.Marshal(d.Mapping.Executable.Filename)
			if !strings.Contains(argv, d.Mapping.Executable.Filename) && !strings.Contains(argv, string(encoded[1:len(encoded)-1])) {
				continue
			}
			candidate := d.Binding()
			if found && (candidate.Loader != b.Loader || candidate.NVRAMTemplate != b.NVRAMTemplate) {
				return nil, fmt.Errorf("automatic firmware selection is ambiguous in host metadata; define an explicit named firmware profile")
			}
			b = candidate
			found = true
		}
		if !found {
			return nil, fmt.Errorf("selected firmware cannot be verified as supported ROM or persistent file-backed pflash using host metadata")
		}
		// Preserve requested selection constraints, not an assertion about future keys.
		b.SecureBoot = f.SecureBoot
		b.EnrolledKeys = f.EnrolledKeys
		applyFirmware(c, &b)
	}
	if b.LoaderFormat == "qcow2" && version < 9002000 {
		return nil, fmt.Errorf("qcow2 CODE requires libvirt >= 9.2")
	}
	if b.NVRAMFormat == "qcow2" && version < 10010000 {
		return nil, fmt.Errorf("qcow2 firmware requires libvirt >= 10.10 with matching template/private format support")
	}
	if b.Type == "efi" && (b.LoaderType != "pflash" || b.NVRAMTemplate == "") {
		return nil, fmt.Errorf("firmware must use persistent pflash and a file VARS template")
	}
	if err := validateFirmwareFiles(uri, b); err != nil {
		return nil, err
	}
	raw, e := c.Marshal()
	if e != nil {
		return nil, e
	}
	if _, e = conn.DomainXMLToNative("qemu-argv", raw, 0); e != nil {
		return nil, fmt.Errorf("explicit firmware binding unsupported by selected backend: %w", e)
	}
	return &b, nil
}

func sameFirmware(saved, observed hardware.FirmwareBinding) bool {
	saved.Owned = false
	saved.OwnerVM = ""
	saved.Namespace = ""
	saved.Evidence = ""
	observed.Owned = false
	observed.OwnerVM = ""
	observed.Namespace = ""
	observed.Evidence = ""
	// These are selection requirements. Libvirt versions may omit them in saved
	// XML for explicit loaders; compare them separately when actually present.
	if observed.SecureBoot == nil || saved.SecureBoot == nil {
		observed.SecureBoot = nil
		saved.SecureBoot = nil
	}
	if observed.EnrolledKeys == nil || saved.EnrolledKeys == nil {
		observed.EnrolledKeys = nil
		saved.EnrolledKeys = nil
	}
	return reflect.DeepEqual(saved, observed)
}
func validateFirmwareAdoption(c *x.Domain, p hardware.Profile, binding *hardware.FirmwareBinding) error {
	if err := supportedFirmwareXML(c); err != nil {
		return err
	}
	if p.FirmwarePolicy == nil {
		return nil
	}
	f := p.FirmwarePolicy
	o := observeFirmware(c)
	if binding != nil && binding.Loader == o.Loader && binding.NVRAMTemplate == o.NVRAMTemplate {
		if o.SecureBoot == nil {
			o.SecureBoot = binding.SecureBoot
		}
		if o.EnrolledKeys == nil {
			o.EnrolledKeys = binding.EnrolledKeys
		}
	}
	if f.Explicit && f.Type != "auto" && o.Type != f.Type {
		return fmt.Errorf("existing firmware type conflicts with explicit policy; firmware migration is unsupported")
	}
	if f.Type == "efi" && f.Explicit {
		secure := o.SecureBoot
		if secure == nil && o.Secure != "" {
			v := o.Secure == "yes"
			secure = &v
		}
		if secure == nil || *secure != *f.SecureBoot {
			return fmt.Errorf("existing firmware does not establish requested Secure Boot support")
		}
		// Initial keys cannot be inferred from mutable private NVRAM contents.
		if *f.SecureBoot && f.EnrolledKeys != nil && (o.EnrolledKeys == nil || *o.EnrolledKeys != *f.EnrolledKeys) {
			return fmt.Errorf("existing firmware selection does not establish initial enrolled_keys policy")
		}
	}
	if binding != nil {
		expected := *binding
		expected.NVRAM = o.NVRAM
		if !sameFirmware(expected, o) {
			return fmt.Errorf("existing CODE/template/format differs from selected firmware profile; migration is unsupported")
		}
	}
	return nil
}
func firmwareUndefineFlags(c *x.Domain, plan hardware.Plan) vir.DomainUndefineFlagsValues {
	b := plan.Firmware
	if b != nil && b.Owned && b.OwnerVM == c.UUID && b.NVRAM != "" && b.NVRAM != b.Loader && b.NVRAM != b.NVRAMTemplate && sameFirmware(*b, observeFirmware(c)) {
		return vir.DOMAIN_UNDEFINE_NVRAM
	}
	if c.OS != nil && c.OS.NVRam != nil {
		return vir.DOMAIN_UNDEFINE_KEEP_NVRAM
	}
	return 0
}
func firmwareDrift(saved *hardware.FirmwareBinding, observed hardware.FirmwareBinding) error {
	if saved != nil && !sameFirmware(*saved, observed) {
		return &compute.AdoptionRequiredError{Message: "Firmware configuration differs from its saved policy; explicitly re-adopt a compatible profile. Firmware migration/reset is unsupported."}
	}
	return nil
}

func (repo *VirtualMachineRepository) prepareFirmwareAdoption(vm *compute.VirtualMachine, plan *hardware.Plan, c *x.Domain) error {
	var binding *hardware.FirmwareBinding
	if f := plan.Profile.FirmwarePolicy; f != nil && f.Profile != "" {
		conn, err := repo.pool.Acquire(vm.NodeId)
		if err != nil {
			return err
		}
		defer repo.pool.Release(vm.NodeId)
		platform := domainPlatform(c)
		caps, err := queryDomainCapabilities(conn)(platform.Emulator, vm.Arch.String(), platform.Machine, platform.DomainType)
		if err != nil {
			return err
		}
		binding, err = repo.resolveFirmware(conn, plan.Profile, repo.settings[vm.NodeId].FirmwareProfiles, caps, vm.NodeId)
		if err != nil {
			return err
		}
	}
	if f := plan.Profile.FirmwarePolicy; f != nil && f.Profile == "" && f.Explicit && f.Type == "efi" {
		observed := observeFirmware(c)
		descriptors, err := hostFirmwareDescriptors(repo.pool.nodeUri[vm.NodeId])
		if err != nil {
			return err
		}
		for _, d := range descriptors {
			if d.Mapping.Executable.Filename == observed.Loader && d.Mapping.NVRAMTemplate.Filename == observed.NVRAMTemplate {
				if err := hardware.ValidateFirmwareDescriptor(d, *f, c.OS.Type.Arch, c.OS.Type.Machine); err != nil {
					continue
				}
				b := d.Binding()
				binding = &b
				break
			}
		}
		if binding == nil {
			return fmt.Errorf("existing firmware initial security policy cannot be established from host metadata; select a compatible named artifact contract")
		}
	}
	if err := validateFirmwareAdoption(c, plan.Profile, binding); err != nil {
		return err
	}
	observed := observeFirmware(c)
	if binding != nil {
		observed.SecureBoot = binding.SecureBoot
		observed.EnrolledKeys = binding.EnrolledKeys
		observed.Evidence = binding.Evidence
	}
	// Re-adoption retains positively proven ownership only of the exact same file.
	if old, err := hardware.DecodePlan(vm.AppliedPolicy); err == nil && old.Firmware != nil && old.Firmware.Owned && old.Firmware.OwnerVM == vm.Id && sameFirmware(*old.Firmware, observed) {
		observed.Owned = true
		observed.OwnerVM = vm.Id
		observed.Namespace = string(vm.NamespaceID)
	}
	plan.Firmware = &observed
	return nil
}
func (repo *VirtualMachineRepository) recordFirmwareAllocation(domain *vir.Domain, vm *compute.VirtualMachine) error {
	plan, err := hardware.DecodePlan(vm.AppliedPolicy)
	if err != nil {
		return err
	}
	if plan.Firmware == nil {
		return nil
	}
	raw, err := domain.GetXMLDesc(vir.DOMAIN_XML_INACTIVE | vir.DOMAIN_XML_SECURE)
	if err != nil {
		return err
	}
	var c x.Domain
	if err = c.Unmarshal(raw); err != nil {
		return err
	}
	if err = supportedFirmwareXML(&c); err != nil {
		return err
	}
	observed := observeFirmware(&c)
	saved := *plan.Firmware
	saved.NVRAM = observed.NVRAM
	if !sameFirmware(saved, observed) {
		return fmt.Errorf("defined firmware differs from accepted binding; inspect partial creation without automatic retry")
	}
	if saved.Type == "efi" && (saved.NVRAM == saved.Loader || saved.NVRAM == saved.NVRAMTemplate) {
		return fmt.Errorf("private NVRAM aliases shared firmware; ownership not established")
	}
	// The domain was defined with an empty NVRAM destination. This path is chosen
	// by libvirt in its connection-specific standard directory, never by callers.
	plan.Firmware = &saved
	vm.AppliedPolicy, err = json.Marshal(plan)
	if err != nil {
		return err
	}
	if err = writeHardwareMetadata(&c, vm.AppliedPolicy); err != nil {
		return err
	}
	// Metadata-only update preserves the complete persistent firmware XML.
	metadata, err := xml.Marshal(hardwareXML{Snapshot: string(vm.AppliedPolicy)})
	if err != nil {
		return err
	}
	return domain.SetMetadata(vir.DOMAIN_METADATA_ELEMENT, string(metadata), "hardware", hardwareNamespace, vir.DOMAIN_AFFECT_CONFIG)
}

func (repo *VirtualMachineRepository) BackendArtifactRefs(vm *compute.VirtualMachine) []tasks.ResourceRef {
	plan, err := hardware.DecodePlan(vm.AppliedPolicy)
	if err != nil || plan.Firmware == nil {
		return nil
	}
	b := plan.Firmware
	if !b.Owned || b.OwnerVM != vm.Id || b.NVRAM == "" {
		return nil
	}
	return []tasks.ResourceRef{{Kind: "libvirt-nvram", Scope: vm.NodeId, ID: b.NVRAM}}
}
