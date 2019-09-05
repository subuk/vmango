RPM_NAME = vmango
RPM_VERSION = $(VERSION)
RPM_DIST = $(shell rpm --eval '%{dist}')
RPM_TOPDIR = $(shell rpm --eval '%{_topdir}')
RPM_ARCH = $(shell rpm --eval '%{_arch}')
RPM_RELEASE = $(shell git describe --tags 2>/dev/null | awk -F- '{print $$2"."$$3}')
RPM_OUTDIR = .
RPM_SPEC = $(RPM_NAME).spec

ifeq ($(RPM_RELEASE),.)
	RPM_RELEASE = 1
endif

ifeq ($(RPM_RELEASE),)
	RPM_RELEASE = 1
endif


spec: $(RPM_SPEC)
$(RPM_SPEC): $(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE).spec
	cp $(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE).spec $(RPM_SPEC)
$(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE).spec: $(RPM_NAME).spec.in
	sed -e "s/@@_VERSION_@@/$(RPM_VERSION)/g" -e "s/@@_RELEASE_@@/$(RPM_RELEASE)/g" $(RPM_NAME).spec.in > $(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE).spec

srpm: $(RPM_OUTDIR)/$(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE)$(RPM_DIST).src.rpm
$(RPM_OUTDIR)/$(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE)$(RPM_DIST).src.rpm: $(RPM_SPEC) $(RPM_NAME)-$(RPM_VERSION).tar.gz
	mkdir -p $(RPM_OUTDIR)
	cp $(RPM_NAME)-$(RPM_VERSION).tar.gz $(RPM_TOPDIR)/SOURCES
	rpmbuild -bs $(RPM_SPEC)
	mv $(RPM_TOPDIR)/SRPMS/$(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE)$(RPM_DIST).src.rpm $(RPM_OUTDIR)

rpm: $(RPM_OUTDIR)/$(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE)$(RPM_DIST).$(RPM_ARCH).rpm
$(RPM_OUTDIR)/$(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE)$(RPM_DIST).$(RPM_ARCH).rpm: $(RPM_OUTDIR)/$(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE)$(RPM_DIST).src.rpm
	mkdir -p $(RPM_OUTDIR)
	yum-builddep -y $(RPM_OUTDIR)/$(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE)$(RPM_DIST).src.rpm
	rpmbuild --rebuild $(RPM_OUTDIR)/$(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE)$(RPM_DIST).src.rpm
	mv $(RPM_TOPDIR)/RPMS/$(RPM_ARCH)/$(RPM_NAME)-$(RPM_VERSION)-$(RPM_RELEASE)$(RPM_DIST).$(RPM_ARCH).rpm $(RPM_OUTDIR)/

.PHONY: docker-rpm
docker-rpm:
	./dockerbuild.sh centos-7 rpm
