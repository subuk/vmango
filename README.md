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

### RHEL 8, 9 and 10 (x86_64)

Enable the [release COPR repository](https://copr.fedorainfracloud.org/coprs/subuk/vmango/)
and install the package. For RHEL 8:

```sh
sudo dnf install -y dnf-plugins-core
sudo dnf copr enable subuk/vmango rhel-8-x86_64
sudo dnf install vmango
```

For RHEL 9 or 10, use `rhel-9-x86_64` or `rhel-10-x86_64` instead.
Specify the target explicitly: some versions of the COPR plugin select
`epel-8` or `epel-9` by default, while these packages use the `rhel-*` repositories.

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

## Hypervisor configuration

Install libvirt and qemu-kvm.

Ubuntu:

    sudo apt-get install libvirt-bin qemu-kvm qemu-system

Centos:

    yum install -y libvirt qemu-kvm
    systemctl enable --now libvirtd

Allow your user to access libvirt socket:

    sudo usermod -aG libvirtd [username]
    newgrp libvirtd

Download vm images to default libvirt pool location:

    cd /var/lib/libvirt/images/
    wget https://cloud.centos.org/centos/7/images/CentOS-7-x86_64-GenericCloud-1901.qcow2
    wget https://cloud-images.ubuntu.com/minimal/releases/bionic/release/ubuntu-18.04-minimal-cloudimg-amd64.img

Define default volume pool (if not exists) and start it:

    virsh pool-define-as default dir --target /var/lib/libvirt/images/
    virsh pool-start default
    virsh pool-autostart default


## Local run

Building requires Go 1.26 or newer and the libvirt development libraries.

Copy vmango.dist.conf to vmango.conf and change configuration if needed.

Run app

    make && ./bin/vmango web

View it on http://localhost:8080 (login with admin / admin by default)

The VNC console uses noVNC 1.7 and requires a modern browser. For remote access,
serve Vmango over HTTPS; browser APIs used by noVNC require a secure context
(localhost is also supported).

CSRF protection uses Go's `net/http.CrossOriginProtection`: browser requests
are checked using `Sec-Fetch-Site` and `Origin`, without form tokens. Reverse
proxies must preserve these headers and the public `Host`. Set `web.base_url`
to the public HTTPS URL and enable `web.session_secure` behind a TLS proxy.
Requests without either browser header are treated as non-browser requests;
session authentication is still required for protected routes.


### Dependencies for Ubuntu 14.04+

Install libvirt and kvm

    sudo apt-get install libvirt-dev libvirt-bin qemu-kvm qemu-system genisoimage

Install Go compiler.
Configure libvirt as described above.
Now you can use your own computer as hypervisor.

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
documentation routes. The homepage is a compact project overview with package
links and source build instructions.

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
