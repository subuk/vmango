export GOFLAGS=-mod=vendor
export GOBIN=$(PWD)/bin/
GO = go
INSTALL = install
GO_SOURCES = $(shell find . -name '*.go')
ASSETS_SOURCES = $(shell find templates static)
UNAME_S := $(shell uname -s)
TARBALL_SOURCES = $(GO_SOURCES) Makefile README.md vmango.dist.conf vmango.service vendor/

RPM_NAME = vmango
VERSION = 0.8.0
RELEASE = 1

BUILD_LDFLAGS = -X subuk/vmango/web.AppVersion=$(VERSION)

ifeq ($(UNAME_S),Darwin)
	TAR = gtar
else
	TAR = tar
endif

DESTDIR =
PREFIX = /usr
CONF_DIR = $(DESTDIR)/etc
BIN_DIR = $(DESTDIR)/$(PREFIX)/bin

default: bin/vmango

bin/vmango: $(GO_SOURCES) web/assets_generated.go Makefile
	$(GO) build -ldflags='$(BUILD_LDFLAGS)' -o bin/vmango

.PHONY: vendor
vendor:
	$(GO) mod tidy -v
	$(GO) mod vendor -v

test:
	go test vmango/...

bin/go-bindata:
	$(GO) install github.com/go-bindata/go-bindata/go-bindata

web/assets_generated.go: bin/go-bindata $(ASSETS_SOURCES)
	bin/go-bindata $(ASSETS_FLAGS) -o web/assets_generated.go -pkg web static/... templates/...

install: bin/vmango vmango.dist.conf
	$(INSTALL) -d -m 0755 $(CONF_DIR)
	$(INSTALL) -d -m 0755 $(BIN_DIR)
	$(INSTALL) -m 0755 bin/vmango $(BIN_DIR)/
	$(INSTALL) -m 0644 vmango.dist.conf $(CONF_DIR)/vmango.conf

.PHONY: spec
spec: $(RPM_NAME).spec.in
	sed -e "s/@@_VERSION_@@/$(VERSION)/g" -e "s/@@_RELEASE_@@/$(RELEASE)/g" $(RPM_NAME).spec.in > $(RPM_NAME).spec

.PHONY: apparchive
apparchive: $(TARBALL_SOURCES) RELEASE_BUILD_COMMIT.txt
	$(TAR) --transform "s,,$(RPM_NAME)-$(VERSION)/," -czf $(RPM_NAME)-$(VERSION).tar.gz $^

.PHONY: rpm
rpm: spec apparchive
	./build-rpm.sh centos-7 $(RPM_NAME).spec

clean:
	rm -rf web/assets_generated.go result/ bin/ $(RPM_NAME).spec $(RPM_NAME)-*.tar.gz
	$(GO) clean -testcache
