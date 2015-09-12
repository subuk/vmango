GOPATH = $(CURDIR)/vendor:$(CURDIR)
SOURCES = $(shell find src/ -name *.go)
.PHONY = clean test show-coverage-html show-coverage-text


bin/vmango: $(SOURCES)
	GOPATH=$(GOPATH) go get vmango/...
	GOPATH=$(GOPATH) go build -o bin/vmango vmango/cmd/vmango

bin/vmango-add-ip: src/vmango/cmd/vmango-add-ip/*.go
	GOPATH=$(GOPATH) go get vmango
	GOPATH=$(GOPATH) go build -o bin/vmango-add-ip vmango/cmd/vmango-add-ip

bin/vmango-add-plan: src/vmango/cmd/vmango-add-plan/*.go
	GOPATH=$(GOPATH) go get vmango
	GOPATH=$(GOPATH) go build -o bin/vmango-add-plan vmango/cmd/vmango-add-plan

test:
	GOPATH=$(GOPATH) go get github.com/stretchr/testify/mock
	GOPATH=$(GOPATH) go get github.com/stretchr/testify/assert
	GOPATH=$(GOPATH) go get github.com/stretchr/testify/suite
	GOPATH=$(GOPATH) go test -race -coverprofile=coverage.out --run=. vmango

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

all: bin/vmango bin/vmango-add-ip bin/vmango-add-plan
