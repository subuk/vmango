export GOFLAGS=-mod=vendor
export GOBIN=$(PWD)/bin/
# Fix for debian sbuild
export GOCACHE=/tmp/.vmango-go-build-cache
GO = go
INSTALL = install
GO_SOURCES = $(shell find . -path './ui/node_modules' -prune -o -path './ui/dist' -prune -o -path './vendor' -prune -o -name '*.go' -print)
UNAME_S := $(shell uname -s)
TARBALL_SOURCES = main.go bootstrap/ compute/ tasks/ auth/ api/ ui/ filesystem/ libvirt/ config/ configdrive/ catalog/ util/ acceptance/ Makefile README.md LICENSE docs/API.md docs/UI-MIGRATION.md docs/TASKS.md docs/CLOUDS.md docs/content/en/docs/images.md docs/content/en/docs/docker.md docs/data/image-sources.json scripts/verify-image-sources.py vmango.dist.conf vmango.quickstart.conf vmango.service vendor/ go.mod go.sum scripts/version.sh scripts/ui-assets.py
UI_PREBUILT ?= 0

# Release workflows can override this before the release tag exists.
.DEFAULT_GOAL := default
VERSION ?= $(shell sh scripts/version.sh)

.PHONY: print-version
print-version:
	@printf '%s\n' '$(VERSION)'

BUILD_LDFLAGS = -X subuk/vmango/api.AppVersion=$(VERSION)

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

.PHONY: FORCE
FORCE:

bin/vmango: FORCE ui $(GO_SOURCES) Makefile go.mod go.sum
	$(GO) build -ldflags='$(BUILD_LDFLAGS)' -o bin/vmango

.PHONY: ui ui-test api-types
ui:
ifeq ($(UI_PREBUILT),1)
	python3 scripts/ui-assets.py verify
else
	@command -v npm >/dev/null || { echo 'Node/npm required: install the version in ui/.nvmrc, or use UI_PREBUILT=1 with a verified source archive.' >&2; exit 1; }
	@if ! cmp -s ui/package-lock.json ui/node_modules/.vmango-lock; then cd ui && npm ci && cp package-lock.json node_modules/.vmango-lock; fi
	@if ! python3 scripts/ui-assets.py verify >/dev/null 2>&1; then cd ui && npm run build; fi
endif

ui-test: ui
	cd ui && npm run api:check && npm test
	PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_ui_assets.py'

api-types:
	UPDATE_API_SPEC=1 $(MAKE) test
	cd ui && npm run api:generate

install: bin/vmango vmango.dist.conf
	$(INSTALL) -d -m 0755 $(CONF_DIR)
	$(INSTALL) -d -m 0755 $(BIN_DIR)
	$(INSTALL) -m 0755 bin/vmango $(BIN_DIR)/
	$(INSTALL) -m 0644 vmango.dist.conf $(CONF_DIR)/vmango.conf

tarball: vmango-$(VERSION).tar.gz
vmango-$(VERSION).tar.gz: ui $(TARBALL_SOURCES)
	@set -eu; metadata=$$(mktemp -d); trap 'rm -rf "$$metadata"' EXIT; \
	printf '%s\n' '$(VERSION)' > "$$metadata/VERSION"; \
	$(TAR) --exclude=ui/node_modules --exclude='ui/*.tsbuildinfo' --transform "s,,vmango-$(VERSION)/," -czf $@ $(TARBALL_SOURCES) -C "$$metadata" VERSION

.PHONY: vendor
vendor:
	$(GO) mod tidy -v
	$(GO) mod vendor -v

.PHONY: test
test: ui
	$(GO) test ./...

.PHONY: clean
clean:
	rm -rf bin/ *.tar.gz *.tar.hz *.rpm *.deb *_source.buildinfo *_source.changes *_source.ppa.upload *.debian.tar.xz *.dsc *.spec
ifneq ($(UI_PREBUILT),1)
	rm -rf ui/dist
endif

ifneq ("$(wildcard /usr/bin/rpm)","")
-include Makefile.RPM.mk
endif
ifneq ("$(wildcard /usr/bin/dpkg)","")
-include Makefile.DEB.mk
endif

# Real local hypervisor; explicitly supply ACCEPTANCE_IMAGE (see acceptance/README.md).
.PHONY: acceptance
acceptance: bin/vmango
	$(GO) test -tags=acceptance -count=1 -v -timeout=15m ./acceptance
