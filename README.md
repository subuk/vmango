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

## Installation

There are two RPM repositories:

* `vmango` for the latest tagged release, it may be considered stable
* `vmango-devel` contains packages built automatically from the latest commit in master branch

### CentOS 7 and 8

1. Enable copr reposotory, use `subuk/vmango-devel` for the latest version, `subuk/vmango` for stable
1. Install package
1. Edit configuration file `/etc/vmango.conf`
1. Start and enable systemd service `vmango`

```
yum install -y yum-plugin-copr && yum copr -y enable subuk/vmango
yum install -y vmango
systemctl enable --now vmango
```

### Ubuntu 18.04

1. Follow instructions on vmango or vmango-devel ppa page https://launchpad.net/~subuk/+archive/ubuntu/vmango
1. Install package
1. Edit configuration file `/etc/vmango.conf`

```
sudo add-apt-repository ppa:subuk/vmango
sudo apt-get install vmango
```

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

See docs folder in this repo for more complex host configurations.

## Local run

Copy vmango.dist.conf to vmango.conf and change configuration if needed.

Run app

    make && ./bin/vmango web

View it on http://localhost:8080 (login with admin / admin by default)


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

You need a linux hypervisor somewhere in the world, because libvirt doesn't support MacOS.
Make sure to add ?socket option to remote libvirt urls.


## Build RPM

With docker for Centos 7:

    ./dockerbuild.sh centos-7 make rpm

Locally:

    make rpm

## Build DEB

With docker for Ubuntu 18.04:

    ./dockerbuild.sh ubuntu-1804 make deb

Locally:

    make deb
