# Configured libvirt clouds

A cloud exposes a catalog of images, instance types, availability zones, logical
networks and storage classes. Administrators configure the existing libvirt hosts
and bindings. Users select catalog entries; the scheduler chooses a host. Vmango
currently implements only libvirt. Placement intent and capability discovery are
independent of libvirt so future provider adapters can implement supported features.

See the complete `cloud` example in `vmango.dist.conf`. Each `host` refers to a
`libvirt` connection by its stable ID and belongs to exactly one cloud and zone.
Do not configure aliases for the same connection. Multiple hosts can share a zone.
Catalog IDs are separate from display names. Images remain a shared catalog with
host-specific image storage; cloud membership is an explicit list of image IDs.

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
all hosts in the cloud, including inactive and unmanaged domains. Read failures
fail admission. Existing domains without scheduler metadata consume requests
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

Configure `graphics_listen` only inside the private `libvirt` connection block.
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
  "id": "demo",
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
acceptance is not completion. Existing-disk references contain the observed
`host` and opaque `volume_id` returned by discovery. Raw paths are not create inputs.

Queued commands accepted before the cloud migration fail before backend mutation
if they lack cloud context; inspect their history and submit a new cloud command.

All former `/api/v1/nodes/...` resource routes and global `/vms`, `/volumes` and
`/networks` collections are removed without compatibility aliases. Read-only
administrative diagnostics use `/api/v1/hosts/...`. CPU/RAM
patches are replaced by `POST /api/v1/clouds/{cloud}/machines/{node}/{vm}/instance-type`
with `{"instance_type":"medium"}`. The node in an existing-resource URL identifies
observed placement; it cannot be used to move a machine. Adoption uses the same
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
available under `/hosts` for administrative diagnostics. See authenticated OpenAPI for the complete schema.
