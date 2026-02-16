BINARY := logtap
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

.PHONY: all build build-forwarder test test-integration bench lint fmt clean deps install coverage help

all: deps fmt lint test build ## Run deps, fmt, lint, test, and build

build: ## Build logtap binary
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/logtap

build-forwarder: ## Build logtap-forwarder binary (CGO_ENABLED=0)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/logtap-forwarder ./cmd/logtap-forwarder

test: ## Run tests with race detection and coverage
	go test -race -cover ./...

test-integration: ## Run integration tests (requires Kind cluster + KUBECONFIG)
	LOGTAP_INTEGRATION=1 go test -race -v -timeout 5m ./internal/k8s/ -run TestIntegration

bench: ## Run benchmarks with memory stats
	go test -bench=. -benchmem -run=^$$ ./internal/...

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format code with gofmt and goimports
	gofmt -w .
	goimports -w .

deps: ## Download module dependencies
	go mod download

install: build ## Build and install to GOPATH/bin
	cp bin/$(BINARY) $(shell go env GOPATH)/bin/$(BINARY)

coverage: ## Generate HTML coverage report and open in browser
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	open coverage.html

clean: ## Remove build artifacts and coverage files
	rm -rf bin/ coverage.out coverage.html

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
