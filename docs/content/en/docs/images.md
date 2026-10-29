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
libvirt storage pool on the host that will run the VM. Register its path and OS
metadata in an `image` block in
[vmango.conf](https://github.com/subuk/vmango/blob/master/vmango.dist.conf) so it
appears in Vmango's image selector. Set `efi = true` for images that require UEFI.

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
