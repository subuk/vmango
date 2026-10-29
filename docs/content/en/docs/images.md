---
title: "Cloud images and provisioning"
description: "Find cloud images, register them in Vmango and launch a virtual machine."
weight: 3
---

Vmango starts machines from pre-installed cloud images. There is no need to boot
an installer or mount an installation CD for each VM: cloud-init handles the
initial configuration on first boot.

## Choose a cloud image

Download a pre-installed disk image with cloud-init from the distribution's
official site. For QEMU/libvirt, choose QCOW2 and the architecture of your host:
`amd64`/`x86_64` or `arm64`/`aarch64`.

- [Ubuntu Cloud Images](https://cloud-images.ubuntu.com/releases/): choose a
  supported release and its `server-cloudimg-*.img` disk image (QCOW2 despite
  the `.img` extension).
- [Debian Cloud Images](https://cloud.debian.org/images/cloud/): choose a supported
  release, then `latest/` and a `generic-*.qcow2` image for broad driver support.
- [Fedora Cloud](https://fedoraproject.org/cloud/download/): download the Cloud
  Base QCOW2 image.
- [AlmaLinux Generic Cloud](https://wiki.almalinux.org/cloud/Generic-cloud.html):
  cloud-init-enabled QCOW2 images, with download links by release and architecture.
- [Rocky Linux](https://rockylinux.org/download): choose a GenericCloud QCOW2 image
  from the cloud image downloads.
- [Alpine Linux Cloud Images](https://www.alpinelinux.org/cloud/): choose a
  virtual-machine QCOW2 image with **cloud-init** as the bootstrap system and a
  boot method matching your VM (BIOS or UEFI).

These are upstream download sources, not a tested compatibility matrix. Image
defaults vary; select an SSH key when creating the VM and use the distribution's
default cloud user to log in.

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
`x86_64` or `aarch64`. The selector shows images matching the selected
architecture whose source volume is visible, available and not attached to a VM.

The catalog is shared across all nodes. Vmango resolves `path` on the selected
node and reads that volume's actual capacity and format. A volume on another
node cannot satisfy a missing source. The same path on several nodes intentionally
receives the same settings; administrators must ensure its contents match the
image description. Vmango does not download or replicate images.

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

Explicit creation options take precedence over image defaults, which take
precedence over the existing fallback behavior. The simple form uses image
defaults. The advanced form offers **Image default** for the clone bus, NIC model
and guest agent, while retaining explicit choices. Other image settings are
configured in HCL; this feature does not add a full settings editor to the UI.
An explicit `false` or `none` is never treated as an omitted value.

Minimum memory and disk capacity are checked before any disks are created or
cloned. A clone cannot shrink the source volume. If the advanced form leaves the
clone size empty, Vmango uses the larger of the source capacity and the image's
minimum. If several images are cloned into one VM, each disk uses its own image's
bus default. Conflicting explicit image defaults for VM-wide settings (firmware,
initialization, NIC model, guest agent or the SCSI controller) require a common
override or compatible catalog definitions before creation can proceed.

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

1. **Choose an image.** Select a registered cloud-init-enabled OS image from
   your storage pool.
2. **Make it yours.** Set CPU, memory and disk size, select your SSH keys and
   optionally provide cloud-init userdata to install packages or run setup
   commands.
3. **Create and start.** Vmango clones the disk and supplies the configuration
   to the guest. The OS boots from the image and configures itself on first boot.

Once your hosts and images are set up, launching another machine is a single
form and a click. Your infrastructure stays on your own hardware.
