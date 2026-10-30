---
title: "Cloud images and provisioning"
description: "Find cloud images, register them in Vmango and launch a virtual machine."
weight: 3
---

Vmango starts machines from pre-installed cloud images. There is no need to boot
an installer or mount an installation CD for each VM: cloud-init handles the
initial configuration on first boot.

Image entries are exposed to users through a configured cloud. Define the
[cloud catalog and host bindings](/docs/clouds/) before creating machines.

## Shared downloadable catalog

`vmango.dist.conf` ships twelve pinned QCOW2 definitions: Ubuntu 24.04 LTS,
Debian 13 Generic, Fedora Cloud Base 44, AlmaLinux GenericCloud 9.8,
Rocky Linux GenericCloud 9.8 and Alpine 3.24.3 cloud-init VM images, each for
x86_64/BIOS and aarch64/UEFI. The Alpine images use Limine; the selected boot
mode is explicit in each catalog ID and label. These settings describe the
selected variants, not universal BIOS/UEFI or cross-architecture compatibility.
The selector filters architecture and launch requirements using node capabilities.

Each top-level `image` entry is defined once and is available on compatible nodes.
Storage placement is configured separately:

```hcl
libvirt "local" {
    uri = "qemu:///system"
    image_pool = "default"
}
libvirt "remote" {
    uri = "qemu+ssh://libvirt@example.org/system"
    image_pool = "cloud-images"
}
```

`image_pool` names an existing **dir** libvirt storage pool on that node and defaults
to `default`. The pool must be active, writable through libvirt, and have enough
free space for the artifact. Vmango does not create, start or mount pools.
Other pool types, including `fs`, `netfs`, LVM and RBD, are not supported for
managed image storage. VM destination disks use a logical storage class mapped
to a pool on the selected host.
The same catalog entry can use different pool paths on different nodes. Adding
a node needs only its connection and pool binding, without copied image definitions.

A downloadable entry has `url`, mandatory `sha256` and `source_format` (`raw` or
`qcow2`), with no `path`. Use the complete definitions in `vmango.dist.conf` as
ready-to-use examples. SHA-256 covers the exact downloaded file bytes, not a
guest-visible filesystem or an unpacked/converted disk. Archives, external QCOW2
backing/data files, encryption and incompatible QCOW2 features are rejected.

### Lazy preparation and progress

Configuration parsing, startup and discovery make no upstream image requests.
A missing compatible image remains selectable. The first accepted `machine.create`
prepares it on the selected node and then automatically continues creating the VM.
Closing the form or browser does not cancel accepted work.

The data path is **upstream HTTP(S) → Vmango → libvirt volume upload**. Download
and upload overlap with bounded buffers and backpressure. Vmango creates no image
payload file, temporary download, spool or application-side cache. The staging
volume and reusable verified image belong to the destination libvirt node, even
when Vmango and libvirt run on the same computer. Concurrent creates share one
preparation for the same connection, pool and artifact. Followers remain queued
while that artifact is in use by an active create, leaving workers available for
unrelated images and nodes. Different nodes prepare independently. Different image IDs can share bytes while supplying different
launch defaults.

Task progress shows source inspection, destination preparation, concurrent
**Download and upload image**, final verification and normal VM provisioning.
Transfer progress includes forwarded bytes, percentage, average throughput and
estimated transfer time remaining. Speed includes destination backpressure; ETA
covers the transfer stage and excludes verification and VM creation. Estimates
are hidden when progress updates become stale.

The artifact byte size comes from `Content-Length`. If a source cannot provide it,
set `source_size_bytes` in the image definition. An error names this setting when
needed. This is the **exact file byte count**, separate from the QCOW2 virtual
capacity and `minimum_disk_gib`. If both HTTP and configuration provide sizes they
must agree. Truncated/oversized input and checksum mismatches fail the task.
QCOW2 virtual capacity and safety checks use a bounded header; source format and
capacity are confirmed from the resulting libvirt volume before cloning.

Optional application-wide limits:

```hcl
image_transfers {
    max_bytes = 17179869184 # 16 GiB of downloaded bytes per artifact
    timeout_seconds = 3600
    idle_timeout_seconds = 120
}
```

### Verification, failures and pinned updates

Managed names include the SHA-256 and format. Incomplete volumes are hidden from
ordinary volume selection and cannot be cloned through public volume commands.
Readiness is published only after consuming the complete response, checking
SHA-256, successfully finalizing the libvirt stream and confirming backend format,
capacity and identity. Durable verification metadata is stored in a private
`<tasks.directory>-images` directory; keep it alongside task history when backing
up the application. It contains metadata only. Reuse checks pool UUID, volume key,
modification/change timestamps, format and capacity. A matching path alone is
never adopted. External modification invalidates readiness. Removing a managed
source makes it downloadable again; the next explicit create verifies a fresh
transfer.

Ordinary transfer failures attempt to delete only staging created by that
operation, recording confirmed effects and cleanup errors. A verified source is
retained if later VM provisioning fails. Shutdown/interruption retains uncertain
staging for inspection. Stale running tasks become interrupted; they are never
automatically resumed or replayed. Before retrying, inspect the failed task's
exact resources and backend state, remove confirmed unused incomplete storage if
necessary, and explicitly release uncertain task locks through the task UI.
Never remove storage used by a VM or unrelated pre-existing volumes.

A new create after confirmed cleanup can retry explicitly; waiting requests see
the active preparation's failure. There are no background retries, upstream
polls, automatic image updates or automatic deletion of old revisions. Updating
an image requires an explicit configuration edit of URL/checksum together.
Accepted commands retain their image definitions and pool identities, and
existing VM disks stay unchanged. Old source revisions remain protected and
hidden until an administrator deliberately handles them.

### Publisher provenance and maintainer refresh

The source verification record, `docs/data/image-sources.json`,
contains each official direct URL, publisher checksum URL/algorithm/digest, the
calculated literal SHA-256, observed HTTP byte count/type and inspected QCOW2
virtual capacity. Where publishers supply only SHA-512 (Debian and Alpine), the
artifact was streamed through SHA-512 and SHA-256 together, checked against the
publisher SHA-512, and the calculated SHA-256 was pinned in configuration.
No image payload was saved during that verification.

To refresh intentionally:

1. Select a maintained release/build and architecture from the official publisher.
   Prefer a dated/build-specific artifact over a `latest` alias. Confirm its boot
   variant, cloud-init support and minimum resources from publisher documentation.
2. Update that entry's official artifact/checksum URLs and expected publisher digest
   in `docs/data/image-sources.json`. Remove its old `sha256` for an intentional
   revision change. Obtain manifests over HTTPS and verify publisher signatures
   where available using trusted distribution keys.
3. Run `python3 scripts/verify-image-sources.py --record`. This explicit maintainer
   command streams each artifact without saving payloads, checks publisher digests,
   inspects the header/HTTP metadata and records SHA-256. Without `--record`, it
   verifies the pinned records without rewriting them.
4. Copy the verified URL/SHA-256 pair into `vmango.dist.conf`, review firmware,
   initialization and resource defaults, and update documentation together.
5. Run configuration/domain/API/UI checks. Run a real Alpine download-to-boot test
   as described in `acceptance/README.md`; record release, architecture and backend.

Source/checksum validation establishes artifact identity and format. It does not
establish that a guest boots or provisions correctly. Real boot checks and their
backend-specific results are recorded separately in the acceptance documentation.

## Register the image

Check the publisher's checksum, unpack the image if needed, and place it in a
libvirt storage pool. Register a catalog entry in
[vmango.conf](https://github.com/subuk/vmango/blob/master/vmango.dist.conf):

```hcl
image "ubuntu-server" {
    path = "/var/lib/libvirt/images/ubuntu-cloud.img"
    name = "Ubuntu server"
    os_name = "Ubuntu"
    os_version = "24.04"
    os_arch = "x86_64"
    firmware = "efi"
    disk_bus = "virtio"
    nic_model = "virtio"
    guest_agent = true
    initialization = "nocloud"
    minimum_memory_mib = 1024
    minimum_disk_gib = 8
    protected = true
}
```

The block label is a stable image **identifier**, separate from `path`. `name`
is the display name (it defaults to the identifier). `os_arch` must be
`x86_64` or `aarch64`. For path-based entries, the selector shows images matching the selected
architecture whose source volume is visible, available and not attached to a VM.

The catalog is shared across all nodes. Vmango resolves `path` on the selected
node and reads that volume's actual capacity and format. A volume on another
node cannot satisfy a missing source. The same path on several nodes intentionally
receives the same settings; administrators must ensure its contents match the
image description. Path-based entries require an administrator-provisioned source on each selected node. Downloadable entries use the lazy managed flow described above.

Several entries can reference the same disk with different defaults:

```hcl
image "ubuntu-unmanaged" {
    path = "/var/lib/libvirt/images/ubuntu-cloud.img"
    name = "Ubuntu without provisioning"
    os_name = "Ubuntu"
    os_arch = "x86_64"
    guest_agent = false
    initialization = "none"
}
```

Selecting this identifier disables the guest agent channel and omits the config
drive. It does not change the `ubuntu-server` entry or the shared source disk.

### Creation defaults and requirements

| Setting | Values | When omitted |
| --- | --- | --- |
| `firmware` | `auto`, `bios`, `efi` | Existing backend firmware selection |
| `disk_bus` | `virtio`, `scsi`, `ide` | `virtio` |
| `disk_controller` | `virtio-scsi` (requires `disk_bus = "scsi"`) | Backend-selected controller |
| `nic_model` | `virtio`, `e1000`, `rtl8139`, subject to backend support | `virtio` |
| `guest_agent` | `true`, `false` | `true` |
| `initialization` | `none`, `nocloud`, `openstack` | Selected node's `config_drive_write_format` |
| `minimum_memory_mib` | Nonnegative integer, MiB | No additional minimum |
| `minimum_disk_gib` | Nonnegative integer, GiB | Source volume capacity |

The backend checks capabilities for the selected architecture and node. A listed
option is not necessarily supported by every backend. For example, built-in
QEMU user networking and socket-vmnet support only virtio NICs.

The simplified cloud form resolves launch settings from the selected image,
instance type, network and storage class. It does not expose device, firmware,
guest-agent or initialization overrides. Administrators configure these settings
in HCL; explicit `false` and initialization `none` remain distinct from omitted
values. Conflicting requirements are rejected rather than silently overridden.

Instance types set CPU, RAM and the size of a newly provisioned boot disk. Image
minimum memory, minimum disk size and actual source virtual capacity are checked
before cloning. An instance type with an undersized boot disk is rejected; no
clone shrinks the source. Additional data disks have independent sizes. Changing
a stopped VM's instance type stays on its current host and preserves all existing
disk sizes; insufficient host capacity produces an error.

These settings are launch defaults, not ongoing restrictions. Editing the
catalog affects future VMs only. Existing VMs can still have devices added or
changed and their guest agent enabled or disabled through supported VM operations.
Restart Vmango after changing the configuration to reload the read-only catalog.

### Legacy configuration and volume policies

Existing blocks remain supported:

```hcl
image "/var/lib/libvirt/images/ubuntu-cloud.img" {
    os_name = "Ubuntu"
    os_arch = "x86_64"
    efi = true
    protected = true
}
```

Without `path`, the label is both the legacy identifier and source path.
`efi = true` maps to `firmware = "efi"`; `efi = false` preserves the old automatic
behavior (`firmware = "auto"`). Conflicting `efi` and `firmware` settings are
rejected. Unspecified options keep the existing defaults, including the node's
config drive format. To migrate, choose a stable label and put the old label in
`path`; IDs must be unique, but source paths need not be.

Protection and visibility belong to volumes, including ordinary non-image
volumes. They can be configured independently:

```hcl
volume "/dev/data/private" {
    protected = true
    hidden = true
}
```

`protected` prevents deletion through Vmango; `hidden` excludes the volume from
lookup and listing. Existing `image "path"` blocks without `os_name` continue to
act as volume-policy entries and do not become catalog images. The legacy
`protected` and `hidden` fields on image entries remain supported. Policies apply
to the source path on every node; when several entries reference one path,
protection and hiding are combined (any `true` applies to that volume).

## Launch a machine

1. **Choose a configuration.** In the selected cloud, choose an image, instance
   type, availability zone, logical network and storage class. Valid single
   choices are selected automatically.
2. **Make it yours.** Select SSH keys and optionally provide cloud-init user data.
   Optional placement labels express required constraints or soft preferences.
3. **Create and follow progress.** The durable task inventories the cloud and
   schedules a compatible host, prepares a missing downloadable image, clones
   the boot disk and supplies guest initialization. The assigned host is visible
   read-only in machine details. The OS configures itself on first boot.

Once your hosts and images are set up, launching another machine is a single
form and a click. Your infrastructure stays on your own hardware.
