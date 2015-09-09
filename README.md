# vmango

vmango is a virtual machines management web interface written using [Go](http://golang.org/).

# Development installation

For Ubuntu 14.04

Install Go 1.5
    
    cd /usr/local
    sudo wget https://storage.googleapis.com/golang/go1.5.1.linux-amd64.tar.gz
    sudo tar xf go1.5.1.linux-amd64.tar.gz

Install libvirt development package:
    
    sudo apt-get install libvirt-dev

Compile

    make all

Create test ip addresses and plans

    ./bin/vmango-add-plan --name=5 --memory=512 --disk=20 
    ./bin/vmango-add-plan --name=5 --memory=512 --disk=20 --cpus=1
    ./bin/vmango-add-plan --name=10 --memory=1024 --disk=30 --cpus=1
    ./bin/vmango-add-plan --name=20 --memory=2048 --disk=40 --cpus=2
    ./bin/vmango-add-plan --name=40 --memory=4096 --disk=60 --cpus=2
    ./bin/vmango-add-ip --mask=24 --gw=192.168.123.1 --ip=192.168.123.101
    ./bin/vmango-add-ip --mask=24 --gw=192.168.123.1 --ip=192.168.123.102
    ./bin/vmango-add-ip --mask=24 --gw=192.168.123.1 --ip=192.168.123.103
    ./bin/vmango-add-ip --mask=24 --gw=192.168.123.2 --ip=192.168.123.104
    ./bin/vmango-add-ip --mask=24 --gw=192.168.123.1 --ip=192.168.123.105


Run app

    ./bin/vmango
