+++
weight = 11
title = "Network"
date = "2017-02-09T01:27:38+03:00"
toc = true
+++

{{% notice info %}}
Due to limitations of DHCP protocol and dnsmasq, you should install [lease-monitor](https://raw.githubusercontent.com/subuk/vmango/master/qemu-hook-lease-monitor.py) (put this file to /etc/libvirt/hooks/qemu and make it executable) libvirt hook, which will remove dhcp lease from dnsmasq leases database after every machine shutdown. This hook should be installed on every hypervisor, otherwise ip address detection may not work.
{{% /notice %}}

Vmango fully relies on libvirt networking. Network name for new machines may be specified with [hypervisor.network]({{% ref "vmango.conf.md#hypervisor" %}}) configuration option.

The only tested network configuration is a routed network (+NAT) with dhcp server managed by libvirt. Yes, it can be harder to setup than bridged network types and requires additional router configuration, but it makes possible to determine ip address of new machine. Vmango sets static dhcp lease via [net-update](https://libvirt.org/html/libvirt-libvirt-network.html#virNetworkUpdate) api call before machine start.

If you need more complex network configurations, you should look at [openvswitch](http://openvswitch.org/) project.


Example of network definition:

    <network>
      <name>vmango</name>
      <forward mode='nat'/>
      <ip address='192.168.124.1' netmask='255.255.255.0'>
        <dhcp>
          <range start='192.168.124.128' end='192.168.124.254'/>
        </dhcp>
      </ip>
    </network>

Links:

* https://wiki.libvirt.org/page/VirtualNetworking
* https://libvirt.org/hooks.html
