+++
weight = 4
title = "Packages"
date = "2017-02-05T15:36:32+03:00"
toc = true
+++

{{% notice warning %}}
Please set session_secret in /etc/vmango/vmango.conf after installation. It blank by default, so service won't start.
{{% /notice %}}


The simplest way to install.

For all distributions, configuration files located in /etc/vmango, logs managed by init system (systemd or upstart).


#### Ubuntu 14.04/16.04

    echo deb https://dl.vmango.org/ubuntu $(lsb_release -c -s) main |sudo tee /etc/apt/sources.list.d/vmango.list
    wget -O- https://dl.vmango.org/repo.key | sudo apt-key add -
    sudo apt-get install apt-transport-https
    sudo apt-get update
    sudo apt-get install vmango


#### Debian 8

    echo deb https://dl.vmango.org/debian jessie main |sudo tee /etc/apt/sources.list.d/vmango.list
    wget -O- https://dl.vmango.org/repo.key | sudo apt-key add -
    sudo apt-get install apt-transport-https
    sudo apt-get update
    sudo apt-get install vmango

#### CentOS 7

    sudo wget -O /etc/yum.repos.d/vmango.repo https://dl.vmango.org/centos/el7/vmango.repo
    sudo yum install vmango

## Next steps

Configure [hypervisor]({{% ref "configuration/vmango.conf.md#hypervisor" %}}) connection, define [users]({{% ref "configuration/auth_users.md" %}}) and start service:

    sudo service vmango start
