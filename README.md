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


## Local run

Copy vmango.dist.conf to vmango.conf and change configuration if needed.

Run app

    make && ./bin/vmango

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

Requires docker command.

    make clean rpm
