+++
weight = 2
title = "Images"
date = "2017-02-05T16:56:54+03:00"
toc = true

+++

## List

*GET /api/images/*

Success http code: 200

Curl example:

    curl "http://vmango.example.org/api/images/"

Response

    {
      "Title": "Images",
      "Images": {
        "LOCAL1": [
          {
            "Id": "Fedora-22_amd64_qcow2.img",
            "OS": "Fedora-22",
            "Arch": "x86_64",
            "Size": 228605952,
            "Type": 1,
            "Date": "2017-01-13T02:59:23.290541839+03:00",
            "PoolName": "vmango-images",
          },
          {
            "Id": "Ubuntu-16.04_amd64_qcow2.img",
            "OS": "Ubuntu-16.04",
            "Arch": "x86_64",
            "Size": 322437120,
            "Type": 1,
            "Date": "2017-01-19T00:18:59.653488713+03:00",
            "PoolName": "vmango-images",
          },
          {
            "Id": "Centos-7_amd64_qcow2.img",
            "OS": "Centos-7",
            "Arch": "x86_64",
            "Size": 893136896,
            "Type": 1,
            "Date": "2016-09-06T12:05:26+03:00",
            "PoolName": "vmango-images",
          },
          {
            "Id": "Cirros-7.1_amd64_qcow2.img",
            "OS": "Cirros-7.1",
            "Arch": "x86_64",
            "Size": 22024192,
            "Type": 1,
            "Date": "2017-01-17T02:00:39.29017579+03:00",
            "PoolName": "vmango-images",
          },
          {
            "Id": "Ubuntu-14.04_amd64_qcow2.img",
            "OS": "Ubuntu-14.04",
            "Arch": "x86_64",
            "Size": 261754880,
            "Type": 1,
            "Date": "2017-01-19T23:55:40.284055191+03:00",
            "PoolName": "vmango-images",
          }
        ]
      }
    }

