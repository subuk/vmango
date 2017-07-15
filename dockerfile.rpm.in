RUN yum install -y epel-release rpmdevtools tar gzip
RUN yum install -y make fakeroot
RUN yum install -y wget

RUN wget https://storage.googleapis.com/golang/go1.8.1.linux-amd64.tar.gz
RUN tar -xf go1.8.1.linux-amd64.tar.gz

ADD . /tmp/buildd/vmango/
RUN cd /tmp/buildd/vmango/rpm/ && make sources
RUN rm -f /etc/yum.repos.d/CentOS-Sources.repo
RUN yum-builddep -y /tmp/buildd/vmango/rpm/vmango.spec

ENV HOME /home/builder
RUN useradd -m -s /bin/bash -d $HOME builder
USER builder

ENV PATH /go/bin:$PATH
ENV GOROOT /go

RUN rpmdev-setuptree
RUN rmdir $HOME/rpmbuild/SOURCES
RUN ln -s /tmp/buildd/vmango/rpm $HOME/rpmbuild/SOURCES
RUN fakeroot rpmbuild -ba /tmp/buildd/vmango/rpm/vmango.spec

USER root

RUN mkdir /packages
RUN find $HOME/rpmbuild/RPMS $HOME/rpmbuild/SRPMS -type f |xargs -I{} -n1 cp {} /packages
