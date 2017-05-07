+++
weight = 15
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

**trusted_proxies** - List of trusted ip addresses for X-Forwarded-For or X-Real-IP headers processing.

**ssl_key** - Path to private key file

**ssl_cert** - Path to SSL certificate

## Hypervisor

{{% notice info %}}
If you use SSH connection url, make sure it present in known_hosts file and remote user has permissions to access libvirt socket.
{{% /notice %}}

{{% notice tip %}}
Libvirt socket path can be changed via ?socket=/path/to/libvirt-sock url option.
{{% /notice %}}

**hypervisor** - Hypervisor definition, may be specified multiple times.

**hypervisor.url** - Libvirt connection URL. 

**hypervisor.image_storage_pool** - Libvirt storage pool name for VM images.

**hypervisor.root_storage_pool** - Libvirt storage pool name for root disks.

**hypervisor.ignored_vms** - List of ignored virtual machines names.

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

    hypervisor "LOCAL1" {
        url = "qemu:///system"
        image_storage_pool = "vmango-images"
        root_storage_pool = "default"
        network = "vmango"
        vm_template = "vm.xml.in"
        volume_template = "volume.xml.in"
    }

## AWS Connection

For authentication you can use any supported shared credentials configuration like awscli does or specify access and secret keys directly in the config file:

http://docs.aws.amazon.com/cli/latest/userguide/cli-chap-getting-started.html#config-settings-and-precedence

**aws_connection** - AWS connection definition

**aws_connection.profile** - AWS profile name 

**aws_connection.access_key** - AWS access key 

**aws_connection.secret_key** - AWS secret key 


**aws_connection.region** - AWS region to use

**aws_connection.subnet_id** - AWS VPC subnet id

**aws_connection.security_groups** - AWS VPC security group ids

**aws_connection.planmap** - Mapping from vmango plan to AWS instance type. All defined vmango plans should be mapped to aws instance types.

**aws_connection.image** - AWS AMI definition

**aws_connection.image.os** - OS name for defined aws AMI

**aws_connection.assign_tags** - Add this extra tags for each machine

Example:

    aws_connection "AWS-IRELAND" {
        region = "eu-west-1"
        profile = "somename"
        # access_key = ""
        # secret_key = ""
        planmap {
            "small" = "t2.small"
            "medium" = "t2.medium"
            "large" = "t2.large"
        }
        image "ami-0d063c6b" {
            os = "Centos-7"
        }
        image "ami-a8d2d7ce" {
            os = "Ubuntu-16.04"
        }
        image "ami-3291be54" {
            os = "Debian-8"
        }
        subnet_id = "subnet-4e2fdf16"
        security_groups = ["sg-d30f9eb4"]
        assign_tags = {
            "Env" = "testing"
            "Creator" = "vmango"
        }
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
