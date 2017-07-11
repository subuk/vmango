NAME = vmango
VERSION = $(shell cat vmango.spec.in |grep '^%changelog$$' -A1 |awk '{print $$NF}' |tail -1 |cut -d '-' -f 1)
RELEASE = $(shell cat vmango.spec.in |grep '^%changelog$$' -A1 |awk '{print $$NF}' |tail -1 |cut -d '-' -f 2)
TAR = tar

specfile: $(NAME).spec.in
	sed -e "s/@@_VERSION_@@/$(VERSION)/g" -e "s/@@_RELEASE_@@/$(RELEASE)/g" $(NAME).spec.in > $(NAME).spec

sources: specfile
	$(TAR) -C ../ --exclude=rpm/*.tar.gz --exclude=native-packages --exclude=.git --exclude debian --transform "s,^\.,$(NAME)-$(VERSION)," -czf $(NAME)-$(VERSION).tar.gz .
	spectool -g vmango.spec

clean:
	rm -f $(NAME).spec $(NAME)-$(VERSION).tar.gz
