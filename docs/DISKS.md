# Disk buses, controllers and config drives

Each libvirt image selects one complete named `hardware_profile`. Its `disks`
block controls every ordinary disk: root, image-required disks, newly created
data disks, and existing volumes attached during creation or while stopped.
Instance types change resource quantities, not disk hardware.

| Configuration | Libvirt XML | QEMU device family |
| --- | --- | --- |
| `disks { bus="virtio" }` (default) | Disk target `bus="virtio"`, no shared storage controller | `virtio-blk` |
| `bus="scsi" controller_model="virtio-scsi"` | SCSI controller `model="virtio-scsi"`, disk target `bus="scsi"` | `virtio-scsi` with SCSI disk/CD-ROM devices |
| `bus="scsi" controller_model="lsilogic"` | SCSI controller `model="lsilogic"` | LSI53C895A parallel SCSI |
| `bus="scsi" controller_model="lsisas1068"` | SCSI controller `model="lsisas1068"` | LSI SAS1068 |
| `bus="scsi" controller_model="vmpvscsi"` | SCSI controller `model="vmpvscsi"` | VMware PVSCSI |
| `disks { bus="ide" }` | Platform IDE controller, disk target `bus="ide"` | Platform IDE, such as PIIX on i440fx |
| `disks { bus="sata" }` | SATA controller (no invented `model="ahci"`), disk target `bus="sata"` | ICH9 AHCI on compatible QEMU platforms |

The bus names are libvirt target bus names. `virtio-scsi` is a **controller
model**, not a bus alias. SCSI requires an explicit model; `auto` is not accepted.
`controller_model` is forbidden for virtio, IDE and SATA. Omitted `disks`, or an
omitted `bus`, selects virtio-blk for newly accepted work.

The backend checks the selected architecture, canonical machine, emulator and
acceleration using libvirt's read-only native conversion before provisioning.
For example, Q35 has AHCI but no conventional IDE controller; use an appropriate
i440fx machine for IDE. Support is checked on the actual backend, rather than
inferred from the host operating system. Administrators must still choose an
image with the correct guest drivers. Capability validation neither installs
drivers nor establishes that an arbitrary guest will boot.

## Independent config-drive attachment

`config_drive` does not inherit `disks`. Omission defaults to enabled, `nocloud`,
device type `disk`, bus `virtio`. Both `nocloud` and `openstack` formats remain
available. Disk and CD-ROM attachments are supported on compatible IDE, SATA and
SCSI controllers; virtio-blk supports disks only. Explicit SCSI model selection
also applies here.

```hcl
hardware_profile "sata-with-config-cdrom" {
    machine { type = "q35" }
    disks { bus = "sata" }
    config_drive {
        enabled = true
        format = "nocloud"
        device_type = "cdrom"
        bus = "sata"
    }
}
```

This profile needs one AHCI controller shared by root/data and config drive.
Its six ports leave five ordinary-disk positions, **including root**. A disabled
config drive creates neither its artifact nor its controller dependency. The
generated config drive is VM-owned and excluded from firmware boot priority.
User-data, metadata and SSH keys remain provisioning inputs.

With ordinary `scsi/lsilogic` and config-drive `scsi/virtio-scsi`, resolution
creates one controller of each model. With identical requirements, it creates
only one. No second controller is ever added to extend capacity.

## Address space and capacity

| Controller | Supported allocation layout | Maximum addressable attachments in this implementation |
| --- | --- | --- |
| IDE | Controller 0, buses 0–1, target 0, units 0–1 | 4 |
| ICH9 AHCI/SATA | Bus/target 0, ports represented by units 0–5 | 6 |
| lsilogic | Bus/target 0, units 0–6; ID 7 is reserved | 7 |
| lsisas1068 | Bus 0, targets 0–7, LUN 0 | 8 |
| vmpvscsi | Bus 0, targets 0–63, LUN 0 | 64 |
| virtio-scsi | Bus 0, targets 0–255, LUNs 0–16383 | 4,194,304 address combinations |

These are address-space bounds, **not tested operational disk-count or
performance guarantees**. Vmango deliberately supports one LUN per target for
the emulated SAS/PVSCSI models. The actual emulator, guest, memory, PCI resources
and libvirt may impose lower limits; native conversion is also required. A
virtio-scsi LUN is not a PCI device; virtio-blk uses one transport device per disk.

For example, attempting a sixth ordinary SATA disk with a SATA config drive
fails with a controller/capacity/usage error before a volume is created. Initial
creation validates the entire set, including config drive. Later attachments
use available supported addresses on already existing compatible controllers.
Adoption preserves multiple existing controllers. Detach frees an address;
reattachment can reuse it without moving other disks. Empty controllers remain.
Disk target names remain unique across SATA and SCSI's shared `sd` namespace.

Virtio-blk creation validates the complete initial topology through libvirt.
Later attachment requires a free slot on an existing compatible PCI/PCIe bus;
Vmango does not add bridges/root ports or change existing multifunction layout.
An unknown or exhausted transport is rejected, including raw QEMU PCI devices
whose placement libvirt does not track. This does not provide arbitrary
virtio-MMIO or physical SCSI passthrough management.

## Stopped operations and adoption

Create/attach/detach/reattach operate on stopped VMs. Requests that need an active
VM's topology changed fail before provisioning or domain mutations. Startup
may follow successful creation through the normal lifecycle. Root protection,
boot order, namespace checks and volume ownership remain in effect.

Later operations use the independently persisted applied hardware profile,
including its explicit SCSI model, rather than the current catalog or first
disk. Conflicting attachment bus/type overrides are rejected. Changing a
catalog profile or instance type never converts existing disks.

Observation reports actual bus, controller model and addresses, including
unrecognized models, without adoption. Adoption/re-adoption requires an explicit
compatible profile and checks ordinary disks separately from config drive.
Mixed incompatible buses/models are rejected. Adoption does not rewrite XML,
renumber devices or claim external disk ownership.

## Complete configuration examples

[storage.conf](examples/storage.conf) contains a cloud/host, image references,
serial-only and VNC server profiles for virtio-blk, virtio-scsi, emulated SCSI,
IDE and SATA, plus shared and different config-drive controllers. Configuration
tests parse the file. Replace its source image path, host URI and storage/network
bindings with your own; guest/platform testing is recorded separately in the
[acceptance guide](../acceptance/README.md).

## Migrating flat profile settings

Before:

```hcl
hardware_profile "server" {
    disk_bus = "scsi"
    disk_controller = "virtio-scsi"
    config_drive { bus = "scsi" }
}
```

After:

```hcl
hardware_profile "server" {
    disks { bus = "scsi" controller_model = "virtio-scsi" }
    config_drive { bus = "scsi" controller_model = "virtio-scsi" }
}
```

Unmigrated profile fields fail with their cloud/profile location and a migration
link; they are never silently discarded. Deprecated image-inline fields are
also rejected, including empty, zero and false values. Supported stored policies and
plans retain their original interpretation, independently of configuration
syntax. Accepted task version checks, history, effects and uncertainty fences
remain in place; no uncertain side effect is replayed.

## Official references

- [Libvirt disks and target buses](https://libvirt.org/formatdomain.html#hard-drives-floppy-disks-cdroms)
- [Libvirt controllers](https://libvirt.org/formatdomain.html#controllers)
- [Libvirt addresses](https://libvirt.org/formatdomain.html#device-addresses)
- [QEMU virtio-blk and virtio-scsi](https://www.qemu.org/2021/01/19/virtio-blk-scsi-configuration/)
- QEMU model limits: [virtio-scsi](https://github.com/qemu/qemu/blob/master/include/hw/virtio/virtio-scsi.h), [LSI](https://github.com/qemu/qemu/blob/master/hw/scsi/lsi53c895a.c), [SAS1068](https://github.com/qemu/qemu/blob/master/hw/scsi/mptsas.c), [PVSCSI](https://github.com/qemu/qemu/blob/master/hw/scsi/vmw_pvscsi.c)
- [Hardware-profile migration](HARDWARE-PROFILES-MIGRATION.md)
