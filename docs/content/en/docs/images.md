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

## Cloud-owned downloadable catalog

`vmango.dist.conf` ships twelve pinned QCOW2 definitions: Ubuntu 24.04 LTS,
Debian 13 Generic, Fedora Cloud Base 44, AlmaLinux GenericCloud 9.8,
Rocky Linux GenericCloud 9.8 and Alpine 3.24.3 cloud-init VM images, each for
x86_64/BIOS and aarch64/UEFI. The Alpine images use Limine; the selected boot
mode is explicit in each catalog ID and label. These settings describe the
selected variants, not universal BIOS/UEFI or cross-architecture compatibility.
The selector filters architecture and launch requirements using node capabilities.

Each `image` entry belongs inside its owning `cloud` and is available on compatible
nodes of that cloud. Equal image IDs in different clouds are independent.
Set `image_pool` directly in each existing cloud host. This excerpt shows the
image storage settings; keep the cloud's catalog and each host's zone,
network and storage-class bindings:

```hcl
cloud "local" {
    # Keep the cloud catalog here.
    host "local" {
        uri = "qemu:///system"
        image_pool = "default"
        # Keep this host's zone and network/storage bindings.
    }
    host "remote" {
        uri = "qemu+ssh://libvirt@example.org/system"
        image_pool = "cloud-images"
        # Keep this host's zone and network/storage bindings.
    }
}
```

`image_pool` names an existing **dir** libvirt storage pool on that node and defaults
to `default`. The pool must be active, writable through libvirt, and have enough
free space for the artifact. Vmango does not create, start or mount pools.
Other pool types, including `fs`, `netfs`, LVM and RBD, are not supported for
managed image storage. VM destination disks use a logical storage class mapped
to a pool on the selected host.
The same catalog entry can use different pool paths on different nodes. Adding
a node needs only a cloud host block with its connection and pool binding,
without copying definitions within that cloud.

A downloadable entry has `url`, mandatory `sha256` and `source_format` (`raw` or
`qcow2`), with no `path`. Use the complete definitions in `vmango.dist.conf` as
ready-to-use examples. SHA-256 covers the exact downloaded file bytes, not a
guest-visible filesystem or an unpacked disk. Add the archive settings below
when the download contains a disk inside ZIP or TAR. External QCOW2 backing/data
files, encryption and incompatible QCOW2 features are rejected.

### Select a disk inside an archive

Keep `url`, `sha256` and `source_format`, and add `archive_format` and
`archive_member` to select exactly one regular file:

```hcl
hardware_profile "linux-uefi" { firmware { type = "efi" } }
image "archived-linux" {
    hardware_profile = "linux-uefi"
    os_arch = "aarch64"
    url = "https://images.example.org/linux.tar.gz"
    sha256 = "REPLACE_WITH_ARCHIVE_SHA256"
    source_format = "qcow2"
    archive_format = "tar.gz"
    archive_member = "images/system.qcow2"
    extracted_size_bytes = 2147483648
}
```

Replace the URL, checksum, member name and size with the published artifact's
values. `archive_format` accepts `zip`, `tar` and `tar.gz`. `source_format` remains
`raw` or `qcow2` and describes the selected disk, not its container. Both archive
settings must be present together; omitting both retains direct-file downloads.
Local `path` entries do not use archive settings.

`archive_member` is the exact, case-sensitive path stored in the archive, using
forward slashes. Use a relative path without a drive prefix, leading slash,
backslashes, empty components, `.` or `..`. Vmango does not extract a directory
or follow symbolic/hard links: the selected member must be a single regular
file. Missing or duplicate matching members fail preparation. ZIP supports
stored and Deflate members, including ZIP64 and data descriptors; encrypted
members and unsupported compression methods are rejected. TAR supports plain
or gzip-compressed archives.

The sizes describe different things:

| Setting | Meaning |
| --- | --- |
| `sha256` | SHA-256 of the complete downloaded archive |
| `source_size_bytes` | Optional exact archive file byte count; required if HTTP omits `Content-Length` |
| `extracted_size_bytes` | Optional exact selected file byte count after decompression |
| `minimum_disk_gib` | Minimum guest-visible boot disk capacity |

Set `extracted_size_bytes` when known so placement can budget the actual image
storage. Without it, placement conservatively reserves `image_transfers.max_bytes`
for an unprepared archived image. A configured extracted size must agree with
the archive member; it is not the QCOW2 virtual capacity.

ZIP preparation makes two complete HTTP requests. The first verifies the archive
SHA-256 and reads its bounded central-directory metadata; the second streams the
selected disk to libvirt and verifies the full archive again before completing
the upload. ZIP metadata must fit in a 4 MiB tail window with at most 10,000 entries.
Split archives and nonstandard footer gaps are rejected.
This handles members whose local headers omit their sizes without keeping the
archive on disk. TAR and TAR.GZ use one HTTP pass; after the selected member,
Vmango scans the remainder before completing the upload to detect duplicates,
validate trailers and verify the complete download checksum. Nonzero data after
the TAR end marker is rejected; concatenated TAR archives are not supported.

`image_transfers.max_bytes` bounds the downloaded archive and the extracted disk
file separately. For TAR and TAR.GZ it also bounds the complete TAR stream after
decompression, including headers and skipped files. An archive may therefore exceed the limit
even when the selected disk fits. Checksum, ZIP CRC, gzip trailer, size or format
failures leave the image unverified and use the staging cleanup described below.
Archive payloads and extracted disks are never written to temporary files on the
Vmango application host. Only the selected disk is uploaded to the target pool.

### Lazy preparation and progress

Configuration parsing, startup and discovery make no upstream image requests.
A missing compatible image remains selectable. The first accepted `machine.create`
prepares it on the selected node and then automatically continues creating the VM.
Closing the form or browser does not cancel accepted work.

The data path is **upstream HTTP(S) → Vmango → libvirt volume upload**. Download
and upload overlap with bounded buffers and backpressure for direct files and
TAR; ZIP first makes the verification pass described above. Vmango creates no image
payload file, temporary download, spool or application-side cache. The staging
volume and reusable verified image belong to the destination libvirt node, even
when Vmango and libvirt run on the same computer. Concurrent creates share one
preparation for the same connection, pool and artifact. Followers remain queued
while that artifact is in use by an active create, leaving workers available for
unrelated images and nodes. Different nodes prepare independently. Different image IDs can share bytes while supplying different
launch defaults. Archive format and selected member are part of the prepared
image identity, so different disks in one archive cannot reuse each other.

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
    max_bytes = 17179869184 # 16 GiB per download, extracted disk or decompressed TAR scan
    timeout_seconds = 3600
    idle_timeout_seconds = 120
}
```

### Verification, failures and pinned updates

Managed names identify the SHA-256 and format; archived sources also include
the archive format and member selection in their identity. Incomplete volumes are hidden from
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
hardware_profile "linux-uefi" {
    firmware { type = "efi" }
    disks { bus = "virtio" }
    nic_model = "virtio"
    guest_agent = true
    config_drive { format = "nocloud" }
}
image "ubuntu-server" {
    path = "/var/lib/libvirt/images/ubuntu-cloud.img"
    name = "Ubuntu server"
    os_name = "Ubuntu"
    os_version = "24.04"
    os_arch = "x86_64"
    hardware_profile = "linux-uefi"
    minimum_memory_mib = 1024
    minimum_disk_gib = 8
    protected = true
}
```

The block label is a stable image **identifier**, separate from `path`. `name`
is the display name (it defaults to the identifier). `os_arch` must be
`x86_64` or `aarch64`. For path-based entries, the selector shows images matching the selected
architecture whose source volume is visible, available and not attached to a VM.

The catalog is shared across all hosts in its owning cloud. Vmango resolves `path` on the selected
node and reads that volume's actual capacity and format. A volume on another
node cannot satisfy a missing source. The same path on several nodes intentionally
receives the same settings; administrators must ensure its contents match the
image description. Path-based entries require an administrator-provisioned source on each selected node. Downloadable entries use the lazy managed flow described above.

Several entries can reference the same disk with different defaults:

```hcl
hardware_profile "linux-no-provisioning" {
    guest_agent = false
    config_drive { enabled = false }
}
image "ubuntu-unmanaged" {
    path = "/var/lib/libvirt/images/ubuntu-cloud.img"
    name = "Ubuntu without provisioning"
    os_name = "Ubuntu"
    os_arch = "x86_64"
    hardware_profile = "linux-no-provisioning"
}
```

Selecting this identifier disables the guest agent channel and omits the config
drive. It does not change the `ubuntu-server` entry or the shared source disk.

### Hardware profiles and image requirements

Define profiles and images inside their owning `cloud` block. Every image must
reference a named profile using `hardware_profile`; missing or unknown references
fail configuration loading. Multiple images may reference one profile. To change
hardware for one image, define and select another complete profile; there are no
inline overrides or profile inheritance.

The following settings belong to `hardware_profile`, not `image`:

| Setting | Values | When omitted |
| --- | --- | --- |
| `firmware {}` | `type = "auto"`, `"bios"`, `"efi"`, or `profile = "id"` | Platform default selection |
| `disks.bus` | `virtio`, `scsi`, `ide`, `sata` | `virtio` |
| `disks.controller_model` | `virtio-scsi`, `lsilogic`, `lsisas1068`, `vmpvscsi`; SCSI only | Required for SCSI; forbidden otherwise |
| `nic_model` | `virtio`, `e1000`, `rtl8139`, subject to backend support | `virtio` |
| `guest_agent` | `true`, `false` | `true` |
| `graphics.type` | `none`, `vnc`, `spice`, subject to backend support | `vnc` |
| `graphics.video_model` | `none`, `cirrus`, `qxl`, `virtio` (2D), `vmvga` | `virtio` with graphics, otherwise `none` |
| `input.keyboard` / `input.pointer` | `usb` or `none` / `usb-tablet`, `usb-mouse`, `none` | USB keyboard/tablet with graphics |
| `usb_controller.model` | `qemu-xhci` | One shared input dependency, otherwise absent |
| `hugepages` | `true`, `false` | `false` |
| `config_drive.enabled` | `true`, `false` | `true` |
| `config_drive.format` | `nocloud`, `openstack` | `nocloud` |
| `config_drive.device_type` | `disk`, `cdrom`, where supported | `disk` |
| `config_drive.bus` | `virtio`, `scsi`, `ide`, `sata`, compatible with device type | `virtio` |
| `config_drive.controller_model` | Same explicit SCSI models as ordinary disks | Required for SCSI; forbidden otherwise |

`graphics { type="none" }` disables video and input defaults. See [graphics and input](../graphics-input/) for complete server examples, guest prerequisites and migration. New graphics-enabled VMs listen only
on host loopback, and remote console traffic uses SSH. The listener address is
not configurable. Existing VM listeners are preserved. Hugepages must already
be available on the host; Vmango does not configure host page pools.

The ordinary `disks` policy covers root disks, required and user data disks,
existing volumes and later attachments. Virtio block uses individual devices;
virtio SCSI shares compatible controllers. Selecting virtio asserts that the
image already has the guest drivers. Vmango checks host/backend support and
attachment capacity without installing drivers or falling back to another bus.
Config-drive format and attachment are independent of the ordinary disk policy;
the generated drive is VM-owned and excluded from system-disk boot priority.

Images retain `minimum_memory_mib`, `minimum_disk_gib`, artifact sources and
required data disks. Instance types set CPU, RAM and new boot-disk capacity;
actual source capacity and image minimums are checked before cloning. Existing
disk sizes remain unchanged when a stopped VM changes instance type.

The user form still selects image, instance type, network and storage class.
It exposes no firmware, guest-agent or config-drive overrides. Accepted tasks
retain their backend plans, and existing VMs retain applied policy independently
of task retention. Later attachments use that policy even after a profile edit.
Type changes preserve hardware policy, hugepages, explicit pinning and autostart.

Restart Vmango to reload the configuration catalog. Existing VMs missing applied
policy require explicit adoption/re-adoption with a compatible named profile;
there is no policy inference or hardware conversion. See the
[release migration guide](https://github.com/subuk/vmango/blob/master/docs/HARDWARE-PROFILES-MIGRATION.md)
for removed legacy fields and the incompatible task-format boundary.

### Required image data disks

An image can declare blank disks that every new VM needs. Add one or more
`data_disk` blocks inside the `image` block:

```hcl
data_disk "userdata" {
    size_gib = 16
}
```

Each label is a unique disk name within the image; `root` is reserved for the
boot disk. `size_gib` is a positive integer. Vmango creates these disks
automatically using the VM's selected storage class and disk format, attached
through VirtIO. The boot disk comes first, followed by image data disks in their
declaration order, then any additional disks chosen by the user.

The creation form displays required image disks without edit or remove controls.
API callers also receive them automatically and should not repeat them in
`new_disks`; a user disk with the same name is rejected. Storage capacity checks
include the required disks. They start blank, so the guest must partition and
format them as needed.

Image disk configuration applies to new machines. It does not modify existing
VMs, and accepted creation tasks retain the disk definitions from acceptance.
Images without `data_disk` blocks keep their existing behavior.

### Configuration migration and volume policies

Top-level image blocks and `cloud.images` reference lists are removed. Move each
image into its owning `cloud` block. Copy formerly shared definitions explicitly
into every cloud that uses them. For a path-labeled image, choose a stable catalog
ID and put the former label in `path`:

```hcl
cloud "local" {
    hardware_profile "linux-uefi" { firmware { type = "efi" } }
    image "ubuntu-local" {
        path = "/var/lib/libvirt/images/ubuntu-cloud.img"
        os_name = "Ubuntu"
        os_arch = "x86_64"
        hardware_profile = "linux-uefi"
        protected = true
    }
    # Keep this cloud's zones, types, network/storage catalogs and host bindings.
}
```

IDs match `^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$` and are unique within their cloud;
source paths need not be unique. Move legacy image hardware fields into a named
profile, and explicitly reference it. `efi`, inline hardware and `initialization`
are rejected. Config-drive omission defaults to NoCloud on a virtio disk,
independent of deprecated host fields. Restart to activate file edits.

Legacy image blocks used only for volume policy become top-level volume blocks:

```hcl
volume "/dev/data/private" {
    protected = true
    hidden = true
}
```

`protected` prevents deletion and `hidden` excludes an ordinary volume from
listing. Neither flag hides an image definition from Administration. Explicit
volume policies retain their path semantics. Policies derived from a cloud image
apply only to that cloud's configured hosts; equal source paths in unrelated
clouds do not share protection, hiding or attachment exclusions. Multiple images
using one source on the same backend combine source policy (any `true` applies).
Managed sources retain their existing physical protection independently of these
configured flags.

Preserve task, artifact and volume-ownership metadata during migration. Queued
tasks keep their accepted source/default snapshots and backend identities, even
if the running catalog now differs. Unsafe legacy commands fail before mutation
and require explicit resubmission; history and locks remain available for review.

## Android ARM64 with LineageOS

The community [LineageOS QEMU builds](https://github.com/jqssun/android-lineage-qemu)
provide a complete ARM64 UEFI system disk. The `v2026.10.20` prerelease targets
upstream QEMU and works with macOS HVF; it does not require Android Emulator or
Cuttlefish host services. The archive contains a UTM bundle, but Vmango uses its
`vda.qcow2` directly.

Add this definition inside the cloud that owns the ARM64 host. Vmango downloads
the [pinned release archive](https://github.com/jqssun/android-lineage-qemu/releases/download/v2026.10.20/UTM-VM-lineage-24.0-20261020-jqssun-virtio_arm64only.zip)
and selects `LineageOS_on_arm64.utm/Data/vda.qcow2` automatically; no manual
extraction, source-volume registration or local `path` is needed. Configure the
host's `image_pool` as described above.

```hcl
hardware_profile "android-arm64" {
    firmware { type = "efi" }
    disks { bus = "virtio" }
    nic_model = "virtio"
    graphics { type = "vnc" video_model = "virtio" }
    guest_agent = false
    config_drive { enabled = false }
}
image "android-lineage-24-arm64" {
    name = "Android / LineageOS 24 (ARM64)"
    os_name = "Android (LineageOS)"
    os_version = "24.0 / 20261020"
    os_arch = "aarch64"
    url = "https://github.com/jqssun/android-lineage-qemu/releases/download/v2026.10.20/UTM-VM-lineage-24.0-20261020-jqssun-virtio_arm64only.zip"
    sha256 = "f2754f6be04b5e47e35851586d350411cfbf516f67dd53ec4a80121ceaa4428e"
    source_format = "qcow2"
    source_size_bytes = 1119381498
    archive_format = "zip"
    archive_member = "LineageOS_on_arm64.utm/Data/vda.qcow2"
    extracted_size_bytes = 2222784512
    hardware_profile = "android-arm64"
    protected = true
    minimum_memory_mib = 2048
    minimum_disk_gib = 5
    data_disk "userdata" {
        size_gib = 16
    }
}
```

Restart Vmango to load the catalog. Select an ARM64 host and a type with at least
2 vCPUs, 2 GiB RAM and a 5 GiB boot disk. Selecting this Android image automatically
adds the required blank 16 GiB `userdata` disk. Android uses the cloned system
disk as `vda` and formats the second disk, `vdb`, for `/data` on first boot.
The archive's `vdb.qcow2` and `efi_vars.fd` are not extracted: Vmango creates the
blank disk and libvirt creates fresh UEFI variables.

Open the machine's console, select **VNC**, and connect to complete the LineageOS
setup wizard. New ARM64 graphical VMs include USB keyboard and tablet input;
`graphics.video_model = "virtio"` uses 2D graphics without host OpenGL acceleration.
SSH keys and cloud-init user data do not configure this image.

Verified on 2026-11-03 through Vmango's creation API on an Apple M1 Pro with
QEMU 9.2.3, libvirt 11.2.0 and HVF: Vmango downloaded the release ZIP, verified
both passes, selected the system disk and started a 2-vCPU/2-GiB VM with an 8-GiB
boot disk and automatic 16-GiB `userdata`. The guest reached the LineageOS setup
wizard. Display and pointer input were also checked through Vmango's browser
VNC console in the earlier local-source test. These checks do not establish
Linux/KVM compatibility, accelerated graphics, audio or application compatibility.

## Launch a machine

Choose a [namespace](/docs/namespaces/) before opening **Create machine**, then
select one of its allowed clouds. Keys and saved templates come from that
namespace; the new machine and its disks will belong to it.

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

## Firmware and private UEFI variables

Use `firmware { type = "auto" }`, `"bios"`, or `"efi"` in the hardware profile.
For EFI, `secure_boot = true` requests firmware support and initially enrolled
keys; `enrolled_keys = false` requests an empty key template instead. Explicit
EFI defaults to firmware without Secure Boot. Do not set security fields on
`auto` or BIOS. Named cloud `firmware_profile` policies bind to versioned
CODE/VARS paths under each host and are selected with `firmware { profile = "id" }`.

Libvirt creates a private persistent file for each VM in its standard system or
session NVRAM directory. Ordinary changes preserve it. A missing file is
regenerated from the saved template, losing guest variable changes; keep old
templates available. Deletion removes only positively owned files, never shared
CODE/templates or adopted external NVRAM. Saved paths do not pin file contents.

See the repository's [complete firmware guide](https://github.com/subuk/vmango/blob/master/docs/FIRMWARE.md)
for two-host examples, format/version limits, administrator artifact contracts,
Secure Boot validation and excluded reset/migration operations.

## Disk buses and controllers

A profile uses `disks { bus = "virtio" }` by default. Choose `scsi` with an
explicit `controller_model` (`virtio-scsi`, `lsilogic`, `lsisas1068`, `vmpvscsi`),
or a platform-compatible `ide`/`sata` bus. Config drive has an independent bus
and, for SCSI, an explicit controller model. A SATA config CD-ROM shares the
six AHCI ports with the root and data disks. Capacity is checked before creating
volumes, and Vmango never adds another controller to extend a full controller.

Disk attachment changes require a stopped VM and use its persisted applied
profile. Catalog edits do not convert its hardware. See the detailed
[configuration/XML mapping, capacity limits, migration and examples](https://github.com/subuk/vmango/blob/master/docs/DISKS.md)
and [libvirt controller documentation](https://libvirt.org/formatdomain.html#controllers).
