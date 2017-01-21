# vmango

Vmango is a virtual machines management web interface written using [Go](http://golang.org/).

Current features:

* SSH keys management and injection
* KVM via libvirt
* Digitalocean-style interface
* Support for cloud OS images (with cloud-init installed)
* IP address management

Planned features:

* Backups
* DNS zones management
* Other virtualization platforms
* Multiple virtualization servers
* Elastic IP addresses and port forwarding
* Native packages for linux distributions

Hypervisor requirements:

* Libvirt 0.10+ (centos6+, ubuntu14.04+, debian8+)

## Development hypervisor configuration (Ubuntu 14.04/16.04)

    sudo apt-get install libvirt-dev libvirt-bin qemu-kvm virt-manager qemu-system
    sudo usermod -aG libvirtd [username]
    newgrp libvirtd

Define libvirt network

    virsh net-define network.xml

Define libvirt images storage:
    
    sudo mkdir -p /var/lib/libvirt/images/vmango-images
    virsh pool-define storage-pool-images.xml

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

## User passwords

User passwords stored in config file in hashed form (golang.org/x/crypto/bcrypt). For adding new user or change password for existing, generate a new one with `vmango genpw` utility:

    ./bin/vmango genpw plainsecret

Copy output and insert into config file:
       
    ...
    user "admin" {
        password = "$2a$10$uztHNVBxZ08LBmboJpAqguN4gZSymgmjaJ2xPHKwAqH.ukgaplb96"
    }
    ...

## Development environment

### Ubuntu (local hypervisor)

Install Go 1.7

    cd /usr/local
    sudo wget https://storage.googleapis.com/golang/go1.7.4.linux-amd64.tar.gz
    sudo tar xf go1.7.4.linux-amd64.tar.gz

Compile

    make EXTRA_ASSETS_FLAGS=-debug

Run app:

    ./bin/vmango


### MacOS (remote hypervisor)

Install Go compiler

    brew install go

Install libvirt library

    brew install libvirt

Compile 

    make EXTRA_ASSETS_FLAGS=-debug

Steal server with Ubuntu 14.04 and install libvirt/kvm on it following the instructions above.

Change hypervisor url in config file (vmango.conf) to remote location

    ...
    hypervisor {
        ...
        url = "qemu+ssh://user@host/system"
        ...
    }
    ...

Run app 

    ./bin/vmango 
