+++
weight = 5
title = "Machines"
date = "2017-02-05T15:14:41+03:00"
toc = true
+++

## List

*GET /machines/?format=json*

Success http code: 200

Curl example:

    curl "http://vmango.example.org/machines/?format=json"

Response

    {
        "Machines": [{
            "Name": "test",
            "Memory": 456,
            "Cpus": 1,
            "Ip": {"Address": "1.1.1.1", "Gateway": "", "Netmask": 0, "UsedBy": ""},
            "HWAddr": "hw:hw:hw",
            "Hypervisor": "LCL1",
            "VNCAddr": "vnc",
            "OS": "Ubuntu-14.04",
            "Arch": "x86_64",
            "RootDisk": {
                "Size": 123,
                "Driver": "hello",
                "Type": "wow"
            },
            "SSHKeys": [
                {"Name": "test", "Public": "keykeykey"}
            ]
        }, {
            "Name": "hello",
            "Memory": 67897,
            "Cpus": 4,
            "HWAddr": "xx:xx:xx",
            "Hypervisor": "LCL1",
            "VNCAddr": "VVV",
            "OS": "Centos-7",
            "Arch": "x86_64",
            "Ip": {"Address": "2.2.2.2", "Gateway": "", "Netmask": 0, "UsedBy": ""},
            "RootDisk": {
                "Size": 321,
                "Driver": "ehlo",
                "Type": "www"
            },
            "SSHKeys": [
                {"Name": "test2", "Public": "kekkekkek"}
            ]
        }]
    }

## Create

*POST /machines/add/*

Success http codes: 302

Parameters:

* Name (string): Machine hostname
* Plan (string): Plan name
* Image (string): Image full name (see [Images]({{< ref "api/images.md#List" >}}))
* SSHKey ([]string): List of key ssh names
* Userdata (string): Userdata for cloud-init (more about formats in [cloud-init documentation](http://cloudinit.readthedocs.io/en/latest/topics/format.html))

Curl example:

    curl -X POST \
    -d 'Name=testapi&Plan=medium&Image=Centos-7_amd64_qcow2.img&SSHKey=home' \
    "http://vmango.example.org/machines/add/"


## Details

*GET /machines/{name}/?format=json*

Success http code: 200

Curl example:

    curl "http://vmango.example.org/machines/testapi/?format=json"

Response example

    {
        "Machine": {
            "Name": "test-detail-json",
            "Memory": 456,
            "Cpus": 1,
            "Ip": {"Address": "1.1.1.1", "Gateway": "", "Netmask": 0, "UsedBy": ""},
            "HWAddr": "hw:hw:hw",
            "Hypervisor": "LCL1",
            "VNCAddr": "vnc",
            "OS": "Ubuntu-12.04",
            "Arch": "x86_64",
            "RootDisk": {
                "Size": 123,
                "Driver": "hello",
                "Type": "wow"
            },
            "SSHKeys": [
                {"Name": "test", "Public": "keykeykey"}
            ]
        }
    }

## Delete

*POST /machines/{name}/delete/*

Success http code: 302

No parameters.

Curl example:

    curl -X POST "http://vmango.example.org/machines/add/"

## Start

*POST /machines/{name}/start/*

Success http code: 302

No parameters.

Curl example:

    curl -X POST "http://vmango.example.org/machines/testapi/start/"


## Stop

*POST /machines/{name}/stop/*

Success http code: 302

No parameters.

Curl example:

    curl -X POST "http://vmango.example.org/machines/testapi/stop/"

## Reboot

*POST /machines/{name}/reboot/*

Success http code: 302

No parameters.

Curl example:

    curl -X POST "http://vmango.example.org/machines/testapi/reboot/"

