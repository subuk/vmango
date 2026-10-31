---
title: "Docker installation"
description: "Run the Vmango Docker image on Linux amd64 or arm64 and connect existing libvirt hosts."
weight: 2
---

The [subuk/vmango image on Docker Hub](https://hub.docker.com/r/subuk/vmango)
includes Vmango, its web interface, libvirt clients, SSH, CA certificates and
the ISO tools used for cloud-init config drives. It connects to existing
hypervisors; it does not start a libvirt daemon or configure a KVM host.

## Choose an image

```sh
docker pull subuk/vmango:0.15.1
```

The examples below pin release `0.15.1`. Use a published version from
[Docker Hub](https://hub.docker.com/r/subuk/vmango/tags) when upgrading.

| Tag | Contents |
| --- | --- |
| `0.15.1` | Release image for Linux amd64 and arm64; Docker selects the matching platform |
| `latest` | Latest stable release, with both Linux platforms |
| `0.15.1-amd64` | Explicit Linux amd64 image |
| `0.15.1-arm64` | Explicit Linux arm64 image |

Prereleases have version-specific tags and do not update `latest`. The image's
architecture is the **Vmango server's** architecture; guest VM architectures
depend on the connected hypervisors and configured images.

## Prepare the configuration

Export the example configuration from the same image you will run. Start in a
new directory so the command does not overwrite an existing `vmango.conf`:

```sh
mkdir vmango-docker
cd vmango-docker
docker run --rm --entrypoint cat subuk/vmango:0.15.1 /etc/vmango.conf > vmango.conf
docker run --rm -it subuk/vmango:0.15.1 genpw
```

Enter an administrator password in the interactive terminal. In `vmango.conf`,
add a `user "admin"` block inside the existing `api` block and paste the printed
bcrypt hash into `hashed_password`. Preserve its `$` characters.

Keep `api.listen = ":8080"` and set `api.base_url` to your public HTTPS URL.
The following shows the relevant API settings; edit the existing block rather
than appending a second one:

```hcl
api {
    listen = ":8080"
    base_url = "https://vmango.example.com"
    session_secure = true
    jwt_signing_key_file = "/var/lib/vmango/jwt_signing_key"
    console_signing_key_file = "/var/lib/vmango/console_signing_key"
    session_dir = "/var/lib/vmango/sessions"
    token_dir = "/var/lib/vmango/tokens"

    user "admin" {
        hashed_password = "REPLACE_WITH_GENPW_OUTPUT"
    }
}
```

Keep the shipped `image`, `cloud` and `tasks` blocks and the state paths under
`/var/lib/vmango`. Configure the existing storage pools and networks for each
host, following [configured clouds and scheduling](/docs/clouds/). The
[image guide](/docs/images/) explains the managed image catalog.

## Connect a libvirt host

### Remote hosts over SSH

Inside the existing `cloud "local"` → `host "local"` block, replace the URI:

```hcl
uri = "qemu+ssh://libvirt@hypervisor.example.com/system"
```

The SSH account must have permission to use libvirt on that hypervisor.
Prepare a dedicated SSH directory with its private key, public key and a
`known_hosts` file containing the verified hypervisor host key. Mount that
directory read-only at `/var/lib/vmango/.ssh` when starting the container:

```sh
--mount type=bind,src=/absolute/path/to/vmango-ssh,dst=/var/lib/vmango/.ssh,readonly
```

The image runs as UID/GID `10001:10001`. Its user must be able to traverse the
mounted directory and read the key, SSH configuration and `known_hosts`.
Keep private keys restricted to their owner. Set ownership on a dedicated copy
of these files rather than changing your personal SSH directory.
Use an SSH `config` file in that directory if the key has a non-default name or
the host uses a custom SSH port. Passphrase-protected keys require a separately
configured SSH agent. Do not disable host-key checking.

Test the connection with the same mounted SSH directory before starting Vmango:

```sh
docker run --rm \
  --mount type=bind,src=/absolute/path/to/vmango-ssh,dst=/var/lib/vmango/.ssh,readonly \
  --entrypoint virsh subuk/vmango:0.15.1 \
  -c 'qemu+ssh://libvirt@hypervisor.example.com/system' list --all
```

### A local Linux libvirt socket

For `qemu:///system` on the Docker host, mount its libvirt socket directory at
`/run/libvirt` and grant the container user the socket's numeric group ID. For
example, add these options to the container command, replacing `LIBVIRT_GID`
with that group ID:

```sh
--mount type=bind,src=/run/libvirt,dst=/run/libvirt,readonly \
--group-add LIBVIRT_GID
```

The socket and any libvirt access policy must allow that group to connect.
Verify the URI with `virsh` using the same mount and supplementary group.
This example is for a Linux host; Docker Desktop does not expose a macOS libvirt
socket automatically. Use a reachable remote host for that setup.

## Start behind an HTTPS proxy

Create a named volume for application state, then run Vmango. Add the SSH mount
or libvirt socket options from the previous section to match your host URI.

```sh
docker volume create vmango-data
docker run -d --name vmango --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  --mount type=bind,src="$PWD/vmango.conf",dst=/etc/vmango.conf,readonly \
  --mount type=volume,src=vmango-data,dst=/var/lib/vmango \
  subuk/vmango:0.15.1
docker logs vmango
```

Point an HTTPS reverse proxy on the Docker host at `http://127.0.0.1:8080`.
It must preserve the public `Host`, `Origin` and `Sec-Fetch-Site` headers and
support WebSocket upgrades for the serial and VNC consoles. Open the public
HTTPS URL from `api.base_url` and sign in with the configured administrator.

Docker forwards a non-loopback peer address to the container, so the API's
localhost HTTP exception does not apply even when the published host port is
bound to `127.0.0.1`. Configure both `api.base_url` with an HTTPS URL and
`api.session_secure = true`; use the proxy for browser access. If the proxy runs
in another container, put both on a private Docker network and proxy to
`http://vmango:8080` instead of the proxy container's own localhost.

The named volume stores signing keys, SSH key records, sessions, API-token
metadata and durable task/image/volume metadata. VM disks and downloaded cloud
images remain in the storage pools on the hypervisors.

## Upgrade and troubleshoot

Back up `vmango.conf` and `vmango-data`. Pull the desired version, stop and remove
only the application container, then recreate it with the new image and the
same configuration, data volume and connection mounts. Keep the named volume
across upgrades so credentials and task history survive.

| Symptom | Check |
| --- | --- |
| Login or API returns `403 transport_rejected` | HTTPS proxy, public `api.base_url` and `api.session_secure` |
| State or signing-key permission error | Write access to the data volume for UID/GID `10001:10001` |
| SSH host cannot connect | Key permissions, verified `known_hosts`, host URI and remote libvirt permissions |
| Local libvirt socket cannot connect | Socket mount, numeric supplementary group and host access policy |
| Host is unavailable or resources are missing | `virsh` from the same container environment; existing active pools and networks |

Release checks build and start both images on native runners, test JWT login,
the embedded UI, libvirt's test driver and signing-key persistence across
restart. Real KVM/HVF lifecycle operations and production SSH/TLS connections
need separate integration checks.
