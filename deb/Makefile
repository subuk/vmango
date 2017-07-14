NAME = vmango
VERSION = $(shell head -1 ../debian/changelog |cut -d'(' -f2 |cut -d')' -f 1 |cut -d- -f1)
RELEASE = $(shell head -1 ../debian/changelog |cut -d'(' -f2 |cut -d')' -f 1 |cut -d- -f2)
TAR = tar
TARGET_DISTRO =

default: package

$(NAME)_$(VERSION).orig-go.tar.gz:
	wget -O $(NAME)_$(VERSION).orig-go.tar.gz https://storage.googleapis.com/golang/go1.8.1.linux-amd64.tar.gz

sources: $(NAME)_$(VERSION).orig-go.tar.gz
	$(MAKE) -C ../ tarball VERSION=$(VERSION)
	mv ../$(NAME)-$(VERSION).tar.gz ./$(NAME)_$(VERSION).orig.tar.gz
	$(TAR) -xf $(NAME)_$(VERSION).orig.tar.gz
	$(TAR) -C $(NAME)-$(VERSION) -xf $(NAME)_$(VERSION).orig-go.tar.gz

package: sources
	ctbuild -c $(TARGET_DISTRO).cfg -d $(TARGET_DISTRO)/ -- $(NAME)-$(VERSION) '-b -us -uc'

clean:
	rm -rf $(NAME)-$(VERSION)/
	rm -f $(NAME)_$(VERSION).orig.tar.gz $(NAME)_$(VERSION).orig-go.tar.gz
