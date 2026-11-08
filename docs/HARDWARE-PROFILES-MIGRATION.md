# Hardware profiles: release migration and breaking changes

This guide covers the release introducing explicit libvirt hardware profiles.
Migrate configuration before starting the upgraded service. The upgrade does not
rewrite existing VM hardware, convert disks, or replay unfinished old commands.

## Upgrade checklist

1. Back up configuration, the task directory and its related image/volume stores.
   Preserve existing domain metadata and inspect unfinished work before upgrading.
2. Define named hardware profiles inside each libvirt cloud. Reference one profile
   explicitly from every image in that cloud, including aliases and hidden images.
3. Move image hardware settings, instance-type hugepages and config-drive policy
   into profiles. Remove deprecated fields after reviewing their warnings.
4. Start the service and review configuration diagnostics and task history.
   Unfinished commands accepted before the incompatible upgrade fail without
   execution. Inspect any recorded effects and uncertainty locks before resubmitting.
5. Explicitly adopt or re-adopt each existing VM with a compatible named profile
   before attempting mutations. Adoption validates and records policy; it does not
   repair or convert incompatible hardware.

## Configuration ownership

| Concept | Owner |
| --- | --- |
| Artifact URL/path, digest, architecture, resource minimums, required data disks | Image |
| Firmware, ordinary disk protocol, NIC model, guest agent, graphics/video, hugepages | Libvirt hardware profile |
| Config-drive enablement, data format and attachment type/bus | Libvirt hardware profile |
| CPU count, RAM and new root-disk size | Instance type |
| Connection, pools, emulator and local network/storage bindings | Host |
| User-data, metadata and SSH keys | Provisioning request |
| Autostart | Successful user-requested power operations |

Profiles are reusable within one libvirt cloud. Multiple images may select one
profile. There is no profile inheritance, per-image hardware override or instance
family. Other providers do not need to implement this libvirt-specific model.

Every image needs a valid explicit profile reference. A missing or unknown
reference is a configuration error identifying the cloud and image. There is no
automatic profile synthesis or inline-only compatibility path.

## Before and after

Before (legacy excerpt inside a cloud):

```hcl
image "ubuntu" {
    path = "/var/lib/libvirt/images/ubuntu.img"
    os_arch = "x86_64"
    firmware = "efi"
    disk_bus = "scsi"
    disk_controller = "virtio-scsi"
    initialization = "openstack"
}
instance_type "small" {
    cpu = 2
    memory_mib = 2048
    boot_disk_gib = 20
    hugepages = true
    autostart = true
}
```

After (keep existing cloud host, zone, network and storage declarations):

```hcl
hardware_profile "linux-uefi" {
    firmware { type = "efi" }
    disks { bus = "scsi" controller_model = "virtio-scsi" }
    hugepages = true
    config_drive {
        enabled = true
        format = "openstack"
        device_type = "disk"
        bus = "virtio"
    }
}
image "ubuntu" {
    path = "/var/lib/libvirt/images/ubuntu.img"
    os_arch = "x86_64"
    hardware_profile = "linux-uefi"
}
instance_type "small" {
    cpu = 2
    memory_mib = 2048
    boot_disk_gib = 20
}
```

Use a separate named profile when another image needs different hardware. For
an image that does not consume cloud-init data, set `config_drive { enabled=false }`
inside its profile. Host `config_drive_pool` and `config_drive_suffix` remain host
settings; move any former format setting to the profile and remove `graphics_listen`.

## Deprecated fields

The following legacy fields produce migration warnings and are ignored, including
when a valid new profile is present. They never override that profile.

| Old location | Fields | Replacement |
| --- | --- | --- |
| Image | `efi`, `firmware`, `disk_bus`, `disk_controller`, `nic_model`, `guest_agent`, `graphics`, `video_model` | Named hardware profile |
| Image | `initialization` | Profile config-drive enablement and format |
| Instance type | `hugepages` | Profile hugepages policy |
| Instance type | `autostart` | No configurable replacement; lifecycle-managed |
| Host | `config_drive_write_format` | Profile config-drive format |
| Host | `graphics_listen` | No configurable replacement; derived loopback listener |

Warnings identify the configuration location and link to this guide. Remove the
old fields rather than relying on their apparent values. The defaults in new
profiles are independent of deprecated image and host fields.

## Ordinary disks and config drive

The profile's ordinary disk policy applies to the root disk, image-required data
disks, user-created data disks and existing volumes, both during creation and on
later attachments. A virtio policy is an administrator assertion that the guest
supports the protocol and already has its drivers. Vmango checks backend support;
it does not install guest drivers or silently fall back to another bus.

Virtio block disks use individual virtio devices. Virtio SCSI disks share a
compatible SCSI controller and receive distinct addresses. Controller/address
capacity is checked before new disk artifacts are created. Existing attachments
and addresses are preserved; conflicting per-attachment bus requests are rejected.

Config drive has a separate policy. Omission retains NoCloud on a virtio disk;
OpenStack data and explicitly disabled provisioning are also available. Device
type and bus must form a supported combination. Compatible controller dependencies
may be shared with ordinary disks. The generated drive remains VM-owned and is
excluded from system-disk boot priority. User-data and keys are not profile content.

## Existing VMs and adoption

Existing VMs stay observable. Ordinary mutations require both management identity
and valid persisted applied hardware policy. Older managed VMs missing that policy
need explicit re-adoption, just as unmanaged VMs need adoption.

Select a named profile explicitly during adoption. Vmango checks compatibility
against existing hardware and records the accepted policy without converting
disks, moving device addresses, changing graphics listeners or repairing hardware.
It does not infer policy from XML, the first disk or the current image catalog.
Mixed ordinary-disk buses that conflict with the selected profile require explicit
resolution outside adoption. Existing disk ownership is not claimed automatically.

The applied policy is stored with the VM independently of task-history retention.
Later edits or removal of a catalog profile do not change existing VM policy.
Instance-type changes update resources while retaining that policy, autostart,
explicit CPU pinning and existing disks. Incompatible resource changes fail.
Hugepages must already be prepared and available on the host; Vmango does not
allocate or configure host hugepage pools.

## Power operations

A successful user-requested start enables autostart, including creation with
`start=true`. Creation with `start=false` leaves autostart disabled. A successful
hard stop or confirmed graceful shutdown disables it. Graceful shutdown waits up
to 120 seconds for completion; command acceptance alone is not success.

Failed or unconfirmed power operations leave autostart unchanged. Reboot,
guest-initiated shutdown and instance-type changes do not synchronize autostart.
If power succeeds but autostart synchronization fails, task effects preserve the
successful power operation and the task reports the synchronization failure.

## Graphical consoles

New graphics-enabled VMs listen only on an explicit loopback address on the
libvirt host. Disabled graphics produces no listener. There is no editable
listener address. Existing VM listeners are preserved during upgrade and adoption.

Local connections use local loopback. Remote SSH connections tunnel graphics
traffic through SSH using applicable connection settings. Other remote libvirt
transports, including TLS/TCP, attempt SSH to the connection hostname on port 22,
never the libvirt API port. SSH authentication is non-interactive and verifies
host keys. Check the Vmango service account's SSH access and trusted host keys
when the console is unavailable.

Console SSH failures do not prevent profile acceptance, VM creation or ordinary
libvirt management. There is no direct remote VNC fallback or listener expansion.
Remote browser console access still requires HTTPS. Direct VeNCrypt/TLS support
is a separate feature.

## Accepted tasks and restart behavior

The command format has an explicit incompatible version boundary. All unfinished
pre-upgrade commands fail without executing, translating or reconstructing their
payloads. Terminal history remains unchanged. Recorded effects, uncertainty and
resource fences remain significant: failure is not evidence of rollback.

New-format tasks retain accepted policy and candidate-host bindings across
configuration edits and ordinary restarts. Execution gathers current inventory
and schedules in one preparation stage, then durably stores the selected host
and final plan before provisioning. No automatic retry or uncertain side-effect
replay is introduced. Supported work interrupted during execution retains the
existing interruption and inspection workflow.

## Machine type and acceleration

The optional profile `machine` block controls new VM platform selection. Omitted
blocks, empty blocks and omitted fields default to `type = "auto"` and
`acceleration = "hardware"`. `auto` is a machine-selection sentinel, never a
libvirt machine name. Acceleration has no `auto` value. The image's `os_arch`
remains the guest architecture; instance types cannot override it or the profile.

These independent profile examples belong inside a libvirt cloud. Select one
explicitly from an image; all other profile requirements must also be supported.
Names and machine versions below are illustrative, not a compatibility catalog.

```hcl
hardware_profile "server-q35" {
    machine { type = "q35" acceleration = "hardware" }
}
hardware_profile "linux-kvm" {
    machine { type = "q35" acceleration = "kvm" }
}
hardware_profile "arm-hvf" {
    machine { type = "virt" acceleration = "hvf" }
    firmware { type = "efi" }
}
hardware_profile "arm-emulated" {
    machine { type = "virt" acceleration = "qemu" }
    firmware { type = "efi" }
}
hardware_profile "pinned-platform" {
    machine { type = "pc-q35-9.2" acceleration = "kvm" }
}
```

`hardware` requires compatible KVM or HVF. `kvm` requires Linux KVM, `hvf`
requires macOS Hypervisor.framework, and `qemu` explicitly selects QEMU/TCG
software emulation. These concrete values map directly to the root libvirt
`domain` type. Hardware never falls back to software, and explicit backends never
substitute for one another. The host must already have a suitable QEMU build,
permissions and prepared acceleration. Its OS name alone proves nothing.
Hardware acceleration generally requires a compatible host/guest architecture;
TCG can support other architectures only when the emulator and the full profile
support them. It does not promise hardware-equivalent performance or CPU/device
features. Vmango does not install QEMU, load KVM modules or enable HVF.

Machine names and aliases come from the backend. At acceptance, each candidate
host resolves the requested architecture, emulator and acceleration together,
then resolves `auto` or an alias in that context. Driver canonical mappings and
exact domain-capability queries determine the concrete name. Vmango does not
sort version numbers or infer chipset families from name prefixes. `q35` can
resolve to `pc-q35-9.2` on one host and a different version on another; specifying
`pc-q35-9.2` requires exactly that supported machine on every eligible candidate.
An unversioned backend machine without a canonical version keeps its real name;
this does not provide a version-pinning guarantee. The concrete result is emitted
as `os/type/@machine`, never `auto`.

Firmware, CPU mode, ordinary/config-drive disk buses, NIC and display requirements
are validated for that same combination before provisioning artifacts. A machine
name appearing in a list is insufficient. Unsupported candidates are excluded
with diagnostics; no candidate means rejection without disk/config-drive creation.
Capability inspection does not create temporary VMs or modify host configuration.

Emulators remain host-local bindings:

```hcl
host "kvm-01" {
    uri = "qemu+ssh://root@kvm-01/system"
    emulator = "/usr/bin/qemu-system-x86_64"
    # Keep this host's zone, network and storage bindings here.
}
```

The path belongs to the hypervisor host, including with remote SSH connections.
An explicit path must support the complete requested combination; Vmango never
replaces an unavailable/incompatible path. When omitted, libvirt supplies the
appropriate path. The resolved path is stored and emitted as `devices/emulator`.
No per-profile emulator, arbitrary QEMU options, machine properties, download
catalog or binary management is supported.

### Accepted bindings and existing machines

Acceptance stores the requested policy (including which fields were explicit)
and each candidate's concrete machine, domain type and emulator. Execution
collects live inventory and schedules against those saved candidates. It
revalidates the exact saved combination without resolving the original alias or
using edited host settings. The selected host and final plan are persisted before
provisioning side effects. Capability removal produces a diagnostic, not a newer
machine, substitute path, software fallback or automatic retry of uncertain work.
Applied policy lives in VM metadata independently of task-history retention.

Start/stop/reboot, resource and device changes preserve existing platform bindings.
Ordinary mutations still require management identity and a valid applied policy.
Compatible older policy/accepted payloads without the new optional machine block
retain their interpretation; this extension alone does not require re-adoption.
The foundation's incompatible-command boundary still preserves terminal history,
recorded effects and uncertainty/resource fences without replay.

Adoption is validation, not conversion. Omitted fields and explicit `type="auto"`
preserve the existing machine rather than imposing today's creation defaults.
An explicit concrete name must match the existing concrete machine. An explicit
alias is resolved on that host at adoption time: if `q35` now points to a newer
version, use a compatible profile naming the existing older version exactly.
Ambiguous aliases in old XML are not guessed to refer to a historical version.
Explicit `hardware` rejects TCG; explicit `kvm`, `hvf` or `qemu` must match the
existing domain type. A configured host emulator must match the existing path.
Adoption writes policy metadata while preserving hardware.

Administration profiles show requested machine/acceleration and whether each was
explicit. Machine details/API expose the applied platform and separately observed
backend XML bindings, including unmanaged VMs. A requested alias is not presented
as a pinned concrete version. No API/UI/CLI operation changes existing guest
architecture, machine type, acceleration or emulator; migration, conversion and
automatic remediation remain outside this feature.

### QEMU updates and guarantees

The saved machine version is not the QEMU package or binary version. A VM using
`pc-q35-9.2` continues using that type after an administrator updates QEMU while it
remains supported. Removing that machine's support makes start fail; Vmango does
not upgrade it. Replacing the binary at the saved path changes implementation at
the next cold start. The path does not pin file contents, and machine compatibility
behavior is not byte-level reproducibility or immunity to all host changes.
Package/artifact updates, copying, downloading and hashing QEMU binaries remain
administrator responsibilities.

References: [libvirt driver capabilities and canonical aliases](https://libvirt.org/formatcaps.html#examples),
[domain capabilities](https://libvirt.org/formatdomaincaps.html),
[domain types](https://libvirt.org/formatdomain.html#element-and-attribute-overview),
[guest architecture and machine type](https://libvirt.org/formatdomain.html#guest-firmware),
[QEMU/KVM/HVF driver](https://libvirt.org/drvqemu.html),
[QEMU machine invocation](https://www.qemu.org/docs/master/system/qemu-manpage.html),
[machine-version compatibility](https://www.qemu.org/docs/master/devel/migration/compatibility.html),
[ARM virt platform](https://www.qemu.org/docs/master/system/arm/virt.html).

## Guest system information and hardware clock

A profile may constrain descriptive SMBIOS information, the guest hardware clock,
and individual timers. These remain administrator-managed HCL settings; VM
creation still selects an image and instance type. An example for a supported
x86 Q35/KVM backend is:

```hcl
hardware_profile "identified-x86" {
    machine { type = "q35" acceleration = "kvm" }
    smbios {
        bios { vendor = "Example" version = "1.0" date = "01/02/2026" release = "1.2" }
        system { manufacturer = "Example" product = "Cloud VM" version = "1" sku = "general" family = "Cloud" }
        baseboard { manufacturer = "Example" product = "Virtual Board" version = "1" }
    }
    clock { mode = "timezone" timezone = "Europe/Berlin" }
    timer "rtc" { tickpolicy = "catchup" }
    timer "pit" { tickpolicy = "discard" }
    timer "hpet" { present = false }
    timer "kvmclock" { present = true }
}
```

Only the illustrated SMBIOS fields are configurable, with one block of each
kind. Omitted fields remain unspecified. Empty strings, duplicate declarations,
unknown fields, control characters and values over 255 bytes are rejected.
BIOS dates use `mm/dd/yy` (year 19yy) or `mm/dd/yyyy` and must be real dates;
release uses `major.minor`, each component from 0 through 255. Profiles do not
configure serials, asset tags, chassis, OEM strings or independent UUIDs. New VMs
with SMBIOS policy use `os/smbios mode="sysinfo"`, and the emitted system UUID
matches that VM's domain UUID. No physical-host SMBIOS information is copied.
Omitting the SMBIOS block retains the previous creation behavior.

The hardware clock defaults to UTC, including when a profile supplies only
timers. `clock { mode="utc" }` explicitly requires UTC; an omitted clock mode
remains unconstrained for adoption. `localtime` follows the selected hypervisor
host's timezone. `timezone` requires a named IANA zone and retains that name
instead of freezing its current numeric offset. The name is validated against
Go's timezone database; the QEMU host must also have its corresponding timezone
data installed. A timezone argument is invalid with other modes. None of these
settings changes the guest OS timezone, NTP or other guest software. Numeric
offsets, fixed startup dates and reset/adjustment controls are not configurable.

### Timer and platform support

Unspecified timers and attributes retain backend defaults. No OS-specific preset
is inferred. A timer may be declared only once, using these attributes:

| Timer | Attributes | Platform envelope |
| --- | --- | --- |
| `rtc` | `tickpolicy="delay"` or `"catchup"` | x86 PC/Q35, KVM or QEMU |
| `pit` | `tickpolicy="delay"`, `"discard"`, or verified `"catchup"` | x86 PC/Q35, KVM or QEMU; catchup needs the backend's KVM PIT capability |
| `hpet` | `present=true/false` | x86 PC/Q35, KVM or QEMU |
| `kvmclock` | `present=true/false` | x86 PC/Q35 with KVM |
| `hypervclock` | `present=true/false` | x86 PC/Q35 with KVM |
| `armvtimer` | `tickpolicy="delay"` or `"discard"` | ARM virt with KVM and the backend's `kvm-no-adjvtime` capability |

SMBIOS is limited to x86 PC/Q35 under KVM/QEMU. Explicit clock modes are admitted
for those platforms and ARM virt under KVM/HVF/QEMU. Other combinations fail as
unverified. These envelopes do not by themselves establish support: each
candidate must also pass libvirt's native QEMU command conversion for the exact
architecture, canonical machine, acceleration and emulator. This invokes the
backend's capability checks and command builder, including timer-specific flags;
XML schema acceptance or an absent domain-capabilities enumeration is not enough.
The returned command is never executed. No probe domain is defined or started,
and no probe disk, network or config-drive is created. If conversion is
unavailable or rejects the combination, that candidate is excluded with an
unverified-requirement diagnostic before provisioning artifacts. Preflight repeats
this verification against the accepted binding; it cannot change acceleration or
drop a setting. A backend upgrade removing native conversion requires a new
verified adapter, not a silent bypass.

The implementation follows the [libvirt format contract](https://libvirt.org/formatdomain.html#time-keeping)
and [QEMU timer validation](https://github.com/libvirt/libvirt/blob/master/src/qemu/qemu_validate.c).
`armvtimer` requires libvirt 6.1 or later and compatible QEMU/kernel support;
HVF is not ARM/KVM, and ARM virtual timers cannot be disabled here. Real results
for the specific tested libvirt/QEMU versions are recorded in
[acceptance/README.md](../acceptance/README.md); an ARM/KVM unit-test fixture is
not a real-host verification. PIT catchup is accepted only when the native
backend check verifies its capability, including on older vendor builds.

`delay` delivers ticks at the normal rate and may leave the guest behind;
`catchup` attempts to recover missed ticks; `discard` drops them. These policies
do not guarantee wall-clock accuracy. TSC frequency/mode, RTC tracking, catchup
tuning and a generic timer disable control are outside this interface.

### Existing machines and durable policy

Acceptance saves the explicit constraints and each candidate's plan. Creation
records applied system/clock settings separately from the requested profile;
adoption records the observed existing settings. Administration/API displays
requested constraints, saved applied settings and current persistent XML
observations separately. Observations do not establish guest-visible behavior or
clock accuracy, and omitted XML timer attributes remain unspecified.

Adoption checks only explicitly constrained fields and rejects mismatches or
unverifiable values before changing metadata. In particular, a missing XML
attribute does not prove a requested default. An omitted clock mode does not
turn an existing localtime/variable clock into UTC. Adoption and unrelated
mutations preserve the complete existing system/clock XML, including serials,
identifiers, OEM entries, clock adjustments and timer settings that this feature
cannot configure. Multiple existing baseboards are preserved, but a requested
baseboard constraint cannot be matched ambiguously. There is no operation here
to reconfigure these settings on an existing machine.

Catalog edits, restarts and task-history pruning do not replace saved policy.
Compatible older plans lacking the optional SMBIOS/clock/timer fields remain
valid with their previous meaning; no blanket re-adoption or command-version
change is introduced. Missing or unsupported management policy still requires
explicit adoption under the foundation contract.

## CPU policy and admission upgrade (KRG-113)

Remove every `cpu_request` field: configuration now rejects it rather than
silently discounting admission. Historical VM metadata remains intact but full
configured vCPUs count, including stopped/unmanaged domains. Capacity uses
online physical cores (SMT siblings do not add cores), host reserve and overcommit
(default 4). A previously overcommitted host remains observable; compatible
resource reductions are allowed, additional exhausted-resource consumption fails.

Command version 4 deliberately rejects queued/running version-3 commands at the
existing incompatible-upgrade boundary. Inspect and resubmit; uncertain work
retains effects/resource fences and is never replayed. Terminal history is
preserved. Version-1 applied policies remain supported; omitted optional CPU
policy does not require blanket re-adoption or hardware rewrites.

New VMs get one socket, N cores and one thread per core. Existing/adopted
topology and pins remain unchanged; only stopped simple-topology CPU resize or
compatible RAM-only changes are supported. Consult the
[CPU policy reference](content/en/docs/cpu-policies.md) for complete configuration,
capability limits, native numad prerequisites and Hyper-V/timer dependencies.

## Firmware block migration

The flat hardware-profile syntax `firmware = "efi"` is rejected with migration
instructions. Replace it with `firmware { type = "efi" }` (likewise `bios` or
`auto`). An omitted or empty block is `auto`. New explicit EFI defaults to
firmware without Secure Boot; request `secure_boot = true` for support and
initially enrolled keys. Deprecated image-inline firmware remains warned and
ignored. Supported persisted policy/commands keep their historical meaning.
See [firmware profiles and NVRAM](FIRMWARE.md) for named profiles, host bindings,
formats, ownership, missing-file regeneration and security evidence.

## Ordinary disks and config-drive controllers

Flat hardware-profile `disk_bus`/`disk_controller` fields now fail startup with
an actionable migration error. Move them to `disks { bus = "scsi"
controller_model = "virtio-scsi" }`. Use `virtio`, `scsi`, `ide` or `sata`;
`virtio-blk` and `virtio-scsi` are not new bus aliases. SCSI config drive needs
its own explicit `controller_model`. See [Disk policy and complete examples](DISKS.md).

This configuration migration does not convert existing VM devices or alter
supported saved plans. Adoption checks compatibility without rewriting hardware.

## Graphics, video and USB input blocks

Flat hardware-profile `graphics` and `video_model` fields are rejected. Use
`graphics { type="vnc" video_model="virtio" }`, `input { keyboard="usb" pointer="usb-tablet" }`
and optional `usb_controller { model="qemu-xhci" }`. New defaults on both x86/ARM
are VNC, virtio video and USB keyboard/tablet with one shared XHCI. To retain no
managed USB input for newly accepted guests, explicitly disable both input fields.
An explicit `video_model="none"` still means no video; it is never replaced.

Use `graphics {type="none"}` for a serial-only server. It adds no graphics/video,
input or USB dependency; an explicit standalone XHCI is retained. Existing
applied policies and accepted plans keep historical omission semantics. Adoption
preserves omitted input constraints and extra PS/2/devices/addresses/listeners.
Image-inline deprecated fields and host `graphics_listen` remain warned/ignored.
See [graphics/input documentation](content/en/docs/graphics-input.md) and the
[parser-tested examples](examples/graphics.conf) before migrating profiles.
