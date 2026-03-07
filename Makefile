.PHONY: build test lint ci clean install

VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS  = -s -w \
           -X github.com/vertrost/argoiax/cmd.Version=$(VERSION) \
           -X github.com/vertrost/argoiax/cmd.Commit=$(COMMIT) \
           -X github.com/vertrost/argoiax/cmd.Date=$(DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/argoiax .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test -race -count=1 ./...

lint:
	golangci-lint run ./...

ci: lint test
	go install golang.org/x/tools/cmd/deadcode@latest
	deadcode ./...

clean:
	rm -rf bin/ dist/

tidy:
	go mod tidy

fmt:
	gofmt -s -w .

vet:
	go vet ./...
