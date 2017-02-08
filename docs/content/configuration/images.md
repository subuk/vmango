+++
toc = true
weight = 10
title = "Images"
date = "2017-02-09T00:26:13+03:00"
+++

Currently images are stored on each server in separate libvirt storage pool (see [hypervisor.image_storage_pool]({{% ref "vmango.conf.md#hypervisor" %}}) config option).
Information about image determined from filename. It must have the following format:

    {OSName}-{OSVersion}-{Arch:amd64|i386}_{ImageFormat:qcow2|raw}.img

For example this image filename:

    Centos-7_amd64_qcow2.img

Will be parsed as:

* OSName: Centos
* OSVersion: 7
* Arch: amd64
* ImageFormat: qcow2

If there any errors during image name parsing, it will be skipped with warning in logs.
