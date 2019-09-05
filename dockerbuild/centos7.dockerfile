FROM centos:7

RUN yum install -y rpmdevtools make epel-release git
RUN echo '%_topdir /tmp/buildd' > ~/.rpmmacros && rpmdev-setuptree
