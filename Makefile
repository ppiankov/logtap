BINARY := logtap
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build build-forwarder test bench lint fmt clean deps

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/logtap

build-forwarder:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/logtap-forwarder ./cmd/logtap-forwarder

test:
	go test -race -cover ./...

bench:
	go test -bench=. -benchmem -run=^$$ ./internal/...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w .

deps:
	go mod download

clean:
	rm -rf bin/
