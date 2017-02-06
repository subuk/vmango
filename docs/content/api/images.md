+++
weight = 2
title = "Images"
date = "2017-02-05T16:56:54+03:00"
toc = true

+++

## List

*GET /images/?format=json*

Success http code: 200

Curl example:

    curl "http://vmango.example.org/images/?format=json"

Response

    {
      "Title": "Images",
      "Images": [
        {
          "OS": "Centos-7",
          "Arch": 0,
          "Size": 1317994496,
          "Type": 1,
          "Date": "2017-01-17T23:34:43+01:00",
          "FullName": "Centos-7_amd64_qcow2.img",
          "FullPath": "/srv/images/Centos-7_amd64_qcow2.img",
          "PoolName": "vm-images",
          "Hypervisor": "HV1"
        },
        {
          "OS": "Ubuntu-16.04",
          "Arch": 0,
          "Size": 322502656,
          "Type": 1,
          "Date": "2017-01-25T21:05:17.384080365+01:00",
          "FullName": "Ubuntu-16.04_amd64_qcow2.img",
          "FullPath": "/srv/images/Ubuntu-16.04_amd64_qcow2.img",
          "PoolName": "vm-images",
          "Hypervisor": "HV1"
        }
    }
