FROM ubuntu:18.04
ENV TZ=UTC
ENV DEBIAN_FRONTEND noninteractive
RUN rm -f /etc/apt/apt.conf.d/docker-clean
RUN apt-get update && apt-get install -y wget dpkg-dev devscripts equivs fakeroot debhelper software-properties-common vim git-build-recipe
RUN add-apt-repository ppa:longsleep/golang-backports
