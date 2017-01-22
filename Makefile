GOPATH = $(CURDIR)/vendor:$(CURDIR)
GO = GOPATH=$(GOPATH) go
SOURCES = $(shell find src/ -name *.go) src/vmango/web/assets.go Makefile
ASSETS = $(shell find templates/ static/)
.PHONY = clean test show-coverage-html show-coverage-text
PACKAGES = $(shell cd src/vmango; find -type d|sed 's,^./,,' | sed 's,/,@,g' |sed '/^\.$$/d')
TEST_ARGS = -race
test_coverage_targets = $(addprefix test-coverage-, $(PACKAGES))
EXTRA_ASSETS_FLAGS =
VERSION = "$(shell git describe --tags)"

default: bin/vmango

debug:
	@echo $(test_coverage_targets)

vendor/bin/go-bindata:
	$(GO) get github.com/jteeuwen/go-bindata/...

src/vmango/web/assets.go: vendor/bin/go-bindata $(ASSETS)
	vendor/bin/go-bindata $(EXTRA_ASSETS_FLAGS) -o src/vmango/web/assets.go -pkg web static/... templates/...

bin/vmango: $(SOURCES)
	$(GO) get -d vmango/...
	$(GO) build -ldflags "-w -s -X main.STATIC_VERSION=${VERSION}" -o bin/vmango vmango

test-deps:
	$(GO) get -t vmango/...

test-coverage-%: test-deps
	$(GO) test $(TEST_ARGS) -coverprofile=coverage.$*.out --run=. vmango/$(shell echo $* | sed 's,@,/,g')

test-coverage: $(test_coverage_targets)

test: src/vmango/web/assets.go test-deps
	$(GO) test $(TEST_ARGS)  vmango/...

show-coverage-html:
	$(GO) tool cover -html=coverage.out

show-coverage-text:
	$(GO) tool cover -func=coverage.out

clean:
	rm -rf bin/ vendor/pkg/ vendor/bin pkg/ src/vmango/web/assets.go
