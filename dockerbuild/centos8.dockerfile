FROM centos:8

RUN yum install -y rpmdevtools make epel-release git dnf-utils
RUN echo '%_topdir /tmp/buildd' > ~/.rpmmacros && rpmdev-setuptree
