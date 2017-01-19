GOPATH = $(CURDIR)/vendor:$(CURDIR)
SOURCES = $(shell find src/ -name *.go)
.PHONY = clean test show-coverage-html show-coverage-text
PACKAGES = $(shell cd src/vmango)

bin/vmango: $(SOURCES)
	GOPATH=$(GOPATH) go get vmango/...
	GOPATH=$(GOPATH) go build -o bin/vmango vmango

test:
	GOPATH=$(GOPATH) go get github.com/stretchr/testify/mock
	GOPATH=$(GOPATH) go get github.com/stretchr/testify/assert
	GOPATH=$(GOPATH) go get github.com/stretchr/testify/suite
	GOPATH=$(GOPATH) go test -race -coverprofile=coverage.out --run=. vmango/handlers

norace-test:
	GOPATH=$(GOPATH) go get github.com/stretchr/testify/mock
	GOPATH=$(GOPATH) go get github.com/stretchr/testify/assert
	GOPATH=$(GOPATH) go get github.com/stretchr/testify/suite
	GOPATH=$(GOPATH) go test --run=. vmango

show-coverage-html:
	GOPATH=$(GOPATH) go tool cover -html=coverage.out

show-coverage-text:
	GOPATH=$(GOPATH) go tool cover -func=coverage.out

test-race:
	GOPATH=$(GOPATH) go test -race vmango

clean:
	rm -rf bin/ vendor/pkg/ vendor/bin pkg/

all: bin/vmango
