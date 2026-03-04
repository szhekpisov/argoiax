.PHONY: build test lint clean install

VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS  = -s -w \
           -X github.com/vertrost/ancaeus/cmd.Version=$(VERSION) \
           -X github.com/vertrost/ancaeus/cmd.Commit=$(COMMIT) \
           -X github.com/vertrost/ancaeus/cmd.Date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/ancaeus .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/ dist/

tidy:
	go mod tidy

fmt:
	gofmt -s -w .

vet:
	go vet ./...
