NAME = vmango
VERSION = $(shell cat vmango.spec.in |grep '^%changelog$$' -A1 |awk '{print $$NF}' |tail -1 |cut -d '-' -f 1)
RELEASE = $(shell cat vmango.spec.in |grep '^%changelog$$' -A1 |awk '{print $$NF}' |tail -1 |cut -d '-' -f 2)
TARGET_DISTRO =

default: package

specfile: $(NAME).spec.in
	sed -e "s/@@_VERSION_@@/$(VERSION)/g" -e "s/@@_RELEASE_@@/$(RELEASE)/g" $(NAME).spec.in > $(NAME).spec

sources: specfile
	$(MAKE) -C ../ tarball VERSION=$(VERSION)
	mv ../$(NAME)-$(VERSION).tar.gz .

package: sources
	ctbuild -c $(TARGET_DISTRO).cfg -d $(TARGET_DISTRO)/ -- $(NAME).spec

clean:
	rm -f $(NAME).spec $(NAME)-$(VERSION).tar.gz
