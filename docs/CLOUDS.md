# Configured libvirt clouds

A cloud exposes a catalog of images, instance types, availability zones, logical
networks and storage classes. Administrators configure the existing libvirt hosts
and bindings. Users select catalog entries; the scheduler chooses a host. Vmango
currently implements only libvirt. Placement intent and capability discovery are
independent of libvirt so future provider adapters can implement supported features.

See the complete `cloud` example in `vmango.dist.conf`. Each nested `host` owns
its libvirt connection (`uri`) and backend settings, alongside its zone, reserves,
network and storage bindings. Host names and URIs must be globally unique across
clouds. Do not configure aliases for the same connection. Multiple hosts can
share a zone.
Catalog IDs are separate from display names. Each cloud owns its nested image
definitions; the same image ID in another cloud is independent. Image storage
is bound separately on each configured host.

## Cloud-owned image migration

Top-level `image` blocks and `cloud.images = [...]` references are removed.
Move every image definition inside each owning `cloud` and delete the reference
list. Formerly shared definitions must be copied explicitly into every cloud
that uses them. Image IDs match `^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$` and are
unique within a cloud; the same ID in another cloud can have different sources
and launch defaults. Lookup never falls back to another cloud.

```hcl
cloud "local" {
    image "ubuntu-local" {
        path = "/var/lib/libvirt/images/ubuntu-cloud.img"
        os_name = "Ubuntu"
        os_arch = "x86_64"
        firmware = "efi"
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
`-images` and `-volumes` metadata stores. Accepted pre-migration tasks retain
pinned image sources/defaults, backend bindings, artifact identity, history,
ownership and locks; editing today's catalog does not retarget queued work.
A legacy task without safely established accepted cloud context fails before
mutation with a resubmission message while retaining its history and safety state.

## Host configuration migration

Move every setting from each top-level `libvirt "name"` block into the matching
`cloud { host "name" { ... } }` block and remove the old block. This includes
`uri`, `image_pool`, `config_drive_pool`, `config_drive_suffix`,
`config_drive_write_format`, `emulator`, `graphics_listen`,
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

Omitted `image_pool` and `config_drive_pool` default to `default`,
`config_drive_suffix` to `_config.iso` and `config_drive_write_format` to
`nocloud`. An omitted `graphics_listen` preserves the backend's default listener;
`cache` and `qcow_prealloc_metadata` default to `false`.

## Catalog and placement

Instance types configure the actual positive integer vCPU count (`cpu`), admission
requests (`cpu_request`, defaulting to `cpu`), guest RAM (`memory_mib`) and the size
of a newly provisioned boot disk (`boot_disk_gib`). CPU requests must be positive
and no greater than the vCPU count; they do not configure CPU quotas, shares or
runtime guarantees. Memory requests default to the limit and must equal it;
memory overselling and automatic ballooning are unsupported. Images own boot
requirements and device defaults (`guest_agent`, `graphics`, `video_model`).
Images default to guest agent enabled, VNC and no video device. Types own
`autostart`, which defaults to `false`. The create request's `start` flag requests
a one-time start and does not override autostart. A type too small for the image is rejected,
including when the image's actual virtual disk capacity exceeds the boot disk.

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

Unmanaged machines are visible and count toward admission but are read-only.
Interactive consoles and other mutating commands require adoption.
Explicit adoption records management identity without changing resources, power
state, disk sizes, guest initialization or placement. It does not claim existing
disks. Adoption is a durable asynchronous command, including for running machines.

Change instance type only while a managed VM is stopped. Admission replaces its
old requests with the proposed requests on its current host. If that host has
insufficient resources, the command fails even if another host has capacity.
There is no automatic stop, migration or restart. Existing disk sizes stay intact;
growth is a separate explicit operation. Current image defaults are not reapplied. The selected type's autostart policy
is applied while the current guest/device settings and graphics listener remain
unchanged. Adoption also preserves these settings. Configuration edits affect
future explicit operations; accepted tasks retain their policy and backend
binding snapshots.

Configure `graphics_listen` only inside the cloud's private `host` block.
Omission preserves libvirt's listener default. Explicit values must be IP
addresses (IPv4 or IPv6), not hostnames or host:port strings. The backend resolves
the selected host's binding before writing its XML. Console forwarding uses the
observed listener; wildcard addresses use loopback through SSH or the remote
connection host for direct connections. Remote loopback listeners require SSH.
Remote console access through Vmango requires HTTPS. These policy settings have
no per-machine write API; effective values remain read-only diagnostics.

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

Host `cpu_overcommit` is an integer multiplier (default `4`, minimum `1`). CPU request capacity is `(host CPUs - cpu_reserve) * cpu_overcommit`; this applies to creation, adoption and instance type changes. Memory is not overcommitted.
