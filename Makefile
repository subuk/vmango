GOPATH = $(CURDIR)/vendor:$(CURDIR)
GO = GOPATH=$(GOPATH) go
SOURCES = $(shell find src/ -name *.go)
.PHONY = clean test show-coverage-html show-coverage-text
PACKAGES = $(shell cd src/vmango; find -type d|sed 's,^./,,' | sed 's,/,@,g' |sed '/^\.$$/d')
TEST_ARGS = -race
test_coverage_targets = $(addprefix test-coverage-, $(PACKAGES))

default: bin/vmango

debug:
	@echo $(test_coverage_targets)

bin/vmango: $(SOURCES)
	$(GO) get vmango/...
	$(GO) build -o bin/vmango vmango

test-deps:
	$(GO) get -t vmango/...

test-coverage-%: test-deps
	$(GO) test $(TEST_ARGS) -coverprofile=coverage.$*.out --run=. vmango/$(shell echo $* | sed 's,@,/,g')

test-coverage: $(test_coverage_targets)

test:
	$(GO) test $(TEST_ARGS)  vmango/...

show-coverage-html:
	$(GO) tool cover -html=coverage.out

show-coverage-text:
	$(GO) tool cover -func=coverage.out

clean:
	rm -rf bin/ vendor/pkg/ vendor/bin pkg/
