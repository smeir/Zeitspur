.PHONY: build test lint run clean deps fmt vet

BINARY := zeitspur
BUILD_FLAGS := -trimpath
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -X main.version=$(VERSION)

build:
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/zeitspur

build-all:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(BUILD_FLAGS) -ldflags '$(LDFLAGS)' -o $(BINARY)-linux-amd64 ./cmd/zeitspur
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(BUILD_FLAGS) -ldflags '$(LDFLAGS)' -o $(BINARY)-linux-arm64 ./cmd/zeitspur

test:
	go test ./...

lint: fmt vet

run: build
	./$(BINARY) serve

clean:
	rm -f $(BINARY) $(BINARY)-linux-*
	go clean -testcache

deps:
	go mod download

fmt:
	go fmt ./...

vet:
	go vet ./...
