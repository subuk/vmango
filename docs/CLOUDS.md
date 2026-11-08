# Configured libvirt clouds

A cloud exposes a catalog of images, instance types, availability zones, logical
networks and storage classes. Administrators configure the existing libvirt hosts
and bindings. Users select catalog entries; the scheduler chooses a host. Vmango
currently implements only libvirt. Placement intent and capability discovery are
independent of libvirt so future provider adapters can implement supported features.

See the [hardware-profile migration guide](https://github.com/subuk/vmango/blob/master/docs/HARDWARE-PROFILES-MIGRATION.md) before upgrading.
Every libvirt image must select an explicit named hardware profile.

See the complete `cloud` example in `vmango.dist.conf`. Each nested `host` owns
its libvirt connection (`uri`) and backend settings, alongside its zone, reserves,
network and storage bindings. Host names and URIs must be globally unique across
clouds. Do not configure aliases for the same connection. Multiple hosts can
share a zone.
Catalog IDs are separate from display names. Each cloud owns its nested image
definitions; the same image ID in another cloud is independent. Image storage
is bound separately on each configured host.

## System disk boot selection

Creation and disk attachment assign an explicit libvirt boot priority to the
disk with the `root` attachment alias, or to the only ordinary disk when that
alias is absent. The `configdrive` attachment is excluded. Reattach a system
disk with alias `root` when other data disks are present; Vmango does not infer
the OS disk from several unlabelled disks or from filenames. Guest device names
and PCI addresses are not changed.

Legacy `hd`/`cdrom` boot settings are converted on attachment, preserving the
relative priority of an already attached installation CD. Existing explicit
bootable hard disks, direct kernel boot and legacy PXE policies are preserved.
If only non-hard-disk devices have explicit priorities, the system disk is added
after them. Newly attached installation media can be selected in the firmware
boot menu or configured explicitly through libvirt.

Upgrades do not rewrite or restart existing VMs. The boot change applies when
creating a VM or attaching a disk to a stopped VM, on its next start.

## Cloud-owned image migration

Top-level `image` blocks and `cloud.images = [...]` references are removed.
Move every image definition inside each owning `cloud` and delete the reference
list. Formerly shared definitions must be copied explicitly into every cloud
that uses them. Image IDs match `^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$` and are
unique within a cloud; the same ID in another cloud can have different sources
and launch defaults. Lookup never falls back to another cloud.

```hcl
cloud "local" {
    hardware_profile "linux-uefi" { firmware = "efi" }
    image "ubuntu-local" {
        path = "/var/lib/libvirt/images/ubuntu-cloud.img"
        os_name = "Ubuntu"
        os_arch = "x86_64"
        hardware_profile = "linux-uefi"
        protected = true
    }
    # Keep zones, types, networks, storage classes and host bindings here.
}
```

For path-labeled images, choose a stable ID and copy the old label into `path`.
For downloadable images, keep the accepted URL, checksum, format and byte size
inside the new nested block. A legacy image block used only for `hidden` or
`protected` volume policy becomes a top-level `volume "path" { ... }` block,
not a catalog image. Explicit volume policy retains its existing path semantics;
image-derived policy applies only to hosts in the image's owning cloud. Equal
paths in unrelated clouds do not share image policy or attachment exclusions.
`hidden`/`protected` describe source-volume policy, not catalog visibility.

Restart to activate the new file. Preserve the task directory and its sibling
`-images` and `-volumes` metadata stores. Unfinished tasks accepted before the hardware-profile command-format boundary fail
without execution. Terminal history, recorded effects and uncertainty locks remain
intact. New-format tasks retain their accepted policy through ordinary restarts;
see the hardware-profile migration guide before resubmitting old work.

## Host configuration migration

Move every setting from each top-level `libvirt "name"` block into the matching
`cloud { host "name" { ... } }` block and remove the old block. This includes
`uri`, `image_pool`, `config_drive_pool`, `config_drive_suffix`,
`emulator`,
`qcow_prealloc_metadata` and `cache`. These settings are direct host attributes;
there is no nested `libvirt` block. Preserve host names, exact URIs and pool
settings to retain resource identities, task scopes and image ownership.

Top-level `libvirt` blocks and legacy `libvirt_uri` / `libvirt_config_drive_*`
options are rejected with migration guidance, including when old and new formats
are mixed. Hosts must be declared within a cloud. Backend settings stay private;
public cloud discovery exposes catalog choices and host diagnostics.

For example, inside an existing cloud with a `local` zone, `lan` network and
`standard` storage class, configure the complete host in one place:

```hcl
host "local" {
    uri = "qemu:///system"
    image_pool = "default"
    config_drive_pool = "default"
    zone = "local"
    cpu_reserve = 1
        cpu_overcommit = 4
    memory_reserve_mib = 1024
    network "lan" { network = "default" }
    storage_class "standard" { pool = "default" }
}
```

Omitted `image_pool` and `config_drive_pool` default to `default`, and
`config_drive_suffix` to `_config.iso`. `cache` and `qcow_prealloc_metadata`
default to `false`. Legacy `config_drive_write_format` and `graphics_listen`
warn and are ignored: format belongs to the profile, and listeners are derived.

## Catalog and placement

Instance types configure vCPU count (`cpu`), guest RAM (`memory_mib`) and the new
boot disk size (`boot_disk_gib`). Admission always counts full configured vCPUs;
`cpu_request` has been removed and must be deleted from configuration. Memory requests default to the limit and must equal it;
memory overselling and automatic ballooning are unsupported. Images own artifact sources and minimum resource requirements, and select a named
libvirt hardware profile. Profiles own guest devices, hugepages and config-drive
policy. Defaults include guest agent enabled, VNC, no video device and NoCloud on
a virtio disk. Instance types do not select or override hardware profiles.
A type too small for the image is rejected, including when actual source virtual
capacity exceeds the boot disk. Successful user starts, including `start=true`
on creation, enable autostart; confirmed user stops disable it.

Host CPU and RAM reserves reduce allocatable budgets. Every create inventories
hosts in the cloud, including inactive and unmanaged domains. A host with any
failed inventory read is excluded from placement entirely; it is never counted
as empty. Operations on an existing machine require fresh inventory only on its
current host. Existing domains without scheduler metadata consume requests
equal to actual configured limits. Managed domains carry a namespaced applied
snapshot in their XML; catalog edits cannot change their allocations. Host
selection filters required constraints and compatibility, then scores preferred
label matches, remaining CPU and remaining RAM, with host ID as a stable tie-break.

The optional Placement section uses equality selectors on advertised placement
labels. `required` labels filter candidates; `preferred` labels influence scoring.
Unsupported attributes are rejected. Physical host IDs and libvirt connection
URIs are not placement selectors. No placement hints means automatic scheduling.
The observed host appears read-only in machine details.

Tasks sharing cloud capacity execute serially. Inventory, admission, lazy image
preparation and provisioning are covered by the same execution group. Volume
creation, cloning and growth participate in it. Sparse volumes are accounted by
logical capacity as well as physical free space. Provisioning rechecks storage
after image preparation. Interrupted tasks retain uncertain outcomes and block
the cloud group until explicit inspection and lock release; they never resume
automatically. This is a single-writer application, not a distributed scheduler;
external hypervisor changes still require fresh backend validation and may cause
an operation to fail. Do not run independent Vmango writers against the same hosts.

A missing downloadable image does not exclude a host. Its image-pool binding and
preparation capacity must be valid. Pin `source_size_bytes` when known (the
shipped catalog uses verified artifact sizes). For unknown source lengths,
admission conservatively budgets the configured image transfer byte limit.
Downloads retain KRG-55 verification and
interruption behavior. A successfully prepared image can remain after a later
machine provisioning failure. No image bytes are cached on the Vmango server.

## Local disks and interfaces

A storage class maps to a concrete pool and format on each host. Selecting the
same class on two hosts does not make their disks shared. New disks for an
existing VM use that VM's host; standalone disks are placed within their selected
zone and remain bound to the chosen host, including after detachment. Additional
disk sizes are independent of the instance type. Clones remain on the source
host and select a destination storage class on it.

Existing disks attached during creation force the VM onto their owning host.
Conflicting placement constraints, disks from different hosts, unavailable
bindings and insufficient resources cause errors. Vmango never silently copies
a disk or moves the VM. Resource operations use stored backend identity rather
than reinterpreting a changed storage-class mapping.

A host network binding specifies exactly one `network` (a prepared active libvirt
network) or `bridge` (a prepared host bridge). Direct remote bridges remain the
administrator's responsibility because libvirt does not inventory arbitrary host
bridges through its network API.

The UI resolves logical networks on the machine's existing host. It exposes
supported choices without raw pool, network-driver or NIC settings. User network
creation, host provisioning and pool creation are outside this feature.

Volume ownership is stored durably in a private directory next to task storage
(`tasks.directory` plus `-volumes`). Preserve it and the task/image metadata across
restarts. Missing ownership records are not reconstructed from names or adoption.
Only confirmed owned volumes can be deleted; protection and attachment rules
still apply. Ownership is revoked durably before deletion so a later disk at
the same path cannot inherit a stale record. An interrupted or failed deletion
requires explicit inspection; revocation does not prove the disk was deleted.
Machine deletion preserves attached disks unless deletion is
explicitly requested, and always preserves unowned disks.

## Existing machines and instance type changes

Unmanaged machines remain visible and count toward admission. Ordinary mutations
require explicit adoption with a compatible named hardware profile; previously
managed machines without persisted applied policy require re-adoption. Adoption
validates existing hardware and records policy without rewriting hardware, disk
addresses, power state or graphics listeners. It never infers a profile from XML
or claims existing disks. Incompatible configurations require explicit resolution.

Change instance type only while a managed VM is stopped. Admission replaces its
old requests on the current host; insufficient resources fail the command. There
is no automatic stop, migration or restart. Disk sizes, applied guest policy,
hugepages, explicit pinning and autostart are preserved. Later disk operations use
the persisted ordinary-disk policy, independent of catalog edits or task retention.

New graphics-enabled VMs have an explicit loopback-only listener; graphics-disabled
VMs have none. Local consoles connect locally; remote consoles use non-interactive,
host-key-verified SSH. SSH libvirt URIs retain applicable SSH settings. Other remote
transports try the connection hostname on SSH port 22, never the libvirt API port.
Console SSH failure does not affect creation or ordinary management, and never
falls back to direct remote VNC. Existing listeners are preserved. Remote browser
console access through Vmango requires HTTPS.

Autostart follows confirmed user power operations. Graceful shutdown waits up to
120 seconds; an unconfirmed result leaves autostart unchanged. Reboot, guest shutdown
and type changes do not synchronize it. A synchronization failure after successful
power is reported separately with the successful operation retained in task effects.

## API migration

`GET /api/v1/clouds` returns public catalogs, compatible offers and placement
capabilities. Backend pool names, host bindings and source image paths are not
part of the catalog. Create a machine with an idempotency key:

```json
{
  "name": "demo",
  "image": "ubuntu-24.04-x86_64-bios",
  "instance_type": "small",
  "zone": "local",
  "network": "lan",
  "storage_class": "standard",
  "start": true,
  "placement": { "preferred": { "rack": "local" } }
}
```

Submit this body to `POST /api/v1/clouds/local/machines`. It returns 202 with a
durable task reference. Follow the task's stages, effects and confirmed outcome;
acceptance is not completion. Existing disks use `existing_disks: [{volume_id}]`,
with the compound ID returned by discovery and no separate public `host` field.
The ID reversibly encodes its host and path; raw paths are not create inputs.

Queued commands accepted before the cloud migration fail before backend mutation
if they lack cloud context; inspect their history and submit a new cloud command.

All former `/api/v1/nodes/...` resource routes and global `/vms`, `/volumes` and
`/networks` collections are removed without compatibility aliases. Read-only
administrative diagnostics use `/api/v1/administration/clouds/{cloud}/hosts/...`. CPU/RAM
patches are replaced by `POST /api/v1/clouds/{cloud}/machines/{vm}/instance-type`
with `{"instance_type":"medium"}`. Machine URLs use the stable machine ID (the libvirt domain UUID), independently
of its name or observed host. Names must be unique only within a host. Adoption uses the same
resource URL with `/adopt`. New disk creation and attachment uses `/disks` with
`name`, `storage_class` and `size_bytes`. Attachment, power, console and deletion
commands use that same cloud-scoped identity. Existing-disk attachment uses `/disks/attach`; `/disks` creates and
attaches a new disk. Cloud membership is checked before reads, commands and
native console upgrades.

`GET /api/v1/clouds/{cloud}/volumes?usage=attachable` excludes image sources and
already attached disks. The creation form further restricts existing-disk choices
to one owning host after the first selection.

Standalone creation uses `POST /api/v1/clouds/{cloud}/volumes` with `name`, `zone`,
`storage_class` and `size_bytes`. Existing-volume clone commands select
`storage_class`, not `pool` or `format`. Read-only host and pool discovery remain
available under `/administration/clouds/{cloud}/hosts` for administrative
diagnostics. Workload forms use the logical catalogs included in resource detail,
without requiring host-administration requests. See authenticated OpenAPI for the
complete schema.

## Machine discovery and routing

Machine reads return separate `id` and `name` fields. API routes use
`/api/v1/clouds/{cloud}/machines/{id}` and browser routes use
`/machines/{cloud}/{id}/`. Existing libvirt UUIDs are preserved. Old host/name
machine routes are removed; update API clients and bookmarks. Volume routes use
`/api/v1/clouds/{cloud}/volumes/{volume}` without a separate host segment. For
libvirt, `volume` is unpadded `base64url(node) + "." + base64url(path)`; old
path-only IDs and host-segment routes are removed without aliases. VM disk
references, attachment/detachment and existing-disk creation use the same ID.
Backend identities, ownership and durable task references/locks remain unchanged.
Signed console links bind the cloud
and machine UUID; issue new links after upgrading.
Event hooks receive the UUID in `vm_id` and the display name in `vm_name`.

`GET /api/v1/clouds/{cloud}/volumes` returns `{items, unavailable_hosts}` as well.
Volumes is a flat list of the selected cloud across hosts and pools; the UI uses
`?cloud=` like Machines and never implicitly combines unrelated clouds. Partial
and all-unavailable reads return 200 with failed host IDs. Empty items without
failures confirm an empty list. VM and volume detail contain applicable logical
`catalog` choices; `catalog: null` means discovery failed while the resource
remains readable, and empty arrays mean known-empty choices. Lists do not fetch
a catalog for every row.

`GET /api/v1/clouds/{cloud}/machines` returns `items` and `unavailable_hosts`.
Each request reads hosts afresh, with at most 20 concurrent machine-list backend
reads. Concurrent scans of the same cloud share the in-flight read. Results from
failed hosts are omitted and the UI explicitly marks the list incomplete. Cloud
catalogs remain discoverable and expose `unavailable_hosts` for failed live checks.
Reads finish according to the backend's connection and operation timeouts; the
parallel scan does not impose a separate libvirt deadline. Fresh per-host reads
are not an atomic snapshot across hosts.

An in-memory routing index remembers only machine IDs and their observed hosts.
A point request uses that hint and reads live state from the host. A missing hint
or a definite not-found triggers a fresh cloud scan. Connection errors do not
trigger retries of mutations. Successful host scans replace that host's hints;
failed scans preserve hints without retaining or serving old machine state.
Unknown IDs return 503 when discovery is incomplete, and 404 after a complete
scan confirms absence. Duplicate IDs across hosts return 409 rather than choosing
an arbitrary machine. There is no periodic refresh or persistent index.

Queued commands accepted with the former name-based identity are rejected before
execution. Inspect old tasks and resubmit through the new API. New commands retain
the resolved host and UUID, and idempotent recovery happens before backend reads.

Host `cpu_overcommit` is an integer multiplier (default `4`, minimum `1`). Full-vCPU capacity is `(online physical cores - cpu_reserve) * cpu_overcommit`; SMT threads are not independent cores; this applies to creation, adoption and instance type changes. Memory is not overcommitted.

## Storage clone policy

Cloud-level storage classes carry `description` and `clone_mode = "copy" | "overlay"`.
Omission selects `copy`; host bindings continue to choose pool and destination
format. The accepted command pins the policy and source identity. The same image
can serve either class, and blank disk creation is unaffected.

An overlay is a qcow2 child of an unbacked raw/qcow2 source. Multiple children are
allowed; deeper creation is rejected. A copy from a supported one-level overlay
must be independent. Backend readback verifies format, capacity and backing
relationship. Actual disk dependency is returned separately from today's class
policy; no source paths are exposed by the dependency presentation.

Dependency checks protect Vmango starts and base mutation/deletion by inspecting
the whole host, including stopped/unmanaged domains and unattached volumes.
Incomplete scans and unsupported chains fail the affected operation. Existing task
locks coordinate operations; dependencies are rediscovered after restart, without
a persistent registry. Administrators remain responsible for compatible backend
configuration and keeping bases available and unchanged while dependents exist.
External tools, other hosts and libvirt autostart bypass Vmango checks; keep native
QEMU/libvirt locking enabled. No flatten/rebase or automatic base cleanup is added.

An inactive storage pool or a domain disk whose metadata cannot be read makes
the host dependency scan incomplete. Dependency-sensitive operations can then
be blocked even for other disks on that host; the administrator must restore
a complete, readable inventory before retrying.

See [Guest CPU policies](https://vmango.org/docs/cpu-policies/) for CPU modes, full-vCPU migration, native placement, Hyper-V/KVM presentation and stopped resize.
