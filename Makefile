GOPATH = $(CURDIR)/vendor:$(CURDIR)
GO = GOPATH=$(GOPATH) go
TAR = tar
NAME = vmango
SOURCES = $(shell find src -name '*.go' -o -name '*.go.in') src/vmango/web/assets.go src/vmango/dal/aws_instance_types.go
ASSETS = $(shell find templates static)
TARBALL_SOURCES = $(SOURCES) $(ASSETS) vendor/src docs debian *.dist.* Makefile aws_instances.json.gz generate-aws-instance-types.go
.PHONY = clean test show-coverage-html show-coverage-text
PACKAGES = $(shell cd src/vmango; find . -type d|sed 's,^./,,' | sed 's,/,@,g' |sed '/^\.$$/d')
TEST_ARGS = -race -tags "unit"
test_coverage_targets = $(addprefix test-coverage-, $(PACKAGES))
EXTRA_ASSETS_FLAGS =
VERSION = $(shell git describe --tags)
DESTDIR =
INSTALL = install

default: bin/vmango

.PHONY: dependencies
vendorize-dependencies:
	$(GO) get -d -t vmango/...
	$(GO) get -d github.com/jteeuwen/go-bindata/...
	$(GO) get -d github.com/stretchr/testify
	python make-vendor-json.py
	find vendor/ -name .git -type d |xargs rm -rf

vendor/bin/go-bindata:
	$(GO) build -o vendor/bin/go-bindata github.com/jteeuwen/go-bindata/go-bindata

src/vmango/web/assets.go: vendor/bin/go-bindata $(ASSETS)
	vendor/bin/go-bindata $(EXTRA_ASSETS_FLAGS) -o src/vmango/web/assets.go -pkg web static/... templates/...

bin/vmango: $(SOURCES)
	$(GO) build -ldflags "-X main.VERSION=${VERSION}" -o bin/vmango vmango

aws_instances.json.gz:
	curl http://www.ec2instances.info/instances.json |gzip > aws_instances.json.gz

src/vmango/dal/aws_instance_types.go: aws_instances.json.gz src/vmango/dal/aws_instance_types.go.in
	$(GO) run generate-aws-instance-types.go > src/vmango/dal/aws_instance_types.go

test-coverage-%:
	$(GO) test $(TEST_ARGS) -coverprofile=coverage.$*.out --run=. vmango/$(shell echo $* | sed 's,@,/,g')

test-coverage: $(test_coverage_targets)

test: lint bin/vmango
	$(GO) test $(TEST_ARGS)  vmango/...

show-coverage-html:
	$(GO) tool cover -html=coverage.out

show-coverage-text:
	$(GO) tool cover -func=coverage.out

lint:
	$(GO) vet vmango/...

install: bin/vmango
	mkdir -p $(DESTDIR)/usr/bin
	mkdir -p $(DESTDIR)/etc/vmango
	$(INSTALL) -m 0755 -o root bin/vmango $(DESTDIR)/usr/bin/vmango
	$(INSTALL) -m 0644 -o root vmango.dist.conf $(DESTDIR)/etc/vmango/vmango.conf
	$(INSTALL) -m 0644 -o root vm.dist.xml.in $(DESTDIR)/etc/vmango/vm.xml.in
	$(INSTALL) -m 0644 -o root volume.dist.xml.in $(DESTDIR)/etc/vmango/volume.xml.in

tarball: $(NAME)-$(VERSION).tar.gz
$(NAME)-$(VERSION).tar.gz: $(TARBALL_SOURCES)
	$(TAR) --transform "s,^,$(NAME)-$(VERSION)/," -czf $(NAME)-$(VERSION).tar.gz $(TARBALL_SOURCES)

package-debian-8-x64:
	$(MAKE) -C deb TARGET_DISTRO=debian-8-x64

package-debian-9-x64:
	$(MAKE) -C deb TARGET_DISTRO=debian-9-x64

package-ubuntu-trusty-x64:
	$(MAKE) -C deb TARGET_DISTRO=ubuntu-trusty-x64

package-ubuntu-xenial-x64:
	$(MAKE) -C deb TARGET_DISTRO=ubuntu-xenial-x64

package-centos-7-x64:
	$(MAKE) -C rpm TARGET_DISTRO=centos-7-x64-epel

package-all: package-debian-9-x64 package-debian-8-x64 package-ubuntu-trusty-x64 package-ubuntu-xenial-x64 package-centos-7-x64

clean:
	rm -rf bin/ pkg/ vendor/pkg/ vendor/bin pkg/ src/vmango/web/assets.go dockerfile.build.* vmango-*.tar.gz
	rm -f src/vmango/dal/aws_instance_types.go
	rm -f $(NAME)-$(VERSION).tar.gz
	make -C docs clean
