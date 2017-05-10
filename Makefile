GOPATH = $(CURDIR)/vendor:$(CURDIR)
GO = GOPATH=$(GOPATH) go
SOURCES = $(shell find src/ -name *.go) src/vmango/web/assets.go
ASSETS = $(shell find templates/ static/)
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

package-deb-%:
	echo "FROM $*" > "dockerfile.build.$*"
	cat dockerfile.deb.in >> "dockerfile.build.$*"
	docker build -f "dockerfile.build.$*" -t "vmango-build-$*" .
	rm -rf "native-packages/$*"
	mkdir -p "native-packages/$*"
	docker run --rm "vmango-build-$*" /bin/bash -c 'tar -C /packages -cf - .' | tar -C "./native-packages/$*" -xf -

package-rpm-%:
	echo "FROM $*" > "dockerfile.build.$*"
	cat dockerfile.rpm.in >> "dockerfile.build.$*"
	docker build -f "dockerfile.build.$*" -t "vmango-build-$*" .
	rm -rf "native-packages/$*"
	mkdir -p "native-packages/$*"
	docker run --rm "vmango-build-$*" /bin/bash -c 'tar -C /packages -cf - .' | tar -C "./native-packages/$*" -xf -

package-all: package-deb-ubuntu\:14.04 package-deb-ubuntu\:16.04 package-deb-debian\:8 package-rpm-centos\:7

clean:
	rm -rf bin/ pkg/ vendor/pkg/ vendor/bin pkg/ src/vmango/web/assets.go dockerfile.build.*
	make -C docs clean
