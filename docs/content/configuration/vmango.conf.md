+++
weight = 5
title = "vmango.conf"
date = "2017-02-05T17:59:37+03:00"
toc = true
+++

Main configuration file in HCL format: https://github.com/hashicorp/hcl

All vm configuration templates are golang text templates: https://golang.org/pkg/text/template/

## Web server options

**listen** - Web server listen address

**session_secret** - Secret key for session cookie encryption. 

**static_cache** - Static files cache duration, e.g: "1d", "10m", "60s". Used mainly for development.

**ssl_key** - Path to private key file

**ssl_cert** - Path to SSL certificate

## Hypervisor

**hypervisor** - Hypervisor definition, must be used only once.

**hypervisor.url** - Libvirt connection URL.

**hypervisor.image_storage_pool** - Libvirt storage pool name for VM images.

**hypervisor.root_storage_pool** - Libvirt storage pool name for root disks.

**hypervisor.network** - Libvirt network name.

**hypervisor.vm_template** - Path to go template file (relative to vmango.conf) with libvirt domain XML. Used to create a new machine.

Execution context:

* Machine    [VirtualMachine](https://github.com/subuk/vmango/blob/master/src/vmango/models/vm.go#L56) 
* Image  - [Image](https://github.com/subuk/vmango/blob/master/src/vmango/models/image.go#L18)
* Plan       [Plan](https://github.com/subuk/vmango/blob/master/src/vmango/models/plan.go#L3)
* VolumePath string
* Network    string

**hypervisor.volume_template** - Path to go template file (relative to vmango.conf) with libvirt volume XML. It is used to create a new root volume for a new machine.

Execution context:

* Machine    [VirtualMachine](https://github.com/subuk/vmango/blob/master/src/vmango/models/vm.go#L56) 
* Image  - [Image](https://github.com/subuk/vmango/blob/master/src/vmango/models/image.go#L18)
* Plan       [Plan](https://github.com/subuk/vmango/blob/master/src/vmango/models/plan.go#L3)

Example:

    hypervisor {
        url = "qemu:///system"
        image_storage_pool = "vmango-images"
        root_storage_pool = "default"
        network = "vmango"
        vm_template = "vm.xml.in"
        volume_template = "volume.xml.in"
    }


## Authentication users

More about users see in [Users]({{% ref "auth_users.md" %}}) section.

**user** - user definition, may be used multiple times.

**user.password** - password in bcrypt hashed form.

Example:

    user "admin" {
        password = "$2a$10$..."
    }

## Plans

Plans are limiting availaible hardware resources for machines.

**plan** - plan definition, may be used multiple times.

**plan.memory** - memory limit in megabytes.

**plan.cpus** - cpu count.

**disk_size** - disk size in gigabytes.

Example:

    plan "small" {
        memory = 512
        cpus = 1
        disk_size = 5
    }

## SSH Keys

List of availaible ssh keys.

**ssh_key** - key definition, may be used multiple times.

**ssh_key.public** - full public key in ssh format.

Example:

    ssh_key "test" {
        public = "ssh-rsa AAAAB3NzaC1y..."
    }
