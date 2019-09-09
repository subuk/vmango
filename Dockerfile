FROM centos:8
RUN yum install -y golang libvirt-devel make
COPY . /source
WORKDIR /source
RUN make

FROM centos:8
RUN yum install -y libvirt-libs && yum clean all
COPY --from=0 /source/bin/vmango /usr/bin/vmango
COPY vmango.dist.conf /etc/vmango.conf

RUN useradd -s /bin/bash -m -d /var/lib/vmango vmango
USER vmango
WORKDIR /var/lib/vmango

VOLUME /var/lib/vmango
EXPOSE 8080
CMD /usr/bin/vmango web --config /etc/vmango.conf
