ENV DEBIAN_FRONTEND noninteractive
ENV DEBIAN_PRIORITY critical
ENV DEBCONF_NOWARNINGS yes

RUN apt-get update && apt-get install -y wget dpkg-dev cdbs devscripts equivs
RUN wget https://storage.googleapis.com/golang/go1.8.1.linux-amd64.tar.gz
RUN tar -xf go1.8.1.linux-amd64.tar.gz

ADD debian/ /tmp/buildd/vmango/debian/
RUN mk-build-deps -t 'apt-get -y' --remove --install /tmp/buildd/vmango/debian/control

ADD . /tmp/buildd/vmango

ENV HOME /home/builder
RUN useradd -s /bin/bash -d $HOME builder
RUN chown builder. /tmp/buildd -R
USER builder

ENV PATH /go/bin:$PATH
ENV GOROOT /go
RUN cd /tmp/buildd/vmango && dpkg-buildpackage -us -uc

USER root
RUN mkdir -p /packages
RUN mv /tmp/buildd/*.deb /tmp/buildd/*.changes /tmp/buildd/*.dsc /tmp/buildd/*.tar* /packages
