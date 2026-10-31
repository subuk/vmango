package libvirt

import libvirtxml "libvirt.org/go/libvirtxml"

// applyDomainPowerPolicy gives created and adopted guests the same power
// management behavior. Hardware changes take effect on the next full start.
func applyDomainPowerPolicy(config *libvirtxml.Domain) {
	config.OnPoweroff = "destroy"
	config.OnReboot = "restart"
	config.OnCrash = "destroy"
	if config.OS == nil || config.OS.Type == nil || config.OS.Type.Type != "hvm" {
		return
	}
	arch := config.OS.Type.Arch
	if arch != "x86_64" && arch != "i686" {
		return
	}
	if config.Features == nil {
		config.Features = &libvirtxml.DomainFeatureList{}
	}
	// Preserve existing platform features and APIC options.
	if config.Features.ACPI == nil {
		config.Features.ACPI = &libvirtxml.DomainFeature{}
	}
	if config.Features.APIC == nil {
		config.Features.APIC = &libvirtxml.DomainFeatureAPIC{}
	}
}
