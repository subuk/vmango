+++
weight = 5
title = "Machines"
date = "2017-02-05T15:14:41+03:00"
toc = true
+++

## List

*GET /api/machines/*

Success http code: 200

Curl example:

    curl "http://vmango.example.org/api/machines/"

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

*POST /api/machines/*

Success http codes: 201

Parameters:

* Name (string): Machine hostname
* Plan (string): Plan name
* Image (string): Image full name (see [Images]({{< ref "api/images.md#List" >}}))
* SSHKey ([]string): List of key ssh names
* Hypervisor (string): Create machine on this hypervisor
* Userdata (string): Userdata for cloud-init (more about formats in [cloud-init documentation](http://cloudinit.readthedocs.io/en/latest/topics/format.html))

Curl example:

    curl -X POST \
    -d 'Name=testapi&Plan=medium&Image=Centos-7_amd64_qcow2.img&SSHKey=test&SSHKey=home&Hypervisor=LCL1&Userdata=hello' \
    "http://vmango.example.org/api/machines/"

Response example

    {"Message": "Machine testvm created"}

## Details

*GET /api/machines/{hypervisor}/{name}/*

Success http code: 200

Curl example:

    curl "http://vmango.example.org/api/machines/LCL1/testapi/"

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

*DELETE /api/machines/{hypervisor}/{name}/*

Success http code: 204

No parameters.

Curl example:

    curl -X DELETE "http://vmango.example.org/api/machines/LCL1/testvm/"

## Start

*POST /api/machines/{hypervisor}/{name}/start/*

Success http code: 200

No parameters.

Curl example:

    curl -X POST "http://vmango.example.org/api/machines/LCL1/testapi/start/"

## Stop

*POST /api/machines/{hypervisor}/{name}/stop/*

Success http code: 200

No parameters.

Curl example:

    curl -X POST "http://vmango.example.org/api/machines/LCL1/testapi/stop/"

## Reboot

*POST /api/machines/{hypervisor}/{name}/reboot/*

Success http code: 200

No parameters.

Curl example:

    curl -X POST "http://vmango.example.org/api/machines/LCL1/testapi/reboot/"

