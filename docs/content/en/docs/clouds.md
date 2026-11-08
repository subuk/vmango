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

Keep the `hardware_profile` and `image` definitions from the shipped configuration, or define images
using the [image guide](/docs/images/). Define each host's connection and backend
settings directly in its cloud. The example assumes an active libvirt network
named `default` and a writable pool named `default`; Vmango does not create them.

```hcl
cloud "local" {
    name = "Local cloud"
    # Keep the nested hardware profiles and image definitions from vmango.quickstart.conf here.

    zone "local" { name = "Local zone" }
    network "lan" { name = "Private LAN" }
    storage_class "standard" {
        name = "Standard storage"
        description = "Independent copies of source disks."
        clone_mode = "copy"
    }

    instance_type "small" {
        name = "Small"
        cpu = 2
        cpu_request = 1
        memory_mib = 2048
        boot_disk_gib = 20
    }

    host "local" {
        uri = "qemu:///system"
        config_drive_pool = "default"
        image_pool = "default"
        zone = "local"
        labels = { rack = "rack-a" }
        cpu_reserve = 1
        cpu_overcommit = 4
        memory_reserve_mib = 1024
        network "lan" { network = "default" }
        storage_class "standard" {
            pool = "default"
            format = "qcow2"
        }
    }
}
```

Each `host` owns its libvirt connection and belongs to exactly one cloud and AZ.
To add another host, add a `host` block with its own `uri`, zone and network/pool
bindings. Host names and connection URIs must be globally unique across clouds;
several hosts can share the same AZ. Use architecture-appropriate image IDs;
unsupported combinations are rejected.

Connection settings `uri`, `image_pool`, `config_drive_pool`,
`config_drive_suffix`, `emulator`, `qcow_prealloc_metadata` and `cache` belong
in `cloud.host`. Pools default to `default`; the config-drive suffix defaults
to `_config.iso`. Hardware profiles own config-drive format and attachment.
New graphics listeners are derived and loopback-only. Legacy
`config_drive_write_format` and `graphics_listen` warn and are ignored.

Every image references a named `hardware_profile` in its cloud. See the
[release migration guide](https://github.com/subuk/vmango/blob/master/docs/HARDWARE-PROFILES-MIGRATION.md) for before/after examples,
existing VM adoption and the incompatible task-format boundary.

### Migrate existing connections

Move the contents of each former top-level `libvirt "name"` block into
`host "name"` in its cloud, then remove the top-level block. Preserve the exact
host name, URI and pool settings so resource routes, task scopes and image
ownership stay associated with the same backend. Top-level `libvirt` blocks and
legacy `libvirt_uri` / `libvirt_config_drive_*` options now fail startup with a
migration instruction. Backend settings do not belong in the public cloud API.

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

## Storage class cloning policy

Set `clone_mode` on the cloud-level storage class to `copy` (the default) or
`overlay`. Host bindings still choose the destination pool and format. Users
select a class when creating a VM or cloning a volume; there is no per-operation
mode override, and an image needs no extra settings.

```hcl
storage_class "efficient" {
    name = "Space-efficient"
    description = "Disks share their source and store only changes."
    clone_mode = "overlay"
}
```

Bind this class on each applicable host with a pool and `format = "qcow2"`.
The source of the operation is the base: a managed image, a path-based catalog
image or an otherwise eligible existing volume. A `copy` class creates an
independent disk. An `overlay` class creates a space-saving disk that continues
to depend on its source. Blank disks are created normally in either class.
The policy and source identity are pinned for accepted tasks; later configuration
changes affect future operations, not accepted tasks or existing disks. Disk
information shows the dependency observed from the disk itself.

Only one level is supported: a raw or qcow2 base without a backing store, followed
by a qcow2 overlay. Several overlays may share one base. Creating an overlay from
another overlay is rejected; copying a one-level overlay to a `copy` class creates
an independent disk. Conflicting destination formats are rejected without fallback
or configuration changes. Requested capacity must satisfy source size and image
minimums. A larger disk does not automatically enlarge guest partitions or
filesystems. Downloaded managed images still require an active directory pool
and must not contain backing files.

Administrators are responsible for pool/backend compatibility. Libvirt may reject
an unsupported operation during provisioning, or the subsequent VM start may fail.
Vmango verifies created disk metadata and reports partial results rather than
silently changing clone mode or deleting a shared base during cleanup.

Keep bases available and unchanged for as long as any dependent disk exists,
including when dependent VMs are stopped or their disks are detached. Through
Vmango, referenced bases cannot be deleted, resized or overwritten, and a VM
cannot start with a referenced base opened writable. These checks discover current
dependencies across the libvirt host, including unmanaged VMs and unattached
volumes. An incomplete scan or unsupported deeper chain prevents operations that
need a reliable dependency check. Removing an overlay removes only that disk;
detaching one does not remove its dependency.

These checks cover Vmango operations. Libvirt autostart, external tools, other
hosts and direct file changes bypass them. Keep native QEMU/libvirt locking
enabled. Vmango does not maintain a dependency registry, monitor external
changes, flatten or rebase disks, or automatically garbage-collect bases.

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
under `/api/v1/administration/clouds/{cloud}/hosts/{node}/networks`. Administrators provision those networks
outside Vmango.

Interfaces select a configured logical network, resolved on the existing host.
Interface attachment accepts `network` (a logical network ID) and optional `mac`.
NIC model and VLAN are selected by the administrator's host network binding;
`model`, `vlan` and other unsupported request fields are rejected. Observed
interface responses retain `model` and `vlan` for diagnostics.
Custom network creation remains outside this workflow.

Unmanaged VMs remain observable and counted. Ordinary mutations require adoption
with an explicitly selected compatible hardware profile. Older managed VMs without
applied policy require re-adoption. Adoption records policy without converting
hardware, changing listeners or claiming pre-existing disks. Deleting an adopted
VM preserves unowned disks.

Instance-type changes require a stopped managed VM on its current host. They
preserve disk sizes, hardware policy, hugepages, CPU pinning and autostart;
incompatible resources fail. Profile edits never reconfigure existing VMs or
accepted tasks. Ordinary disks attached later follow the VM's persisted policy.

Successful user starts, including creation with `start=true`, enable autostart.
Confirmed user stops disable it; graceful shutdown waits up to 120 seconds.
Failed/unconfirmed operations, reboot and guest-initiated shutdown leave it unchanged.
Autostart synchronization failures preserve the successful power operation in task effects.

Profiles own guest-agent, graphics/video and config-drive policy. New graphics
enabled VMs listen only on host loopback. Remote console connections use SSH;
non-SSH libvirt transports try SSH port 22 on the host. Failed SSH only makes the
console unavailable; management remains available without direct VNC fallback.
Existing VM listeners remain unchanged. Effective settings are read-only diagnostics.

## System disks and boot order

Vmango assigns an explicit boot priority to the system disk when creating a VM
or attaching disks. The `root` attachment alias identifies the system disk. If
there is only one ordinary disk, it can be selected without an alias. The
`configdrive` disk contains cloud-init data and is never selected automatically
as the system disk, even though it is attached through virtio.

When reattaching a system disk to a machine with other data disks, set its alias
to `root`. Vmango does not guess which of several unlabelled disks contains an
OS. Device names and PCI addresses are preserved; the system disk need not be
`vda`.

Legacy `hd`/`cdrom` boot settings are converted on disk attachment when a system
disk can be identified. An already attached installation CD keeps its configured
precedence. Existing explicit bootable hard disks, direct kernel boot and legacy
PXE-only or mixed PXE boot policies are preserved. With per-device priorities, a
reattached system disk is added after existing optical/network boot entries when
no hard disk has a priority. Newly attached installation media can be selected
through the firmware boot menu or an explicit libvirt boot configuration.

Existing machines are not rewritten or restarted merely by upgrading Vmango.
Disk attachment requires a stopped VM, and the updated boot order takes effect
on its next start.

## API and migration

Machine, disk, interface and native console operations use `/api/v1/clouds/{cloud}`.
Create uses catalog references and an AZ without a host input. Machine URLs use
the stable VM UUID. Volumes use `/clouds/{cloud}/volumes/{volume}` with unpadded
`base64url(node) + "." + base64url(path)` as the compound ID. VM disk references
and `existing_disks: [{volume_id}]` use that same ID without a separate public
`host` field. These addresses do not move a resource. Every request validates
cloud membership. All mutations use durable tasks with visible progress and
diagnostics.

Volumes, like Machines, shows one selected cloud's flat inventory. Both list APIs
return `{items, unavailable_hosts}`; partial failures preserve successful rows and
remain explicit even when every host fails. VM/volume detail provides the logical
catalog needed by ordinary forms. A null catalog means supplemental discovery
failed; empty arrays mean known-empty choices. Dedicated infrastructure browsing
is under Administration → Clouds → Cloud; Tasks keeps its existing URLs.

The old `/api/v1/nodes/...`, `/api/v1/hosts/...`, host-segment volume routes and
global `/vms`, `/volumes` and `/networks` endpoints are removed without aliases. Read-only administrative diagnostics use
`/api/v1/administration/clouds/{cloud}/hosts/...`.
Authentication remains installation/principal scoped. Workloads, SSH keys,
cloud-init templates and workload task history require an explicit configured
namespace. The same cloud may serve several namespaces; Administration inventory
and history remain independent of workspace selection. See the repository
[namespace user guide](/docs/namespaces/) for configuration and everyday use,
or the [migration guide](https://github.com/subuk/vmango/blob/master/docs/NAMESPACES.md)
for existing installations.

See [Automation API](/docs/api/) for Swagger UI, authentication and an overview
of supported operations. The repository's
[API reference](https://github.com/subuk/vmango/blob/master/docs/API.md) lists the
routes, task behavior and console examples. Binary
archives include `QUICKSTART.md`, `API.md`, `CLOUDS.md`, `IMAGES.md`, `TASKS.md`
and `MIGRATION.md` for offline reference. They use the maintained repository
guides, with internal guide links adjusted to the archive filenames.

An inactive storage pool or a domain disk whose metadata cannot be read makes
the host dependency scan incomplete. Dependency-sensitive operations can then
be blocked even for other disks on that host; the administrator must restore
a complete, readable inventory before retrying.
