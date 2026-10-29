---
title: "Build from source"
description: "Build and run Vmango for local development."
weight: 4
---

Building requires Go 1.26 or newer, a C compiler, pkg-config and the libvirt
development libraries. Creating and reading cloud-init config drives also
requires `mkisofs` and `isoinfo`, provided by genisoimage or compatible tools.

```sh
git clone https://github.com/subuk/vmango.git
cd vmango
make
cp vmango.dist.conf vmango.conf
```

Edit `vmango.conf` for your host and web login before starting the server.
Use `./bin/vmango genpw` to generate a password hash and replace the example
session secret. The [quick-start guide](/docs/start/#connect-a-hypervisor)
explains host connections, storage pools and authentication; use
`./bin/vmango` in place of `./vmango` when following it with a source build.
The [example configuration](https://github.com/subuk/vmango/blob/master/vmango.dist.conf)
documents the available settings.

```sh
./bin/vmango web -c vmango.conf
```

Open [localhost:8080](http://localhost:8080/) once the server is running.
Use HTTPS when exposing the web interface remotely, as described in the
[quick-start guide](/docs/start/#set-a-password-and-start).

Linux with QEMU/KVM is the main target. Local development on macOS is also
possible with QEMU/HVF; see the
[README](https://github.com/subuk/vmango#dependencies-for-macos) for prerequisites.

## Verify changes

Run `make test` to generate the embedded browser assets and run the Go tests.
For real VM lifecycle checks, follow the
[acceptance testing guide](https://github.com/subuk/vmango/blob/master/acceptance/README.md).
Local Go tests do not establish that real hypervisor operations work.

Code, bug reports and contributions live on
[GitHub](https://github.com/subuk/vmango). Include your Vmango version, host
setup and relevant logs when [reporting an issue](https://github.com/subuk/vmango/issues).
