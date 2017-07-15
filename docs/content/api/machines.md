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
      "Title": "Machines",
      "Machines": {
        "LOCAL1": [
          {
            "Id": "4fe884940a384a15b039f94a9bea4934",
            "Name": "asdf",
            "OS": "Ubuntu-16.04",
            "Arch": "x86_64",
            "Memory": 536870912,
            "Cpus": 1,
            "Creator": "admin",
            "ImageId": "Centos-7_amd64_qcow2.img",
            "Ip": {
              "Address": "192.168.124.128",
              "Gateway": "",
              "Netmask": 0,
              "UsedBy": ""
            },
            "HWAddr": "52:54:00:bd:1c:8e",
            "VNCAddr": "127.0.0.1:5900",
            "RootDisk": {
              "Size": 10737418240,
              "Driver": "qemu",
              "Type": "qcow2"
            },
            "SSHKeys": [
              {
                "Name": "test",
                "Public": "ssh-rsa AAAAB3NzaC1yc2EAA..."
              }
            ]
          }
        ]
      }
    }


## Create

*POST /api/machines/*

Success http codes: 201

Parameters:

* Name (string): Machine hostname
* Plan (string): Plan name
* Image (string): Image full name (see [Images]({{< ref "api/images.md#List" >}}))
* SSHKey ([]string): List of key ssh names
* Provider (string): Create machine on this provider
* Userdata (string): Userdata for cloud-init (more about formats in [cloud-init documentation](http://cloudinit.readthedocs.io/en/latest/topics/format.html))

Curl example:

    curl -X POST \
    -d 'Name=testapi&Plan=medium&Image=Centos-7_amd64_qcow2.img&SSHKey=test&SSHKey=home&Provider=LCL1&Userdata=hello' \
    "http://vmango.example.org/api/machines/"

Response example

    {"Message": "Machine testvm created"}

## Details

*GET /api/machines/{provider}/{id}/*

Success http code: 200

Curl example:

    curl "http://vmango.example.org/api/machines/LCL1/4fe884940a384a15b039f94a9bea4934/"

Response example

    {
      "Provider": "LCL1",
      "Title": "Machine asdf",
      "Machine": {
        "Id": "4fe884940a384a15b039f94a9bea4934",
        "Name": "asdf",
        "OS": "Ubuntu-16.04",
        "Arch": "x86_64",
        "Memory": 536870912,
        "Cpus": 1,
        "Creator": "admin",
        "ImageId": "Centos-7_amd64_qcow2.img",        
        "Ip": {
          "Address": "192.168.124.128",
          "Gateway": "",
          "Netmask": 0,
          "UsedBy": ""
        },
        "HWAddr": "52:54:00:bd:1c:8e",
        "VNCAddr": "127.0.0.1:5900",
        "RootDisk": {
          "Size": 10737418240,
          "Driver": "qemu",
          "Type": "qcow2"
        },
        "SSHKeys": [
          {
            "Name": "test",
            "Public": "ssh-rsa AAAAB3NzaC1yc2E..."
          }
        ]
      }
    }

## Delete

*DELETE /api/machines/{provider}/{id}/*

Success http code: 204

No parameters.

Curl example:

    curl -X DELETE "http://vmango.example.org/api/machines/LCL1/deadbeef/"

## Start

*POST /api/machines/{provider}/{id}/start/*

Success http code: 200

No parameters.

Curl example:

    curl -X POST "http://vmango.example.org/api/machines/LCL1/testapi/start/"

## Stop

*POST /api/machines/{provider}/{id}/stop/*

Success http code: 200

No parameters.

Curl example:

    curl -X POST "http://vmango.example.org/api/machines/LCL1/testapi/stop/"

## Reboot

*POST /api/machines/{provider}/{id}/reboot/*

Success http code: 200

No parameters.

Curl example:

    curl -X POST "http://vmango.example.org/api/machines/LCL1/testapi/reboot/"

