---
title: "Binary quick start"
description: "Download Vmango, connect a libvirt host and open the web interface."
weight: 1
---

Download an archive and `SHA256SUMS` from
[GitHub Releases](https://github.com/subuk/vmango/releases).
Choose the **Vmango server's** architecture, which may differ from the guest VM's
architecture. No Docker or Go compiler is needed.

## Choose an archive

Replace `VERSION` below with the version shown on the release, without its `v` prefix.

| Archive | Runtime |
| --- | --- |
| `vmango-VERSION-linux-x64.tar.gz` | x86_64 Linux with glibc 2.35 or newer; built on Ubuntu 22.04 |
| `vmango-VERSION-macos-arm64.tar.gz` | Apple Silicon, macOS 15 or newer, Homebrew in `/opt/homebrew` |
| `vmango-VERSION-windows-x64.zip` | 64-bit Windows; CI runs on Windows Server 2022; connects to a remote libvirt host |

These are dynamically linked applications. The Windows ZIP includes libvirt DLLs,
their dependencies, `virsh.exe`, license notices and a package manifest. Keep them
next to `vmango.exe`. Linux and macOS use system-installed libvirt libraries.
The archives include a minimal configuration and this guide.

### Linux prerequisites

On Ubuntu 22.04 or newer:

```sh
sudo apt-get update
sudo apt-get install libvirt0 libvirt-clients genisoimage openssh-client
```

`genisoimage` supplies the `mkisofs` and `isoinfo` commands used to create and read
cloud-init config drives. Other distributions need equivalent packages; Alpine's
musl runtime is not supported by this Linux archive.

### macOS prerequisites

Install [Homebrew](https://brew.sh/), then:

```sh
brew install libvirt dvdrtools
```

The binary links to Homebrew's libvirt in `/opt/homebrew`. `dvdrtools` provides
`mkisofs` and `isoinfo`. This does not configure a local hypervisor: use a remote
Linux/KVM host for this quick start. Local QEMU/HVF needs separate setup.
The binary is not Developer ID signed or notarized; follow your organization's
policy for downloaded executables.

### Windows prerequisites

Extract the complete ZIP. The bundled libvirt client does not run a Windows
hypervisor. Use a remote Linux/KVM host with authenticated TLS (`qemu+tls`).
Follow [libvirt's TLS setup](https://libvirt.org/kbase/tlscerts.html) and
[Windows client certificate locations](https://libvirt.org/windows.html).
Do not substitute an unauthenticated public TCP connection.

The bundled Windows client cannot use SSH: `qemu+ssh` is unsupported on Windows,
and the Fedora MinGW libvirt build has both `qemu+libssh` and `qemu+libssh2`
disabled. Installing OpenSSH or adding a libssh2 DLL does not enable these
transports. CI probes all three URIs with the packaged `virsh.exe`; using SSH
would require a different libvirt build and a separate connection test.

The archive does not include ISO tools. Creating or reading cloud-init config
drives requires Windows-compatible `mkisofs.exe` and `isoinfo.exe` on `PATH`.
Until those are installed, use Windows for managing existing VMs; provisioning
with config drives is not covered by the Windows CI smoke check. Event hook
scripts also require a `sh` executable. See the verification scope below.

## Verify and unpack

On Linux, from the directory containing the archive and checksums:

```sh
sha256sum --ignore-missing -c SHA256SUMS
tar -xzf vmango-VERSION-linux-x64.tar.gz
cd vmango-VERSION-linux-x64
cp vmango.quickstart.conf vmango.conf
```

On macOS:

```sh
shasum -a 256 vmango-VERSION-macos-arm64.tar.gz
# Compare the result with this archive's line in SHA256SUMS.
tar -xzf vmango-VERSION-macos-arm64.tar.gz
cd vmango-VERSION-macos-arm64
cp vmango.quickstart.conf vmango.conf
```

On Windows, in PowerShell:

```powershell
Get-FileHash .\vmango-VERSION-windows-x64.zip -Algorithm SHA256
# Compare the result with this archive's line in SHA256SUMS.
Expand-Archive .\vmango-VERSION-windows-x64.zip -DestinationPath .
Set-Location .\vmango-VERSION-windows-x64
Copy-Item vmango.quickstart.conf vmango.conf
# Make the bundled virsh and any tools in this directory available by name.
$env:PATH = "$PWD;$env:PATH"
```

## Connect a hypervisor

For Linux running on an already configured KVM host, verify access as the same
user who will run Vmango:

```sh
virsh -c qemu:///system list --all
```

For Linux or macOS connecting to a remote host, first configure SSH key access
and confirm the host key interactively. The remote account must have permission
to use the libvirt system socket:

```sh
ssh libvirt@hypervisor.example.com
virsh -c 'qemu+ssh://libvirt@hypervisor.example.com/system' list --all
```

Use that URI in `vmango.conf`. On Windows, test the configured TLS connection
with the bundled client:

```powershell
.\virsh.exe -c 'qemu+tls://hypervisor.example.com/system' list --all
```

Edit the archive's minimal configuration:

```hcl
key_file = "./authorized_keys"

libvirt "local" {
    uri = "qemu:///system"
    config_drive_pool = "default"
}

api {
    listen = "127.0.0.1:8080"
    base_url = "http://localhost:8080"
    jwt_signing_key = "REPLACE_WITH_RANDOM_SECRET"
    console_signing_key = "REPLACE_WITH_SEPARATE_RANDOM_SECRET"

    user "admin" {
        hashed_password = "REPLACE_WITH_GENPW_OUTPUT"
    }
}
```

Replace `uri` with the verified URI. The name `local` is just a label; you can
rename it to describe a remote host. Use an existing, active storage pool for
`config_drive_pool`. For example, check `virsh -c URI pool-list --all`.

### Manage multiple hosts

One Vmango instance can manage virtual machines across multiple local or remote
libvirt hosts from the same web interface. Add one `libvirt` block per host in
`vmango.conf`, giving each block a unique name:

```hcl
libvirt "local" {
    uri = "qemu:///system"
    config_drive_pool = "default"
}

libvirt "remote" {
    uri = "qemu+ssh://libvirt@hypervisor.example.com/system"
    config_drive_pool = "default"
}
```

This example connects a Linux Vmango server to its local KVM host and a remote
host over SSH. Use the connection types supported by your server platform;
Windows requires remote hosts with authenticated TLS, as described above.
Verify each URI and its storage pool as the user running Vmango. Pool names
refer to pools on each respective host; they do not create shared storage.
Restart Vmango after changing the configuration, then select the target node
when creating a VM.

## Set a password and start

Run `./vmango genpw` (PowerShell: `.\vmango.exe genpw`), enter your administrator
password, and paste the printed bcrypt hash into `hashed_password` without
changing its `$` characters. Run this in an interactive terminal.

Generate two separate persistent signing keys with `openssl rand -hex 32` on Linux/macOS. In Windows
PowerShell, use:

```powershell
$bytes = New-Object byte[] 32
$rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
$rng.GetBytes($bytes)
$rng.Dispose()
[BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
```

Generate a second value independently. Set `api.jwt_signing_key` and
`api.console_signing_key` to the two values. Preserve both across restarts. Keep `vmango.conf` private to your user.
Start from the extracted directory, which must be writable:

```sh
./vmango web -c vmango.conf
```

On Windows:

```powershell
.\vmango.exe web -c vmango.conf
```

Open [http://localhost:8080/](http://localhost:8080/) and log in as `admin` with
the password you just entered. The node should appear after login. Stop the
foreground server with Ctrl+C.

To create a VM, follow the [cloud images and provisioning guide](/docs/images/)
to put a cloud-init-enabled image in a storage pool **on the hypervisor** and
register it in `vmango.conf`. Configure networking on the hypervisor first.
The binary archive does not configure KVM, pools, networks or guest images for you.

For remote browser access, serve Vmango through HTTPS, set `api.base_url` to
that public URL and enable `api.session_secure`. Preserve `Host`, `Origin` and
`Sec-Fetch-Site` in the reverse proxy. Remote VNC access needs HTTPS too.
SSH console forwarding also needs `ssh` locally and `socat` or `nc` on the
hypervisor. TLS-connected consoles require network reachability to the host's
VNC port; a successful libvirt TLS connection alone does not tunnel VNC.

## Troubleshooting zero nodes

- Check the startup log for the configuration filename. Use `-c` explicitly
  if the current directory differs from the extracted archive.
- Define at least one `libvirt "name"` block. Nodes are configured explicitly,
  not discovered automatically.
- Read `cannot get node` / `cannot open libvirt connection` messages. A failed
  connection is omitted from the node list.
- Run `virsh -c 'EXACT_URI' list --all` as the same user and with the same
  environment. Fix socket permissions, SSH keys, host-key trust or TLS
  certificates there first. A remote account may need membership in the host's
  libvirt group and a new login session.
- Verify the pool exists and is active on that connection. An empty VM list
  is normal on a new host and does not mean that no nodes are configured.

## Verification scope

CI builds each archive, extracts it and checks server startup, login and a node
using libvirt's `test:///default` driver. Unit tests run on Linux, macOS and
Windows. This checks dynamic loading and the HTTP path, not a real hypervisor.
Linux and macOS CI also checks an ISO round trip with the installed tools.
Windows config-drive tools and real Windows-to-Linux TLS/VM operations require
separate integration verification before claiming those deployment scenarios
as tested. A successful macOS check does not establish Linux/KVM behavior.
Verify VM lifecycle operations on a real hypervisor before release.
See [the release process](https://github.com/subuk/vmango/blob/master/HOWTO-RELEASE.md).
