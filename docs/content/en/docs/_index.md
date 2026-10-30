---
title: "Documentation"
description: "Install Vmango, connect your libvirt hosts and launch virtual machines."
weight: 1
menu:
  main:
    weight: 10
---

Vmango manages virtual machines across multiple local or remote libvirt hosts
from one web interface. Configure your
hypervisor, storage pools and networking first, then connect Vmango to launch
and manage machines from your browser.

## Get started

- [Binary quick start](/docs/start/): download a release, connect a host and
  configure your web login.
- [Multiple hosts](/docs/start/#manage-multiple-hosts): connect several
  hypervisors to one Vmango instance.
- [Package installation](/docs/packages/): install release packages on Ubuntu,
  Debian or RHEL.
- [Configured clouds and scheduling](/docs/clouds/): define instance types, AZs,
  logical networks, storage classes and automatic placement.
- [Cloud images and provisioning](/docs/images/): choose an OS image, register
  it in Vmango and create your first machine.
- [Build from source](/docs/build/): build and run Vmango for development.

The [example configuration](https://github.com/subuk/vmango/blob/master/vmango.dist.conf)
documents settings for hosts, images, authentication and event hooks.

## Explore the interface

Review machine states, configured resources and IP addresses in one inventory.
Filter by host or state, and open a machine to inspect its settings or connect to
its console. These screenshots use demonstration data; select a screenshot to
view it at full size.

{{< figure src="/screenshots/machines.webp" link="/screenshots/machines.webp" alt="Vmango machine inventory with state filters, resource summaries and IP addresses." caption="Machine inventory and resource summaries." >}}

The creation form shows the CPU, memory and boot disk included with the selected
instance type before you launch a machine.

{{< figure src="/screenshots/create-machine.webp" link="/screenshots/create-machine.webp" alt="Machine creation with an instance type providing 4 vCPUs, 4 GiB of memory and a 40 GiB boot disk." caption="Choose an image, instance type, network and SSH keys. Demonstration data." >}}

## A cloud configuration example

Replace the `cloud "local"` block in the
[quick-start configuration](/docs/start/) with this example, keeping its image
and API settings. It connects a local Linux/KVM host and offers two instance
types. It uses the existing `ubuntu-24.04-x86_64-bios` image definition and assumes
an active libvirt network and a writable storage pool, both named `default`.

{{< cloud-example >}}

Continue with [configured clouds and scheduling](/docs/clouds/) to add hosts,
choose resource reserves and configure placement rules.
