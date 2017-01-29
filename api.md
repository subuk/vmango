# Vmango API

Description of availaible API.

## Machines

### Create

*POST /machines/add/*

Parameters:
* Name (string): Machine hostname
* Plan (string): Plan name
* Image (string): Image name
* SSHKey ([]string): List of key ssh names

### Delete

*POST /machined/{name}/delete/*

No parameters.

### List

*GET /machines/?format=json*

Response example

    {
        "Machines": [{
            "Name": "test",
            "Memory": 456,
            "Cpus": 1,
            "Ip": {"Address": "1.1.1.1", "Gateway": "", "Netmask": 0, "UsedBy": ""},
            "HWAddr": "hw:hw:hw",
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

### Details

*GET /machines/{name}/?format=json*
        
Response example

    {
        "Machine": {
            "Name": "test-detail-json",
            "Memory": 456,
            "Cpus": 1,
            "Ip": {"Address": "1.1.1.1", "Gateway": "", "Netmask": 0, "UsedBy": ""},
            "HWAddr": "hw:hw:hw",
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
