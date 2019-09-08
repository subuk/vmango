DEB_NAME = vmango
DEB_VERSION = $(VERSION)
DEB_TOPDIR = /tmp/debian-build-root
DEB_OUTDIR = .
DEB_ARCH = $(shell dpkg --print-architecture)
DEB_RELEASE = $(shell git describe --tags 2>/dev/null | awk -F- '{print $$2"."$$3}')
DEB_DISTRIBUTION = unstable

ifeq ($(DEB_RELEASE),.)
       DEB_RELEASE = 1
endif
ifeq ($(DEB_RELEASE),)
       DEB_RELEASE = 1
endif

# Signing:
#   gpg --list-keys --keyid-format=short
#   debsign -k $ID vmango_$version_source.changes
#
sdeb: $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_source.changes $(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_source.buildinfo $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).dsc $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).debian.tar.xz $(DEB_OUTDIR)/vmango_$(VERSION).orig.tar.gz
$(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_source.changes $(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_source.buildinfo $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).dsc $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).debian.tar.xz $(DEB_OUTDIR)/vmango_$(VERSION).orig.tar.gz: vmango-$(VERSION).tar.gz debian/* Makefile.DEB.mk
	mk-build-deps -t 'apt-get -y' --remove --install debian/control
	mkdir -p $(DEB_TOPDIR)
	cp vmango-$(VERSION).tar.gz $(DEB_TOPDIR)/vmango_$(DEB_VERSION).orig.tar.gz

	rm -rf $(DEB_TOPDIR)/vmango-$(VERSION)/
	tar -C $(DEB_TOPDIR)/ -xf $(DEB_TOPDIR)/vmango_$(DEB_VERSION).orig.tar.gz
	cp -r debian $(DEB_TOPDIR)/vmango-$(VERSION)/

	cd $(DEB_TOPDIR)/vmango-$(VERSION)/ \
		&& dch -D '$(DEB_DISTRIBUTION)' -m -v $(DEB_VERSION)-$(DEB_RELEASE) 'Local docker source build' \
		&& debuild -S -sa -us -uc

	mkdir -p $(DEB_OUTDIR)
	cp $(DEB_TOPDIR)/$(DEB_NAME)_$(DEB_VERSION).orig.tar.gz $(DEB_OUTDIR)/
	cp $(DEB_TOPDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_source.changes $(DEB_OUTDIR)/
	cp $(DEB_TOPDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_source.buildinfo $(DEB_OUTDIR)/
	cp $(DEB_TOPDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).dsc $(DEB_OUTDIR)/
	cp $(DEB_TOPDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).debian.tar.xz $(DEB_OUTDIR)/
	rm -rf $(DEB_TOPDIR)/vmango-$(VERSION)

deb: $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_$(DEB_ARCH).deb
$(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_$(DEB_ARCH).deb: $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_source.changes $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).dsc $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).debian.tar.xz $(DEB_OUTDIR)/vmango_$(VERSION).orig.tar.gz Makefile.DEB.mk
	mkdir -p $(DEB_TOPDIR)/
	cp $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_source.changes $(DEB_TOPDIR)/
	cp $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).dsc $(DEB_TOPDIR)/
	cp $(DEB_OUTDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).debian.tar.xz $(DEB_TOPDIR)/
	cp $(DEB_OUTDIR)/vmango_$(VERSION).orig.tar.gz $(DEB_TOPDIR)/

	rm -rf $(DEB_TOPDIR)/vmango-$(VERSION)
	cd $(DEB_TOPDIR)/ && dpkg-source -x $(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE).dsc
	cd $(DEB_TOPDIR)/vmango-$(DEB_VERSION)/ && mk-build-deps -t 'apt-get -y' --remove --install debian/control
	cd $(DEB_TOPDIR)/vmango-$(DEB_VERSION)/ && DEB_BUILD_OPTIONS=noddebs debuild -us -uc

	cp $(DEB_TOPDIR)/$(DEB_NAME)_$(DEB_VERSION)-$(DEB_RELEASE)_$(DEB_ARCH).deb $(DEB_OUTDIR)/
