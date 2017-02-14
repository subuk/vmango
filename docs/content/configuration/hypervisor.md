+++
weight = 10
title = "Livirt Hypervisor"
date = "2017-02-09T23:36:17+03:00"
toc = true
+++

{{% notice tip %}}
Vmango doesn't require any database and store all information directly in libvirt's domain definitions, which implies that you can change domain configuration or even create new domains with other tools (virsh, virt-manager, ...), but please read [machine templates]({{% ref "#machine-templates" %}}) section about conventions.
{{% /notice %}}

In short, to configure new hypervisor you need to:

* Install libvirt and qemu-kvm on it
* Define network
* Define images storage pool
* Define root volumes storage pool
* Download or create machine images
* Customize machine and volume templates if needed
* Add connection parameters to Vmango config file

## Libvirt installation

First of all, you need to install libvirt >=0.10 and qemu-kvm. The following distributions have it in default repositories:

* CentOS 6+
* Ubuntu14.04+
* Debian8+

Detailed instructions for your distribution can be found on the internet. Just use parts about basic installation, network and storage configuration detailed below.

## Network

{{% notice info %}}
Due to limitations of DHCP protocol and dnsmasq, you should install [lease-monitor](https://raw.githubusercontent.com/subuk/vmango/master/qemu-hook-lease-monitor.py) libvirt hook (put this file to /etc/libvirt/hooks/qemu, make it executable and restart libvirt), which will remove dhcp lease from dnsmasq leases database after every machine shutdown. This hook should be installed on every server, otherwise ip address detection may not work.
{{% /notice %}}

Vmango fully relies on libvirt networking. Network name for new machines may be specified with [hypervisor.network]({{% ref "vmango.conf.md#hypervisor" %}}) configuration option. You should create network via libvirt and it must has dhcp server (`dhcp` xml element). To create network use:

```shell
    virsh net-define network-definition.xml
    virsh net-start <name>
    virsh net-autostart <name>
```

If you have multiple servers and you need more complex network configurations with vlans/overlay networks, you should look at [openvswitch](http://openvswitch.org/) project. New versions of libvirt have support for it.


### Public IPv4 subnet

If you have a public IP address subnet, purchased from your server provider, you can use it for your machines. In this case use the routed network setup without NAT. Notice you loose one ip address (usually first address of subnet) for routing purposes.

XML example with `203.0.113.0/24` public subnet:

```xml
    <network>
      <name>vmango</name>
      <forward mode='route' />
      <ip address='203.0.113.1' netmask='255.255.255.0'>
        <dhcp>
          <range start='203.0.113.2' end='203.0.113.254'/>
        </dhcp>
      </ip>
    </network>
```

### Small office / Home network

In case you have a small network in office or home, fully controlled by you, you again should use routed mode. But unlike example above with provider's controlled subnet, you also need to configure your router. And again, you loose one ip address for routing, but it should not be a problem.

For example, if your main network is `192.168.0.0/24`, choose additional subnet for virtual machines, which doesn't intersect with your main network, e.g. `192.168.21.0/24` and define it on hypervisor with the following xml:

```xml
    <network>
      <name>vmango</name>
      <forward mode='route' />
      <ip address='192.168.21.1' netmask='255.255.255.0'>
        <dhcp>
          <range start='192.168.21.2' end='192.168.21.254'/>
        </dhcp>
      </ip>
    </network>
```

The last, you need to add static route to your router:

    192.168.21.0/24 via 192.168.0.10

Where 192.168.0.10 is the IP address of hypervisor. It definitly should be in the main (192.168.0.0/24) network. For detailed instruction on how to add a static route, please consult with your router documentation.

### NAT network

If you don't have a routed subnet for your machines, you may use a NAT network. In this case, if you have a service, which should be publically availaible, you should manually configure iptables forwarding rules for it. (you may already have such network configured automatically after installation, it usually has name 'default'. Check it with `virsh net-list` and `virsh net-dumpxml <name>`).

Nat network XML example:

```xml
    <network>
      <name>vmango</name>
      <forward mode="nat"/>
      <ip address="192.168.122.1" netmask="255.255.255.0">
        <dhcp>
          <range start="192.168.122.2" end="192.168.122.254"/>
        </dhcp>
      </ip>
    </network>
```

But, if you plan to use any VPN software (e.g. [OpenVPN](https://openvpn.net/)) to access machines on this server, don't use `<forward mode="nat"/>`, instead use `<forward mode="route"/>` with custom NAT iptables rule added on your server manually. In case of "nat" mode, libvirt adds iptables restrictions that may prevent you from access private network over VPN.

## Storage

You need two pools on each server. One for images and one for machine drives. Image pool should always be a directory, for machine drives you can use directory or LVM volume group.

Storage pool can be defined with

```shell
    virsh pool-define pool-definition.xml
    virsh pool-start <name>
    virsh pool-autostart <name>
```

### Directory pool

Just a directory. Must exists before pool definition.

XML example:

```xml
    <pool type='dir'>
      <name>vmango-images</name>
      <target>
        <path>/var/lib/libvirt/images/vmango-images</path>
        <permissions>
          <mode>0711</mode>
          <owner>0</owner>
          <group>0</group>
        </permissions>
      </target>
    </pool>
```

### LVM pool

LVM volume group must exists before you start to define libvirt pool. If hypervisor's operating system installed on LVM, you can use same volume group for machines.

XML example:

```xml
    <pool type='logical'>
      <name>vmango-vms</name>
      <source>
        <name>vmango</name>
        <format type='lvm2'/>
      </source>
      <target>
        <path>/dev/vmango</path>
      </target>
    </pool>
```

## Machine images

Vmagno machine images are fully compatible with [openstack](http://docs.openstack.org/image-guide/). In short, each image must have [cloud-init](http://cloudinit.readthedocs.io/en/latest/topics/format.html) installed with support for [configdrive-style](http://docs.openstack.org/user-guide/cli-config-drive.html#configuration-drive-contents) metadata. Many linux distributions provide such images, so you can just download and use them as is:

* Ubuntu https://cloud-images.ubuntu.com/
* Centos https://cloud.centos.org/centos/
* OpenSUSE http://download.opensuse.org/repositories/Cloud:/Images:/ (with Openstack suffix)
* Arch http://linuximages.de/openstack/arch/

Windows also has a similiar utility - [cloudbase-init](https://cloudbase.it/cloudbase-init/).

As mentioned above, images are stored on each server in separate libvirt storage pool ([image_storage_pool]({{% ref "vmango.conf.md#hypervisor" %}}) option). Information about image determined from filename. It must have the following format:

    {OSName}-{OSVersion}-{Arch:amd64|i386}_{ImageFormat:qcow2|raw}.img

For example this image filename:

    Centos-7_amd64_qcow2.img

Will be parsed as:

* OSName: Centos
* OSVersion: 7
* Arch: amd64
* ImageFormat: qcow2

If there any errors during image name parsing, it will be skipped with warning in logs.

## Machine templates

Two templates are used to tell Vmango how to create new machines on hypervisor. The first one is a domain xml template ([hypervisor.vm_template]({{% ref "vmango.conf.md#hypervisor" %}}) option), the second is a machine's root volume template ([hypervisor.volume_template]({{% ref "vmango.conf.md#hypervisor" %}})). By customizing this templates you can use any storage configuration and hypervisor driver supported by libvirt, but currently only QEMU/KVM machines with LVM or qcow2 storages are tested. Feel free to experiment and send pull requests for other configurations.

The exact templates depends on your system configuration, but examples are shipped with deb/rpm packages ([vm.dist.xml.in](https://github.com/subuk/vmango/blob/master/vm.dist.xml.in) and [volume.dist.xml.in](https://github.com/subuk/vmango/blob/master/volume.dist.xml.in)). You should also look at [test fixtures](https://github.com/subuk/vmango/tree/master/fixtures/libvirt). 

When customizing templates, remember that filename of root drive must ends with `_disk` suffix.

Also remember to keep metadata section in domain xml template, web interface may work incorrectly if you omit it. Metadata section uses xml namespace `http://vmango.org/schema/md` and should has the following elements:

* `md>os` - Operating system name and version separated by '-', e.g  `<vmango:os>Ubuntu-14.04</vmango:os>`
* `md>sshkeys>key` - List of injected ssh keys
* `md>userdata` - Userdata specified by user

Example of valid metadata template:

```xml
    <domain type='kvm'>
      ...
      <metadata>
        <vmango:md xmlns:vmango="http://vmango.org/schema/md">
          <vmango:os>{{ .Image.OS }}</vmango:os>
          <vmango:sshkeys>
            {{ range .Machine.SSHKeys }}
            <vmango:key name="{{ .Name }}">{{ .Public }}</vmango:key>
            {{ end }}
          </vmango:sshkeys>
          <vmango:userdata>
            <![CDATA[
              {{ .Machine.Userdata }}
            ]]>
          </vmango:userdata>
        </vmango:md>
      </metadata>
      ...
    </domain>
```

## Vmango connection configuration

The last and the simplest part. You just need to configure Vmango to use this hypervisor. For example:

```
    hypervisor "LOCAL1" {
        url = "qemu+ssh://virt@my-server.example.com/system"
        image_storage_pool = "vmango-images"
        root_storage_pool = "default"
        network = "vmango"
        vm_template = "vm.xml.in"
        volume_template = "volume.xml.in"
    }

```

The full description of config options can be found in corresponding [section]({{% ref "vmango.conf.md#hypervisor" %}}).
