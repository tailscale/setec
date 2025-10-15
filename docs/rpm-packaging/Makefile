# leger Makefile
# Production-ready RPM packaging inspired by Tailscale

# Project configuration
PROJECT := leger
BINARY := leger
MODULE := github.com/yourname/leger

# Version extraction from git (same pattern as Tailscale)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.0.0-dev")
VERSION_SHORT := $(shell echo $(VERSION) | sed 's/^v//')
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

# Build settings
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0

# ldflags for version embedding (Tailscale pattern)
LDFLAGS := -ldflags "\
	-X $(MODULE)/version.Version=$(VERSION) \
	-X $(MODULE)/version.Commit=$(COMMIT) \
	-X $(MODULE)/version.BuildDate=$(BUILD_DATE) \
	-w -s"

# Build flags (same as build_dist.sh)
BUILD_FLAGS := -trimpath $(LDFLAGS)

# Package settings
RPM_ARCH := $(GOARCH)
RPM_FILE := $(BINARY)-$(VERSION_SHORT)-1.$(RPM_ARCH).rpm

.PHONY: help
help: ## Show this help message
	@echo "leger - Podman Quadlet Manager"
	@echo ""
	@echo "Common targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Build variables:"
	@echo "  VERSION:     $(VERSION)"
	@echo "  COMMIT:      $(COMMIT)"
	@echo "  BUILD_DATE:  $(BUILD_DATE)"
	@echo "  GOOS:        $(GOOS)"
	@echo "  GOARCH:      $(GOARCH)"

.PHONY: build
build: ## Build the leger binary
	@echo "Building $(BINARY) $(VERSION) for $(GOOS)/$(GOARCH)..."
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) \
		go build $(BUILD_FLAGS) -o $(BINARY) ./cmd/$(PROJECT)
	@echo "Built: ./$(BINARY)"

.PHONY: build-all
build-all: ## Build for all supported architectures
	@$(MAKE) build GOARCH=amd64
	@mv $(BINARY) $(BINARY)-amd64
	@$(MAKE) build GOARCH=arm64
	@mv $(BINARY) $(BINARY)-arm64
	@echo "Built: $(BINARY)-{amd64,arm64}"

.PHONY: install
install: build ## Install leger to /usr/local/bin
	@echo "Installing $(BINARY) to /usr/local/bin..."
	sudo install -m 755 $(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed. Try: leger --version"

.PHONY: uninstall
uninstall: ## Uninstall leger from /usr/local/bin
	sudo rm -f /usr/local/bin/$(BINARY)

.PHONY: test
test: ## Run tests
	go test -v -race ./...

.PHONY: test-integration
test-integration: ## Run integration tests
	go test -v -tags=integration ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: fmt
fmt: ## Format code
	go fmt ./...
	gofmt -s -w .

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: check
check: fmt vet lint test ## Run all checks

.PHONY: clean
clean: ## Clean build artifacts
	rm -f $(BINARY) $(BINARY)-*
	rm -f *.rpm *.deb
	rm -rf dist/
	rm -f nfpm-build.yaml

.PHONY: rpm
rpm: ## Build RPM package for current GOARCH
	@echo "Building RPM for $(GOARCH)..."
	@$(MAKE) build GOARCH=$(GOARCH)
	@mv $(BINARY) $(BINARY)-$(GOARCH)
	@command -v nfpm >/dev/null 2>&1 || { \
		echo "ERROR: nfpm not found. Install it:"; \
		echo "  go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest"; \
		exit 1; \
	}
	@# Create temporary nfpm config with substituted variables
	@VERSION=$(VERSION_SHORT) ARCH=$(RPM_ARCH) BINARY=$(BINARY)-$(GOARCH) envsubst < nfpm.yaml > nfpm-build.yaml
	@nfpm pkg --packager rpm -f nfpm-build.yaml
	@rm -f nfpm-build.yaml $(BINARY)-$(GOARCH)
	@echo "Created: $(RPM_FILE)"

.PHONY: rpm-all
rpm-all: ## Build RPMs for all architectures
	@$(MAKE) rpm GOARCH=amd64
	@$(MAKE) rpm GOARCH=arm64
	@echo "Created: $(BINARY)-$(VERSION_SHORT)-1.{amd64,arm64}.rpm"

.PHONY: install-rpm
install-rpm: rpm ## Build and install RPM locally
	@echo "Installing RPM..."
	sudo dnf install -y ./$(RPM_FILE)
	@echo "Installed. Configure and start:"
	@echo "  systemctl --user enable --now leger-daemon.service"

.PHONY: uninstall-rpm
uninstall-rpm: ## Uninstall RPM package
	sudo dnf remove -y $(PROJECT)

.PHONY: sign
sign: ## Sign RPM packages with GPG
	@command -v rpmsign >/dev/null 2>&1 || { \
		echo "ERROR: rpmsign not found. Install: sudo dnf install rpm-sign"; \
		exit 1; \
	}
	@if [ -z "$(GPG_KEY)" ]; then \
		echo "ERROR: GPG_KEY not set. Usage: make sign GPG_KEY=your@email.com"; \
		exit 1; \
	fi
	@for rpm in $(BINARY)-*.rpm; do \
		if [ -f "$$rpm" ]; then \
			echo "Signing $$rpm..."; \
			rpmsign --addsign --key-id=$(GPG_KEY) $$rpm; \
		fi; \
	done

.PHONY: verify
verify: ## Verify RPM signatures
	@for rpm in $(BINARY)-*.rpm; do \
		if [ -f "$$rpm" ]; then \
			echo "Verifying $$rpm..."; \
			rpm --checksig $$rpm; \
		fi; \
	done

.PHONY: release
release: ## Create a release tag (Usage: make release VERSION=v1.0.0)
	@if [ -z "$(VERSION)" ]; then \
		echo "ERROR: VERSION not set. Usage: make release VERSION=v1.0.0"; \
		exit 1; \
	fi
	@if git rev-parse "$(VERSION)" >/dev/null 2>&1; then \
		echo "ERROR: Tag $(VERSION) already exists"; \
		exit 1; \
	fi
	@echo "Creating release $(VERSION)..."
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)
	@echo "Release tag created. GitHub Actions will build and publish."

.PHONY: version
version: ## Show version information
	@echo "Version:    $(VERSION)"
	@echo "Short:      $(VERSION_SHORT)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "GOOS:       $(GOOS)"
	@echo "GOARCH:     $(GOARCH)"

.PHONY: dev
dev: build ## Quick development build and run
	./$(BINARY) --version

.PHONY: setup-dev
setup-dev: ## Install development dependencies
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
	@echo "Development tools installed."

.DEFAULT_GOAL := help
