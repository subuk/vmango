#!/bin/bash
set -e

USAGE="Usage: $0 [centos-7] <make args...>"
CACHE_DIR=/tmp/rpmbuild-cache

case "$1" in
    "centos-7")
        DOCKER_IMAGE=vmango:build_centos7
        YUM_CONFIG=`pwd`/dockerbuild/centos7.yum.conf
        if [[ "Y$(docker images -q $DOCKER_IMAGE)" == "Y" ]]; then
            docker build -t $DOCKER_IMAGE -f dockerbuild/centos7.dockerfile .
        fi
    ;;
    *)
        echo $USAGE
        exit 1
esac
shift

mkdir -p $CACHE_DIR

exec docker run --rm -it \
    -v `pwd`:/source \
    -v $CACHE_DIR:/cache \
    -v $YUM_CONFIG:/etc/yum.conf \
    -w /source \
    $DOCKER_IMAGE $@
