FROM ubuntu:24.04
ENV TZ=UTC
ENV DEBIAN_FRONTEND=noninteractive
RUN rm -f /etc/apt/apt.conf.d/docker-clean
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential dpkg-dev devscripts equivs fakeroot debhelper \
    golang-1.26-go libvirt-dev pkg-config ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
ENV PATH="/usr/lib/go-1.26/bin:${PATH}"
ENV GOTOOLCHAIN=local
