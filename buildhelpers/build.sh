#!/bin/sh
set -e
set -x

SPECFILE="/source/$1"
SOURCES_DIR=$(dirname $SPECFILE)

yum makecache fast
yum install -y rpmdevtools yum-utils fakeroot make gcc

useradd -s /bin/bash builder
chown builder. /cache -R

pushd $SOURCES_DIR
	spectool -g $SPECFILE
	yum-builddep -y $SPECFILE
popd

su builder -c /bin/bash <<EOF
set -e
set -x

mkdir -p /cache/builder-cache-dir
ln -sf /cache/builder-cache-dir ~/.cache

cat > ~/.rpmmacros <<EOT
%_topdir /tmp/buildd
EOT

mkdir -p /tmp/buildd/{BUILD,BUILDROOT,RPMS,SPECS,SRPMS}
ln -s $SOURCES_DIR /tmp/buildd/SOURCES

fakeroot-sysv rpmbuild -ba $SPECFILE
EOF

find /tmp/buildd/RPMS /tmp/buildd/SRPMS -type f |xargs -I{} -n1 cp {} /result
