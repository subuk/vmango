#!/bin/bash
set -e

USAGE="Usage: $0 [centos-7|centos-8|ubuntu-1804] <make args...>"
CACHE_DIR=/tmp/vmango-package-build-cache
mkdir -p $CACHE_DIR

case "$1" in
    "centos-8")
        DOCKER_IMAGE=vmango:build_centos8
        YUM_CONFIG=`pwd`/dockerbuild/centos8.yum.conf
        if [[ "Y$(docker images -q $DOCKER_IMAGE)" == "Y" || "Y${FORCE_REBUILD}" != "Y" ]]; then
            docker build -t $DOCKER_IMAGE -f dockerbuild/centos8.dockerfile .
        fi
        shift
        exec docker run --rm -it \
            -v `pwd`:/source \
            -v $CACHE_DIR:/cache \
            -v $YUM_CONFIG:/etc/yum.conf \
            -w /source \
            $DOCKER_IMAGE $@
    ;;
    "centos-7")
        DOCKER_IMAGE=vmango:build_centos7
        YUM_CONFIG=`pwd`/dockerbuild/centos7.yum.conf
        if [[ "Y$(docker images -q $DOCKER_IMAGE)" == "Y" || "Y${FORCE_REBUILD}" != "Y" ]]; then
            docker build -t $DOCKER_IMAGE -f dockerbuild/centos7.dockerfile .
        fi
        shift
        exec docker run --rm -it \
            -v `pwd`:/source \
            -v $CACHE_DIR:/cache \
            -v $YUM_CONFIG:/etc/yum.conf \
            -w /source \
            $DOCKER_IMAGE $@
    ;;
    "ubuntu-1804")
        DOCKER_IMAGE=vmango:build_ubuntu1804
        YUM_CONFIG=`pwd`/dockerbuild/centos7.yum.conf
        if [[ "Y$(docker images -q $DOCKER_IMAGE)" == "Y" || "Y${FORCE_REBUILD}" != "Y" ]]; then
            docker build -t $DOCKER_IMAGE -f dockerbuild/ubuntu1804.dockerfile .
        fi
        mkdir -p $CACHE_DIR/apt
        shift
        exec docker run --rm -it \
            -v `pwd`:/source \
            -v $HOME/.gnupg/:/root/.gnupg \
            -v $CACHE_DIR/apt_ubuntu1804:/var/cache/apt \
            -w /source \
            $DOCKER_IMAGE $@
    ;;
    "debian-10")
        DOCKER_IMAGE=vmango:build_debian10
        YUM_CONFIG=`pwd`/dockerbuild/centos7.yum.conf
        if [[ "Y$(docker images -q $DOCKER_IMAGE)" == "Y" || "Y${FORCE_REBUILD}" != "Y" ]]; then
            docker build -t $DOCKER_IMAGE -f dockerbuild/debian10.dockerfile .
        fi
        mkdir -p $CACHE_DIR/apt
        shift
        exec docker run --rm -it \
            -v `pwd`:/source \
            -v $CACHE_DIR/apt_debian_10:/var/cache/apt \
            -w /source \
            $DOCKER_IMAGE $@
    ;;
    *)
        echo $USAGE
        exit 1
esac
