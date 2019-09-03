#!/bin/bash
set -e

USAGE="Usage: $0 [centos-7] <path_to_rpm_spec>"
CACHE_DIR=/tmp/rpmbuild-cache
BUILD_SCRIPT=`pwd`/buildhelpers/build.sh
RESULT_DIR="result/RPMS-$1"

case "$1" in
    "centos-7")
        DOCKER_IMAGE=centos:7
        YUM_CONFIG=`pwd`/buildhelpers/centos7.yum.conf
    ;;
    *)
        echo $USAGE
        exit 1
esac

mkdir -p $RESULT_DIR
mkdir -p $CACHE_DIR

RPMSPEC=$2
if [ -z "$RPMSPEC" ];then
    echo $USAGE
    exit 1
fi

exec docker run --rm -it \
    -v `pwd`:/source \
    -v `pwd`/$RESULT_DIR:/result \
    -v $CACHE_DIR:/cache \
    -v $YUM_CONFIG:/etc/yum.conf \
    -v $BUILD_SCRIPT:/build.sh \
    $DOCKER_IMAGE /build.sh $RPMSPEC
