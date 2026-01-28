.PHONY: all build test test-integration lint fmt vet clean help

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
GOFMT=gofmt
GOMOD=$(GOCMD) mod
GOLINT=golangci-lint

# Build info
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Directories
PKG_DIR=./pkg/...
INTERNAL_DIR=./internal/...

all: lint test build ## Run lint, test, and build

build: ## Build all packages
	$(GOBUILD) $(PKG_DIR)

test: ## Run unit tests
	$(GOTEST) -v -race -short $(PKG_DIR) $(INTERNAL_DIR)

test-integration: ## Run integration tests (requires Docker)
	$(GOTEST) -v -race -tags=integration $(PKG_DIR)

test-coverage: ## Run tests with coverage
	$(GOTEST) -v -race -coverprofile=coverage.out -covermode=atomic $(PKG_DIR) $(INTERNAL_DIR)
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

lint: ## Run linters
	$(GOLINT) run ./...

fmt: ## Format code
	$(GOFMT) -s -w .

vet: ## Run go vet
	$(GOVET) $(PKG_DIR) $(INTERNAL_DIR)

clean: ## Clean build artifacts
	rm -f coverage.out coverage.html
	$(GOCMD) clean -cache

deps: ## Download dependencies
	$(GOMOD) download
	$(GOMOD) tidy

deps-update: ## Update dependencies
	$(GOMOD) tidy
	$(GOMOD) verify

generate: ## Run go generate
	$(GOCMD) generate $(PKG_DIR)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
