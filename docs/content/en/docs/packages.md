---
title: "Package installation"
description: "Install release packages on Ubuntu, Debian and RHEL."
weight: 2
---

Use the release repositories below. Configure `/etc/vmango.conf` for your
libvirt host and web login before starting the service.

## Ubuntu 22.04, 24.04 and 26.04 LTS (amd64)

```sh
sudo add-apt-repository ppa:subuk/vmango
sudo apt-get update
sudo apt-get install vmango
```

The [release PPA](https://launchpad.net/~subuk/+archive/ubuntu/vmango)
contains separate Jammy, Noble and Resolute builds.

## Debian 12 and 13 (amd64)

Use the **Ubuntu 22.04 Jammy build** from the release PPA. These are Ubuntu
packages, not native Debian builds. The package validation workflow checks
installation, server startup, login and a libvirt test node on Debian 12
(Bookworm) and Debian 13 (Trixie). Real KVM operations require separate testing.

Add only the Vmango PPA, with its signing key restricted to this repository:

```sh
sudo apt-get update
sudo apt-get install ca-certificates curl gnupg
curl -fsSL 'https://keyserver.ubuntu.com/pks/lookup?op=get&search=0x10538B83BC93D600E0C98A39AF0F396262965789' \
  -o /tmp/vmango-ppa.asc
gpg --show-keys --with-fingerprint /tmp/vmango-ppa.asc
```

Verify the primary key fingerprint is
`1053 8B83 BC93 D600 E0C9 8A39 AF0F 3962 6296 5789`, also shown on the
[PPA overview](https://launchpad.net/~subuk/+archive/ubuntu/vmango).
Then install the key and add the repository:

```sh
sudo install -d -m 0755 /etc/apt/keyrings
sudo install -m 0644 /tmp/vmango-ppa.asc /etc/apt/keyrings/vmango-ppa.asc
printf '%s\n' \
  'Types: deb' \
  'URIs: https://ppa.launchpadcontent.net/subuk/vmango/ubuntu' \
  'Suites: jammy' \
  'Components: main' \
  'Architectures: amd64' \
  'Signed-By: /etc/apt/keyrings/vmango-ppa.asc' \
  | sudo tee /etc/apt/sources.list.d/vmango.sources
sudo apt-get update
sudo apt-get install vmango
```

Keep `Suites: jammy` on both Debian versions. Do not replace it with
`bookworm` or `trixie`, and do not add Ubuntu's main repositories to Debian.
The Go compiler and compiler PPA are not needed to install binary packages.

## RHEL 8, 9 and 10 (x86_64)

```sh
sudo dnf install -y dnf-plugins-core
sudo dnf copr enable subuk/vmango
sudo dnf install vmango
```

See the [release COPR project](https://copr.fedorainfracloud.org/coprs/subuk/vmango/)
for completed builds and repository setup for your system.

## Configure and start

Edit `/etc/vmango.conf`, configure a password and connect a libvirt host as
described in the [quick-start guide](/docs/start/). Package installation does
not configure a hypervisor, storage pools or VM images.

On systems where the package installs the systemd unit:

```sh
sudo systemctl enable --now vmango
```
