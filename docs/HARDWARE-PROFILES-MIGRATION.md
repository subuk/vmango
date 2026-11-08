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
| CPU count, CPU admission request, RAM and new root-disk size | Instance type |
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
    firmware = "efi"
    disk_bus = "scsi"
    disk_controller = "virtio-scsi"
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
