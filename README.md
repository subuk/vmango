# vmango

Vmango is a virtual machines management web interface written using [Go](http://golang.org/).

Current features:

* SSH keys management and injection
* KVM via libvirt
* Digitalocean-style interface
* Support for cloud OS images (with cloud-init installed)
* IP address management

Hypervisor server requirements:

* Libvirt 0.10+ (centos6+, ubuntu14.04+, debian8+)
* Routed network with libvirt managed dhcp server. Bridged networks not supported due to impossibility to determine machine ip address.

Web interface server requirements:
* Libvirt 1.2.0+ (Ubuntu 14.04+, debian8+, centos7+)

## Installation

Please, set session_secret in /etc/vmango/vmango.conf after installation. It blank by default, so service won't start.

To start service, use:

    sudo service vmango start

To view logs via journalctl you can use the following command:

    journalctl -t vmango -o cat


### Ubuntu 14.04/16.04

    echo deb https://dl.vmango.org/ubuntu $(lsb_release -c -s) main |sudo tee /etc/apt/sources.list.d/vmango.list
    wget -O- https://dl.vmango.org/repo.key | sudo apt-key add -
    sudo apt-get install apt-transport-https
    sudo apt-get update
    sudo apt-get install vmango

### Debian 8

    echo deb https://dl.vmango.org/debian jessie main |sudo tee /etc/apt/sources.list.d/vmango.list
    wget -O- https://dl.vmango.org/repo.key | sudo apt-key add -
    sudo apt-get install apt-transport-https
    sudo apt-get update
    sudo apt-get install vmango

### CentOS 7

    sudo wget -O /etc/yum.repos.d/vmango.repo https://dl.vmango.org/centos/el7/vmango.repo
    sudo yum install vmango

## Conventions about VM configurations

* Root disk of machine has suffix '_disk'
* Machine has only one network interface

## User passwords

User passwords stored in config file in hashed form (golang.org/x/crypto/bcrypt). For adding new user or change password for existing, generate a new one with `vmango genpw` utility:

    ./bin/vmango genpw plainsecret

Copy output and insert into config file:
       
    ...
    user "admin" {
        password = "$2a$10$uztHNVBxZ08LBmboJpAqguN4gZSymgmjaJ2xPHKwAqH.ukgaplb96"
    }
    ...

## Hypervisor configuration (Ubuntu 14.04/16.04)

    sudo apt-get install libvirt-bin qemu-kvm qemu-system dnsmasq-utils
    sudo usermod -aG libvirtd [username]
    newgrp libvirtd

Define libvirt network

    virsh net-define network.xml
    virsh net-start vmango
    virsh net-autostart vmango

Define libvirt images storage:
    
    sudo mkdir -p /var/lib/libvirt/images/vmango-images
    virsh pool-define storage-pool-images.xml
    virsh pool-start vmango-images
    virsh pool-autostart vmango-images

Install dhcp lease monitor hook (symlink doesn't work due to Apparmor restrictions):
    
    sudo cp qemu-hook-lease-monitor.py /etc/libvirt/hooks/qemu

Download vm images (file names matter!)

    wget -O- http://cloud.centos.org/centos/7/images/CentOS-7-x86_64-GenericCloud.qcow2.xz |unxz - > Centos-7_amd64_qcow2.img
    wget -O- https://cloud-images.ubuntu.com/xenial/current/xenial-server-cloudimg-amd64-disk1.img  > Ubuntu-16.04_amd64_qcow2.img
    sudo chown root. *_qcow2.img
    sudo mv Centos-7_amd64_qcow2.img /var/lib/libvirt/images/vmango-images/
    sudo mv Ubuntu-16.04_amd64_qcow2.img /var/lib/libvirt/images/vmango-images/
    virsh pool-refresh vmango-images

If your processor doesn't support hardware acceleration, change type from "kvm" to "qemu" in the first line of vm.xml.in (or you will get an error during first machine creation):

    <domain type='qemu'> 

## Development environment

### Dependencies for Ubuntu 14.04+

Install libvirt and kvm

    sudo apt-get install libvirt-dev libvirt-bin qemu-kvm virt-manager qemu-system dnsmasq-utils genisoimage
    sudo usermod -aG libvirtd [username]
    newgrp libvirtd

Install Go 1.7

    cd /usr/local
    sudo wget https://storage.googleapis.com/golang/go1.7.4.linux-amd64.tar.gz
    sudo tar xf go1.7.4.linux-amd64.tar.gz

Configure libvirt as described above.
Now you can use your own computer as hypervisor.

### Dependencies for MacOS

Install Go compiler, libvirt C library and mkisofs util (for configdrive creation)

    brew install go
    brew install libvirt
    brew install dvdrtools

You need a linux hypervisor somewhere in the world, because libvirt doesn't support MacOS.

### Development

Compile for development

    make EXTRA_ASSETS_FLAGS=-debug

Change libvirt url in config if needed

    ...
    hypervisor {
        ...
        url = "qemu+ssh://user@host/system"
        ...
    }
    ...

Run app

    ./bin/vmango

View it on http://localhost:8000

### Run tests

Unit tests

    make test

Libvirt integration tests tests (please, do not run tests on production servers)

    VMANGO_TEST_TYPE=ubuntu_file VMANGO_TEST_LIBVIRT_URI=qemu:///system make test TEST_ARGS='-tags integration'
