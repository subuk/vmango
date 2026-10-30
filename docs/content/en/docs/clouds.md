---
title: "Configured clouds and scheduling"
description: "Define a cloud catalog and automatically place machines on prepared libvirt hosts."
weight: 3
---

Vmango exposes configured clouds rather than asking users to assemble a libvirt
domain. Administrators prepare hosts, pools and networks and define their private
bindings. Users select an image, instance type, AZ, logical network and storage
class. The scheduler assigns a host, shown read-only in the machine details.

## Configure a cloud

Keep the `image` definitions from the shipped configuration, or define images
using the [image guide](/docs/images/). Add the following alongside the existing
`libvirt "local"` connection. The example assumes an active libvirt network named
`default` and a writable pool named `default`; Vmango does not create them.

```hcl
cloud "local" {
    name = "Local cloud"
    images = ["ubuntu-24.04-x86_64-bios"]

    zone "local" { name = "Local zone" }
    network "lan" { name = "Private LAN" }
    storage_class "standard" { name = "Standard storage" }

    instance_type "small" {
        name = "Small"
        cpu = 2
        cpu_request = 1
        memory_mib = 2048
        boot_disk_gib = 20
    }

    host "local" {
        zone = "local"
        labels = { rack = "rack-a" }
        cpu_reserve = 1
        memory_reserve_mib = 1024
        network "lan" { network = "default" }
        storage_class "standard" {
            pool = "default"
            format = "qcow2"
        }
    }
}
```

Each `host` references one configured libvirt connection and belongs to exactly
one cloud and AZ. To add another host, add its connection and a matching `host`
block with its own network/pool bindings. Several hosts can share the same AZ.
Use the architecture-appropriate image IDs; unsupported combinations are rejected.

## Resources and placement

`cpu` sets the actual vCPU count. `cpu_request` defaults to `cpu`, must be positive
and no greater than it, and affects admission only; it does not impose quotas or
CPU shares. Memory requests equal guest RAM; memory overselling is unsupported.
`boot_disk_gib` sets the size of a newly created boot disk and must satisfy the
image minimum. Additional disks have independent sizes.

Each create inventories all cloud hosts, including stopped and unmanaged VMs.
Existing VMs without scheduler metadata count at their actual configured CPU/RAM.
Failed inventory reads fail admission. Host reserves reduce the available budget;
disk and lazy image preparation capacity are checked too. Competing provisioning
and storage commands execute serially so they cannot admit the same capacity twice.

Placement is optional. Required equality labels filter candidates; preferred
labels affect scoring and do not guarantee a match. Only advertised attributes
are accepted. These semantics are independent of libvirt node names. With no
hints, placement is automatic within the selected AZ.

## Disks, interfaces and existing machines

A storage class maps to a pool on each host. The same class on two hosts does not
make storage shared. A standalone disk is scheduled within its AZ and remains
host-bound after detachment. A new disk for an existing machine uses that machine's
host. Selecting existing local disks during creation constrains placement to their
owning host; disks from different hosts cannot be combined or silently copied.

The main **Networks** page lists the selected cloud’s logical network catalog,
including display names and network IDs. Its cloud selector scopes the list;
multiple host bindings for one logical network appear as one entry. Backend
libvirt network names are shown as read-only diagnostics on each **Hosts** detail
page and remain available through the read-only host diagnostic API
under `/api/v1/hosts/{node}/networks`. Administrators provision those networks
outside Vmango.

Interfaces select a configured logical network, resolved on the existing host.
Interface attachment accepts `network` (a logical network ID) and optional `mac`.
NIC model and VLAN are selected by the administrator's host network binding;
`model`, `vlan` and other unsupported request fields are rejected. Observed
interface responses retain `model` and `vlan` for diagnostics.
Custom network creation remains outside this workflow.

Unmanaged VMs are visible and counted, but read-only until explicit adoption.
Adoption does not change resources, power state, disks or placement, and does not
claim ownership of pre-existing disks. Deleting an adopted VM preserves unowned
disks. Preserve the private volume ownership metadata alongside task history.

An explicit instance type change requires a stopped managed VM, stays on its
current host, applies the selected type's `autostart` policy and preserves all
disk sizes, applied guest devices and graphics listener bindings. If that host lacks resources, the task
fails; there is no automatic migration, stop or restart. Catalog edits never
silently resize or reconfigure existing machines or disks.

Set `autostart` in `instance_type` blocks (default `false`). Image settings
`guest_agent`, `graphics` and `video_model` control guest integration and devices;
their defaults are `true`, `vnc` and `none`. The private `libvirt` connection
sets `graphics_listen` as an IP address, with omission preserving the backend
default. Creation's `start` flag is a separate one-time action. Adoption preserves
existing policy and devices, and accepted tasks snapshot policy and private host
bindings. None of these five settings accepts per-machine overrides; the machine
PATCH endpoint is removed and effective values remain read-only.

## API and migration

Machine, disk, interface and native console operations use `/api/v1/clouds/{cloud}`.
Create uses catalog references and an AZ without a host input. Existing resource
URLs include the observed owning host to distinguish local disks and legacy VM
names; it cannot be changed to move a resource. Every request validates cloud
membership. All mutations use durable tasks with visible progress and diagnostics.

The old `/api/v1/nodes/...` and global `/vms`, `/volumes` and `/networks` endpoints
are removed. Read-only administrative diagnostics use `/api/v1/hosts/...`.
Authentication, keys and task history remain application-wide.

The application serves interactive API documentation at `/api/docs`, authenticated
OpenAPI at `/api/openapi.json`, and the console protocol at `/api/console-protocol`.
The repository's [API reference](https://github.com/subuk/vmango/blob/master/docs/API.md)
lists the routes, task behavior, authentication and console examples. Binary
archives include `API.md`, `CLOUDS.md` and `MIGRATION.md` for offline reference.
