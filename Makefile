export GOFLAGS=-mod=vendor
export GOBIN=$(PWD)/bin/
# Fix for debian sbuild
export GOCACHE=/tmp/.vmango-go-build-cache
GO = go
INSTALL = install
GO_SOURCES = $(shell find . -name '*.go')
ASSETS_SOURCES = $(shell find templates static)
UNAME_S := $(shell uname -s)
TARBALL_SOURCES = $(GO_SOURCES) Makefile README.md vmango.dist.conf vmango.service static/ templates/ vendor/ go.mod go.sum

VERSION = 0.9.0

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

bin/go-bindata:
	$(GO) build -o bin/go-bindata github.com/go-bindata/go-bindata/go-bindata

web/assets_generated.go: bin/go-bindata $(ASSETS_SOURCES)
	bin/go-bindata $(ASSETS_FLAGS) -o web/assets_generated.go -pkg web static/... templates/...

install: bin/vmango vmango.dist.conf
	$(INSTALL) -d -m 0755 $(CONF_DIR)
	$(INSTALL) -d -m 0755 $(BIN_DIR)
	$(INSTALL) -m 0755 bin/vmango $(BIN_DIR)/
	$(INSTALL) -m 0644 vmango.dist.conf $(CONF_DIR)/vmango.conf

tarball: vmango-$(VERSION).tar.gz
vmango-$(VERSION).tar.gz: $(TARBALL_SOURCES)
	$(TAR) --transform "s,,vmango-$(VERSION)/," -czf vmango-$(VERSION).tar.gz $^

.PHONY: vendor
vendor:
	$(GO) mod tidy -v
	$(GO) mod vendor -v

.PHONY: test
test:
	go test

.PHONY: clean
clean:
	rm -rf web/assets_generated.go bin/ *.tar.gz *.tar.hz *.rpm *.deb *_source.buildinfo *_source.changes *_source.ppa.upload *.debian.tar.xz *.dsc

-include Makefile.RPM.mk
-include Makefile.DEB.mk
