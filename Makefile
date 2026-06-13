.PHONY: build test lint run clean deps fmt vet

BINARY := zeitspur
BUILD_FLAGS := -trimpath

build:
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -o $(BINARY) ./cmd/zeitspur

test:
	go test ./...

lint: fmt vet

run: build
	./$(BINARY) serve

clean:
	rm -f $(BINARY)
	go clean -testcache

deps:
	go mod download

fmt:
	go fmt ./...

vet:
	go vet ./...
