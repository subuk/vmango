# vmango

Vmango is a virtual machines management web interface written using [Go](http://golang.org/).

The main goal of project is not to provide a hypervisor configuration tool,
because that problem already solved by many configuration management systems
like Ansible or Puppet, but provide a convenient way to manage virtual
machines on existing hypervisors.

Current features:

* SSH keys management and injection
* Volume management
* KVM machines via libvirt
* [Web console](https://streamja.com/LLEA)
* Support for cloud OS images (with cloud-init installed)
* Custom userdata for cloud-init
* Bridged network

## Binary quick start

Download a ready-built archive for **Linux x64**, **Windows x64** or
**macOS arm64** from [GitHub Releases](https://github.com/subuk/vmango/releases),
then follow the [binary quick-start guide](docs/content/en/docs/start.md)
([website](https://vmango.org/docs/start/)). It covers runtime dependencies,
checksums, libvirt connectivity, an administrator password and zero-node
troubleshooting. No Docker or Go compiler is needed. Windows includes libvirt
DLLs for remote hosts; ISO tools are separate.

See [HOWTO-RELEASE.md](HOWTO-RELEASE.md) for CI and the manually triggered
release workflow, its inputs and verification scope.

## Installation

Stable packages are published from tagged releases.

### RHEL 8/9/10 and CentOS Stream 10 (x86_64)

Enable the [release COPR repository](https://copr.fedorainfracloud.org/coprs/subuk/vmango/)
and install the package. On RHEL 8/9 or CentOS Stream 10:

```sh
sudo dnf install -y dnf-plugins-core
sudo dnf copr enable subuk/vmango
sudo dnf install vmango
```

On RHEL 10, enable `sudo dnf copr enable subuk/vmango rhel-10-x86_64` instead:
the default EPEL 10 build uses CentOS Stream and requires a newer libvirt.
Both `epel-*` and `rhel-*` repositories are available for versions 8, 9 and 10.
See the [package guide](https://vmango.org/docs/packages/) for explicit targets.

Edit `/etc/vmango.conf`, then enable the service:

```sh
sudo systemctl enable --now vmango
```

### Ubuntu 22.04, 24.04 and 26.04 LTS (amd64)

Use the [release PPA](https://launchpad.net/~subuk/+archive/ubuntu/vmango):

```sh
sudo add-apt-repository ppa:subuk/vmango
sudo apt-get update
sudo apt-get install vmango
```

Edit `/etc/vmango.conf` before starting the service. The PPA packages target
Ubuntu; they are not Debian distribution builds.

### Debian 12 and 13 (amd64)

Use the Ubuntu 22.04 Jammy package from the release PPA, following the
[Debian package instructions](https://vmango.org/docs/packages/#debian-12-and-13-amd64).
The repository suite must remain `jammy`, not the Debian codename. Package
validation checks installation and startup on both Debian versions.

### Docker (Linux amd64 and arm64)

Follow the [Docker installation guide](docs/content/en/docs/docker.md)
([website](https://vmango.org/docs/docker/)) for setup and troubleshooting.

Release images are published to [Docker Hub](https://hub.docker.com/r/subuk/vmango)
as `subuk/vmango:VERSION`; `latest` tracks the latest stable GitHub release.
Prereleases use version-specific tags. The image includes libvirt clients,
SSH, CA certificates, ISO tools and the embedded frontend. It connects to
existing hypervisors and does not run a libvirt daemon.
Docker selects the matching architecture automatically; explicit
`VERSION-amd64` and `VERSION-arm64` tags are also available.

Copy `vmango.dist.conf` to `vmango.conf`, configure your cloud's host connections,
and add an administrator password hash generated with:

```sh
docker run --rm -it subuk/vmango:latest genpw
```

Keep state paths under `/var/lib/vmango` and `api.listen = ":8080"`. Set
`api.base_url` to your public HTTPS URL and `api.session_secure = true`, then
place an HTTPS reverse proxy in front of the container's HTTP port. Docker
forwards a non-loopback peer address, so the API requires this HTTPS setup even
when the host port is bound to localhost. For remote hypervisors, use a libvirt
URI such as `qemu+ssh://vmango@hypervisor/system` and
mount SSH keys and a verified `known_hosts` file at `/var/lib/vmango/.ssh`.
Bind-mounted files must be readable by container UID/GID `10001:10001`.
The configured SSH account needs permission to use libvirt on the hypervisor.
For a local host's `qemu:///system`, mount its libvirt socket and grant the
container user access to the socket's group.

```sh
docker volume create vmango-data
docker run -d --name vmango --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v "$PWD/vmango.conf:/etc/vmango.conf:ro" \
  -v vmango-data:/var/lib/vmango \
  subuk/vmango:latest
```

Point your HTTPS reverse proxy at `http://127.0.0.1:8080`, preserve `Host` and
browser origin headers, then open the configured public HTTPS URL.
Preserve the data volume across upgrades: it contains signing keys, credentials,
SSH key records and durable task/image/volume metadata. Pin a version tag when
you need controlled upgrades.

## Configure a cloud

Administrators prepare the hypervisors, libvirt connections, active storage pools
and networks outside Vmango. They then define image requirements, instance types,
availability zones, logical networks, storage classes and private host bindings
in the configuration. Each `cloud` contains `host` blocks with the connection
`uri`, backend options, zone and network/storage bindings together. Vmango does
not provision hosts or create their pools and networks.

Follow the [configured-cloud guide](docs/content/en/docs/clouds.md)
([website](https://vmango.org/docs/clouds/)) and the
[managed-image guide](docs/content/en/docs/images.md)
([website](https://vmango.org/docs/images/)). The shipped configuration includes
pinned downloadable images that are prepared lazily on the selected hypervisor;
manual image downloads are not required for that catalog. Administrators may
also register an existing source volume as described in the image guide.

Users select a cloud, image, instance type, availability zone, logical network
and storage class. The scheduler chooses the host. Creation accepts catalog
references, SSH keys and optional cloud-init user data; it does not accept raw
pool names, image paths or image-policy overrides. See the
[API creation example](docs/API.md#create-a-vm) for automation.

## Local run

Building requires Go 1.26 or newer, the libvirt development libraries, Python 3
and Node/npm (use the pinned version in `ui/.nvmrc`). Distribution source archives
include a verified frontend and support `make UI_PREBUILT=1` without Node.

Copy vmango.dist.conf to vmango.conf and change configuration if needed.

Run app

    make && ./bin/vmango web

Signing keys are generated automatically in `~/.vmango` when missing. Override
the paths with `api.jwt_signing_key_file` and `api.console_signing_key_file`. Configure a user with a
password hash generated by `vmango genpw`, then open http://localhost:8080.

The VNC console uses noVNC 1.7 and requires a modern browser. For remote access,
serve Vmango over HTTPS; browser APIs used by noVNC require a secure context
(localhost is also supported).

CSRF protection uses Go's `net/http.CrossOriginProtection`: browser requests
are checked using `Sec-Fetch-Site` and `Origin`, without form tokens. Reverse
proxies must preserve these headers and the public `Host`. Set `api.base_url`
to the public HTTPS URL and enable `api.session_secure` behind a TLS proxy.
Requests without either browser header are treated as non-browser requests;
Bearer authentication is still required for protected resource routes.


### Dependencies for Linux

Install a C compiler, pkg-config and the libvirt development libraries alongside
Go 1.26 or newer, Python 3 and the pinned Node toolchain. For example, on Ubuntu
24.04 the native build and ISO tools are:

```sh
sudo apt-get install build-essential pkg-config libvirt-dev genisoimage
```

Building the application does not configure a hypervisor. Use an existing local
or remote libvirt host and define the cloud catalog and private bindings as
[described above](#configure-a-cloud). See the maintained
[source-build guide](docs/content/en/docs/build.md) for build and verification steps.

### Dependencies for MacOS

Install Go compiler, libvirt C library and mkisofs util (for configdrive creation)

    brew install go
    brew install libvirt
    brew install dvdrtools

For local VMs, install QEMU (`brew install qemu`) and use libvirt's
`qemu:///session` connection with HVF acceleration and a matching guest
architecture. A remote Linux/KVM host is also supported.

See [local acceptance testing](acceptance/README.md) for a real VM lifecycle
test on macOS or Linux. Ordinary tests run with `make test`.


## Documentation development

The homepage uses a standalone HTML template in `docs/layouts/home.html` and
plain CSS in `docs/static/landing/style.css`. It does not load Docsy styles or
JavaScript. Hugo still builds the complete site, with Docsy reserved for the
documentation routes. The homepage is a compact project overview linking to
the documentation. Installation, cloud images and source build instructions
live under `/docs/`.

The documentation in `docs/` uses Docsy 0.18. It requires Hugo Extended
0.163.0 or newer, Node.js 22 or newer, and the native Dart Sass executable
(`sass`) on `PATH`. Install Dart Sass outside the project directory; the
JavaScript-only npm `sass` package does not support Hugo's embedded protocol.
See the [Hugo Sass setup guide](https://gohugo.io/functions/css/sass/).

```sh
cd docs
npm ci
npm run hugo -- server
```

After updating the Docsy module, run `hugo mod npm pack` and `npm install`
to refresh its npm workspace and lockfile. Commit `docs/packages/hugoautogen/`
along with the dependency manifests.

## Build RPM

In an RPM build environment providing Go 1.26 or newer and libvirt-devel:

    make rpm

The legacy CentOS 7/8 Docker images in `dockerbuild/` are not maintained;
use a supported distribution with the required compiler or the COPR builders.

## Build DEB

With Docker for Ubuntu 24.04:

    ./dockerbuild.sh ubuntu-2404 make deb

Locally:

    make deb

DEB builds require `golang-1.26-go`; `debian/rules` selects
`/usr/lib/go-1.26/bin` explicitly. Ubuntu 22.04, 24.04 and 26.04 provide this
compiler through their standard repositories with updates/security enabled.
RPM and DEB builds use vendored modules and `GOTOOLCHAIN=local`, so the compiler
must be installed before building. Both package builds run `make test`.

## Package validation and releases

Maintainer instructions for manual package validation and release publication
are in [HOWTO-RELEASE.md](HOWTO-RELEASE.md). Pushing a branch does not publish
distribution packages.

## Automation API

Vmango provides a versioned JSON API with JWT Bearer authentication, generated
OpenAPI and locally bundled Swagger UI at `/api/docs`. Start with the
[API guide](https://vmango.org/docs/api/) for Swagger access and authentication.
See the [API reference](docs/API.md) for VM/cloud-init examples, durable
asynchronous task semantics and serial/VNC clients with reusable signed browser
links.


## Frontend development and authentication migration

The browser UI is React/TypeScript/Vite under `ui/`, embedded in the executable
with `go:embed`. HTTP transport lives in `api/`; authentication lifecycle rules
and repository contracts live in `auth/`. The old `web/` package, HTML mutation
handlers and go-bindata assets have been removed.

Read [UI and authentication migration](docs/UI-MIGRATION.md) before upgrading an
existing installation. The `web { ... }` configuration block and Basic Auth are
intentionally rejected. Existing resource API URLs are retained.

Use `make ui`, `make test` and `make ui-test` for frontend/Go verification.
See [frontend development](docs/UI-MIGRATION.md#development) for the Vite proxy,
OpenAPI generation and source-archive validation workflow.

Configured clouds, instance types, placement and local disk rules are documented in [docs/CLOUDS.md](docs/CLOUDS.md).
