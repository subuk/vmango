+++
title = "Machines"
date = "2017-02-09T00:41:35+03:00"
toc = true
weight = 5

+++

Vmango aims to be lightweight and simple in setup and operation, so it doesn't require any database and store all information directly in libvirt's domain definitions. All extra data stored in domain [metadata](https://libvirt.org/formatdomain.html#elementsMetadata) section inside `{http://vmango.org/schema/md}md` element.

Each hypervisor has [vm_template]({{% ref "vmango.conf.md#hypervisor" %}}) and [root volume template]({{% ref "vmango.conf.md#hypervisor" %}}). By customizing this templates, in theory, you can use any machine and storage pool configurations supported by libvirt, but Vmango tested only with:

* LVM storage pool
* QCOW2 directory storage pool
* QEMU/KVM virtual machines

All new machines created with Vmango use this templates. You can customize them on your own, but remember to keep metadata section in vm.xml, web interface may work incorrectly if you omit it. Metadata section should have the following elements:

* md>os - Required. Operating system name and version separated by '-', e.g  `<vmango:os>Ubuntu-14.04</vmango:os>`
* md>sshkeys>key - Optional. List of injected ssh keys
* md>userdata - Optional. Userdata, specified by user

Example of valid metadata:

    <domain type='kvm'>
      ...
      <metadata>
        <vmango:md xmlns:vmango="http://vmango.org/schema/md">
          <vmango:os>Debian-8</vmango:os>
          <vmango:sshkeys>
            <vmango:key name="home">ssh-rsa AAAAB3NzaC1yc...</vmango:key>
            <vmango:key name="work">ssh-rsa AAAAB3NzaC1yc...</vmango:key>
          </vmango:sshkeys>
          <vmango:userdata>
            <![CDATA[
              #!/bin/sh
              echo hello > /tmp/ud.txt
            ]]>
          </vmango:userdata>
        </vmango:md>
      </metadata>
      ...
    </domain>


Since vmango doesn't require database, you can change domain configuration or even create new domains with other tools (virsh, virt-manager, ...), but remember about the following conventions:

* Metadata section must be filled
* Root drive filename of machine must ends with `_disk`

Inside machine you can access metadata in [configdrive](http://docs.openstack.org/user-guide/cli-config-drive.html) style - mount cdrom and view files in `<mountpoint>/openstack/latest` directory. There must be at least `meta_data.json` file. If you provide a userdata during machine creation, it will be availaible in `<mountpoint>/openstack/latest/user_data` file.
