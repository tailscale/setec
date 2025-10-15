# Leger Project Makefile

# Version info
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

# Build settings
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0

# ldflags for version embedding
LDFLAGS := -ldflags "\
	-X github.com/leger-labs/leger/internal/version.Version=$(VERSION) \
	-X github.com/leger-labs/leger/internal/version.Commit=$(COMMIT) \
	-X github.com/leger-labs/leger/internal/version.BuildDate=$(BUILD_DATE) \
	-w -s"

# Build flags
BUILD_FLAGS := -trimpath $(LDFLAGS)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: build-leger build-legerd ## Build both binaries

.PHONY: build-leger
build-leger: ## Build leger CLI
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) \
		go build $(BUILD_FLAGS) -o leger ./cmd/leger

.PHONY: build-legerd
build-legerd: ## Build legerd daemon
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) \
		go build $(BUILD_FLAGS) -o legerd ./cmd/legerd

.PHONY: test
test: ## Run tests
	go test -v -race ./...

.PHONY: lint
lint: ## Run linters
	golangci-lint run

.PHONY: clean
clean: ## Clean build artifacts
	rm -f leger legerd *.rpm
	rm -rf dist/

.PHONY: dev
dev: build ## Quick build and test
	./leger --version || echo "leger CLI placeholder"
	./legerd --version

.DEFAULT_GOAL := help
