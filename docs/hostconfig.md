# Host configuration

Various aspects of host configuration.

- [Hugepages](#Hugepages)
- Vcpu pinning
- NUMA pinning
- PCI passthrough
- CPU isolation

Also see [RedHat virtualization tuning and optimization guide](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/7/html-single/virtualization_tuning_and_optimization_guide/index).

## Kernel options configuration

Some options require custom kernel boot parameters and configuration process is different for each linux distribution.

### Ubuntu

Summary of [official documentation](https://wiki.ubuntu.com/Kernel/KernelBootParameters) below.

Open `/etc/default/grub` and add options to `GRUB_CMDLINE_LINUX_DEFAULT`:

    ...
    GRUB_CMDLINE_LINUX_DEFAULT="quiet splash intel_iommu=on"
    ...

Regenerate grub configuration file

    update-grub

Reboot the system to apply the changes.

### CentOS

Create new [tuned](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/7/html/performance_tuning_guide/sect-red_hat_enterprise_linux-performance_tuning_guide-performance_monitoring_tools-tuned_and_tuned_adm) profile named `local` or any other name.
You can inherit another standard profile for better performance, `virtual-host` in this example, run `tuned-adm list` for all available profiles.

    mkdir /etc/tuned/local/
    cat > /etc/tuned/local/tuned.conf <<EOF
    [main]
    summary=Local profile
    include=virtual-host

    [bootloader]
    cmdline=intel_iommu=on
    EOF

Reconfigure bootloader by applying new profile

    tuned-adm apply local

Reboot the system to apply the changes.


## Hugepages

You should always enable hugepages if you care about virtual cpu performace.
Due to memory fragmentation the most reliable way to enable hugepages is kernel boot options.

For example the following options will configure 2M hugepages as default for your system, disable
transparent hugepages and allocate 2G of memory. This memory will be reported as used by standard
utilities like `top` and `free` but you can start vm with 2G of memory.

    transparent_hugepages=never default_hugepagesz=2M hugepages=1024

The same for 1G hugepages

    transparent_hugepages=never default_hugepagesz=1G hugepages=2
