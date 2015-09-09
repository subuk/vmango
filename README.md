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

Run app

    ./bin/vmango
