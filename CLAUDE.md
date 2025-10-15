# Leger Development Plan: GitHub Issues Sequence (Fork-First Strategy)

The leger repo **IS** a setec fork from day one.

---

## 🎯 Revised Architecture Decision

**Strategy:** Fork setec → Automated refactor via Claude Code → Add Leger components

**Benefits:**
- Complete git history from setec preserved
- Upstream remote already configured
- Single automated refactoring step
- Clear lineage: `tailscale/setec` → `leger-labs/leger`

---

### Phase 1: Automated Setec → Legerd Refactor (Day 1)

#### Issue #1: `chore: refactor setec fork to legerd structure`
**Labels:** `type:chore`, `area:foundation`, `priority:critical`

**Description:**
Automated refactoring of forked setec repository into legerd structure using Claude Code GitHub Action. This preserves setec's git history while establishing Leger's structure.

**Approach:**
Single Claude Code prompt executes all transformations atomically.

---

### 📝 Claude Code Refactoring Prompt

<details>
<summary><b>🤖 Complete Claude Code Prompt (Click to Expand)</b></summary>

```markdown
You are refactoring a fork of tailscale/setec into the legerd daemon for Leger Labs, Inc.

## Context

This repository is a fresh fork of github.com/tailscale/setec. Your task is to:
1. Rename user-facing elements (binary, defaults, docs)
2. Add Leger project structure around the setec core
3. Preserve attribution and upstream compatibility
4. Set up for future Leger CLI development

## Critical Constraints

**DO NOT CHANGE:**
- Package names (keep `package setec`, `package db`, etc.)
- Import paths (keep `github.com/tailscale/setec`)
- API endpoints
- Database format
- Cryptographic code
- Core functionality

**DO CHANGE:**
- Binary name: setec → legerd
- Default paths: setec-dev → legerd-dev
- User-facing documentation
- Add Leger project structure

## Tasks

### Task 1: Rename Setec Binary to Legerd

**1.1 Rename directory:**
```bash
git mv cmd/setec cmd/legerd
```

**1.2 Update `cmd/legerd/setec.go` → `cmd/legerd/legerd.go`:**
```bash
git mv cmd/legerd/setec.go cmd/legerd/legerd.go
```

**1.3 Modify `cmd/legerd/legerd.go`:**

Find and replace:
```go
// Line ~35: Default state directory
- flag.String("state-dir", "setec-dev.state", ...)
+ flag.String("state-dir", "legerd-dev.state", ...)

// Line ~40: Default hostname  
- flag.String("hostname", "setec-dev", ...)
+ flag.String("hostname", "legerd-dev", ...)

// Line ~246: HTTP header (keep for compatibility, but update)
- w.Header().Set("Sec-X-Tailscale-No-Browsers", "setec")
+ w.Header().Set("Sec-X-Tailscale-No-Browsers", "legerd")
```

### Task 2: Create Leger Project Structure

**2.1 Create directory structure:**
```bash
mkdir -p cmd/leger
mkdir -p internal/{version,cli,daemon,podman,config,git,staging,backup,validation}
mkdir -p pkg/types
mkdir -p systemd
mkdir -p config
mkdir -p release/rpm
mkdir -p docs
mkdir -p scripts
mkdir -p .github/workflows
```

**2.2 Create placeholder files:**

Create `cmd/leger/main.go`:
```go
package main

import "fmt"

func main() {
	fmt.Println("leger CLI - coming soon")
}
```

Create `internal/version/version.go`:
```go
package version

var (
	// Set via ldflags during build
	Version   = "development"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func String() string {
	return Version
}

func Long() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, BuildDate)
}
```

Create `.gitkeep` files in empty dirs:
```bash
touch internal/{cli,daemon,podman,config,git,staging,backup,validation}/.gitkeep
touch pkg/types/.gitkeep
```

### Task 3: Update Root Files

**3.1 Update `go.mod`:**
```go
// Keep module name as-is for now
module github.com/tailscale/setec

go 1.23

// ... keep existing dependencies
```

**3.2 Create `NOTICE` file:**
```
This software is a fork of setec by Tailscale Inc.
Original: https://github.com/tailscale/setec
License: BSD-3-Clause

Setec Copyright (c) 2023 Tailscale Inc & AUTHORS
Leger modifications Copyright (c) 2025 Leger Labs, Inc.

legerd is a minimal fork of setec that preserves upstream
compatibility. We regularly merge upstream improvements.

The cmd/legerd directory and all setec packages (client/, server/,
db/, acl/, audit/, types/) are derived from setec and remain
under BSD-3-Clause license.

New Leger components (cmd/leger, internal/*, pkg/*) are licensed
under Apache License 2.0.
```

**3.3 Create new root `LICENSE` file:**
```
Apache License 2.0 (Leger Labs, Inc.)

                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

[... full Apache 2.0 text ...]

================================================================================

This project incorporates setec under BSD-3-Clause license.
See LICENSE.setec for the original setec license.
See NOTICE file for full attribution.
```

**3.4 Preserve original license:**
```bash
git mv LICENSE LICENSE.setec
```

**3.5 Update `README.md`:**

Replace entire file with:
```markdown
# Leger - Podman Quadlet Manager with Secrets

[![CI](https://github.com/leger-labs/leger/actions/workflows/ci.yml/badge.svg)](https://github.com/leger-labs/leger/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Leger manages Podman Quadlets from Git repositories with integrated secrets management via legerd (based on Tailscale's setec).

## Components

- **`leger`** - CLI for managing Podman Quadlets
- **`legerd`** - Secrets management daemon (fork of [tailscale/setec](https://github.com/tailscale/setec))

## Status

🚧 **Pre-release** - Active development towards v0.1.0

## Installation

Coming soon - RPM packages for Fedora 42+

## Architecture

- **Authentication:** Tailscale identity
- **Networking:** Tailscale MagicDNS
- **Secrets:** legerd (setec fork)
- **Containers:** Podman Quadlets (systemd integration)

## Attribution

legerd is a fork of [setec](https://github.com/tailscale/setec) by Tailscale Inc.
See [NOTICE](NOTICE) and [LICENSE.setec](LICENSE.setec) for full attribution.

## License

- Leger components: Apache License 2.0
- legerd (setec fork): BSD-3-Clause (see LICENSE.setec)

## Development

```bash
# Build both binaries
make build

# Run tests
make test

# Build RPM
make rpm
```

See [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) for details.
```

### Task 4: Create Essential Config Files

**4.1 Create `Makefile`:**
```makefile
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
	./leger --version
	./legerd --version

.DEFAULT_GOAL := help
```

**4.2 Create `.gitignore`:**
```gitignore
# Binaries
leger
legerd
*.exe
*.dll
*.so
*.dylib

# Test artifacts
*.test
*.out
coverage.txt

# Build artifacts
dist/
*.rpm
*.deb
*.tar.gz

# State directories
*.state/
/tmp/

# IDE
.vscode/
.idea/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db
```

**4.3 Create `config/leger.yaml`:**
```yaml
# Leger Configuration

# Daemon connection
daemon:
  url: "http://localhost:9090"
  timeout: 10s

# Storage
storage:
  state_dir: "/var/lib/leger"
  backup_dir: "/var/lib/leger/backups"
  staged_dir: "/var/lib/leger/staged"

# Tailscale
tailscale:
  required: true
  verify_identity: true

# Logging
logging:
  level: "info"
  format: "text"
```

### Task 5: Create Systemd Units

**5.1 Create `systemd/legerd.service` (user scope):**
```ini
[Unit]
Description=Leger Secrets Daemon (User)
Documentation=https://docs.leger.run
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=/usr/bin/legerd server
Restart=on-failure
RestartSec=5s

# Security
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/run/user/%U /var/lib/legerd

# Directories
RuntimeDirectory=legerd
StateDirectory=legerd

[Install]
WantedBy=default.target
```

**5.2 Create `systemd/legerd@.service` (system scope):**
```ini
[Unit]
Description=Leger Secrets Daemon (System)
Documentation=https://docs.leger.run
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
User=legerd
Group=legerd
ExecStart=/usr/bin/legerd server
EnvironmentFile=-/etc/default/legerd
Restart=on-failure
RestartSec=5s

# Security
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/run/legerd /var/lib/legerd

# Directories
RuntimeDirectory=legerd
StateDirectory=legerd

[Install]
WantedBy=multi-user.target
```

**5.3 Create `systemd/legerd.default`:**
```bash
# Environment file for legerd (system service)

# Tailscale hostname for legerd
# LEGERD_HOSTNAME=legerd

# State directory
# LEGERD_STATE_DIR=/var/lib/legerd

# Additional options
# LEGERD_OPTS=""
```

### Task 6: Documentation

**6.1 Create `docs/SETEC-SYNC.md`:**
```markdown
# Syncing Upstream Setec Changes

legerd is a minimal fork of [tailscale/setec](https://github.com/tailscale/setec).
This document describes how to merge upstream changes.

## Quarterly Sync Workflow

### 1. Check for Updates

```bash
git fetch upstream
git log HEAD..upstream/main --oneline
```

### 2. Review Changes

```bash
git diff HEAD..upstream/main

# Focus on these directories:
# - cmd/legerd/ (was cmd/setec/)
# - client/
# - server/
# - db/
# - acl/
# - audit/
```

### 3. Merge Upstream

```bash
git checkout main
git merge upstream/main

# Conflicts will likely appear in:
# - cmd/legerd/legerd.go (our renames)
# - README.md (our content)
# - go.mod (if they update dependencies)
```

### 4. Resolve Conflicts

**cmd/legerd/legerd.go:**
- Keep our renames (legerd-dev, legerd-dev.state)
- Accept their functional changes
- Preserve version embedding hook

**README.md:**
- Keep our Leger-focused content
- Note upstream changes in separate upstream-README.md if needed

**go.mod:**
- Accept their dependency updates
- Test thoroughly

### 5. Commit


```bash
git add .
git commit -m "chore(daemon): sync setec upstream to vX.Y.Z

Merged changes from tailscale/setec@<commit-hash>

Changes:
- [list key changes]

Conflicts resolved:
- cmd/legerd/legerd.go: preserved legerd naming
- README.md: kept Leger documentation

All tests passing.
"
```

## When to Merge

✅ **Always merge:**
- Security fixes
- Bug fixes
- Cryptographic library updates
- Database improvements

⚠️ **Review carefully:**
- API changes
- New features
- Breaking changes
- Dependency updates

❌ **Skip or defer:**
- Major refactoring (wait for stable release)
- Features unrelated to Leger's use case
- Changes that conflict with Leger architecture

## Upstream Monitoring

**Subscribe to:**
- https://github.com/tailscale/setec/releases
- https://github.com/tailscale/setec/security/advisories

**Check quarterly:** First week of Jan, Apr, Jul, Oct
```

**6.2 Create `docs/DEVELOPMENT.md`:**
```markdown
# Leger Development Guide

## Repository Structure

```
leger/
├── cmd/
│   ├── leger/          # CLI (Leger-specific)
│   └── legerd/         # Daemon (setec fork)
├── internal/           # Leger-specific internals
├── client/             # setec client library (upstream)
├── server/             # setec server (upstream)
├── db/                 # setec database (upstream)
├── acl/                # setec ACL (upstream)
├── audit/              # setec audit (upstream)
├── types/              # setec types (upstream)
└── setectest/          # setec testing (upstream)
```

## Building

```bash
# Build both binaries
make build

# Build individually
make build-leger
make build-legerd

# Development build with version
make dev
```

## Testing

```bash
# All tests
make test

# Specific packages
go test ./internal/...
go test ./client/...

# With coverage
go test -coverprofile=coverage.txt ./...
```

## Attribution

- `cmd/legerd/` and all `*ec*` packages: BSD-3-Clause (Tailscale)
- `cmd/leger/` and `internal/`: Apache-2.0 (Leger Labs, Inc.)

See [NOTICE](../NOTICE) for full attribution.

### Task 7: GitHub Workflows

**7.1 Create `.github/workflows/semantic-pr.yml`:**
```yaml
name: Semantic PR

on:
  pull_request:
    types: [opened, edited, synchronize, reopened]

permissions:
  pull-requests: read

jobs:
  validate:
    name: Validate PR Title
    runs-on: ubuntu-latest
    steps:
      - uses: amannn/action-semantic-pull-request@v5
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        with:
          types: |
            feat
            fix
            docs
            chore
            test
            refactor
            ci
            perf
          scopes: |
            cli
            daemon
            rpm
            docs
            ci
          requireScope: false
```

**7.2 Create `.github/workflows/ci.yml`:**
```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest

  test:
    name: Test
    runs-on: ubuntu-latest
    strategy:
      matrix:
        go-version: ['1.23']
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go-version }}
      - name: Run tests
        run: go test -v -race ./...

  build:
    name: Build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0  # For git describe
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - name: Build binaries
        run: make build
      - name: Verify versions
        run: |
          ./leger --version || echo "leger not yet implemented"
          ./legerd --version
```

### Task 8: Update Documentation References

**8.1 Update `cmd/legerd/README.md`:**

At the top, add:
```markdown
# legerd

**Note:** This is the legerd daemon directory, a fork of tailscale/setec.
For Leger project documentation, see the [root README](../../README.md).

---

*Original setec documentation follows:*

[... keep existing content ...]
```

**8.2 Create `docs/ARCHITECTURE.md`:**
```markdown
# Leger Architecture

## Overview

Leger consists of two binaries:

### leger (CLI)
- **Purpose:** Manage Podman Quadlets from Git repositories
- **Language:** Go
- **Dependencies:** Tailscale, Podman
- **License:** Apache-2.0

### legerd (Daemon)
- **Purpose:** Secrets management service
- **Based on:** tailscale/setec (BSD-3-Clause)
- **Language:** Go
- **Dependencies:** Tailscale
- **License:** BSD-3-Clause

## Authentication

Both components use Tailscale identity:
- No separate authentication system
- Device must be on authenticated Tailnet
- Identity verified via `tailscale status`

## Secrets Flow

```
User → Web UI → Cloudflare KV (encrypted)
                    ↓
                    ↓ (sync)
                    ↓
Device → leger secrets sync → legerd HTTP API
                                    ↓
                              SQLite (encrypted)
                                    ↓
         leger secrets fetch → legerd returns secret
                                    ↓
                          Written to /run (tmpfs)
                                    ↓
                          Podman reads env file
                                    ↓
                          Container starts with secret
```

## Directory Structure

```
/usr/bin/leger                  # CLI
/usr/bin/legerd                 # Daemon
/etc/leger/config.yaml          # CLI config
/etc/default/legerd             # Daemon env
/var/lib/leger/                 # CLI state
  ├── staged/                   # Staged config updates
  ├── backups/                  # Quadlet backups
  └── manifests/                # Config metadata
/var/lib/legerd/                # Daemon state
  └── secrets.db                # Encrypted secrets
```

## Upstream Relationship

legerd maintains compatibility with setec:
- Same API endpoints
- Same database format
- Same client library
- Can sync upstream quarterly

See [docs/SETEC-SYNC.md](SETEC-SYNC.md) for details.
```

### Task 9: Create Changelog

**9.1 Create `CHANGELOG.md`:**
```markdown
# Changelog

All notable changes to Leger will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Initial fork of tailscale/setec as legerd daemon
- Leger CLI skeleton structure
- Project infrastructure (Makefile, CI, systemd units)
- Documentation for development and upstream syncing

### Changed
- Renamed setec binary to legerd
- Updated default paths (setec-dev → legerd-dev)
- Reorganized repository for monorepo structure

## [0.1.0] - TBD

Initial release - coming soon
```

## Summary of Changes

This refactoring accomplishes:

✅ **Binary renamed:** `cmd/setec/` → `cmd/legerd/`  
✅ **Defaults updated:** All "setec-dev" → "legerd-dev"  
✅ **Leger structure added:** `cmd/leger/`, `internal/*`, `pkg/*`  
✅ **Attribution preserved:** NOTICE, LICENSE.setec  
✅ **Dual licensing:** Apache-2.0 (Leger) + BSD-3-Clause (setec)  
✅ **Build system:** Makefile, version stamping ready  
✅ **CI/CD foundation:** GitHub workflows configured  
✅ **Documentation:** Development guide, sync process  
✅ **Systemd units:** User and system service files  

## Files Modified

**Moved:**
- `cmd/setec/` → `cmd/legerd/`
- `cmd/setec/setec.go` → `cmd/legerd/legerd.go`
- `LICENSE` → `LICENSE.setec`

**Modified:**
- `cmd/legerd/legerd.go` (renamed defaults)
- `README.md` (complete rewrite)
- `go.mod` (kept as-is for now)

**Created:**
- `NOTICE`
- `LICENSE` (Apache-2.0)
- `Makefile`
- `.gitignore`
- `CHANGELOG.md`
- `cmd/leger/main.go`
- `internal/version/version.go`
- `config/leger.yaml`
- `systemd/*.service`
- `docs/*.md`
- `.github/workflows/*.yml`

## Commit Message

Use this exact commit message:

```
chore: refactor setec fork into leger monorepo structure

This commit transforms the forked tailscale/setec repository into
the Leger project monorepo while preserving git history and upstream
compatibility.

Changes:
- Rename binary: cmd/setec → cmd/legerd
- Update defaults: setec-dev → legerd-dev  
- Add Leger CLI skeleton: cmd/leger/
- Add internal packages: internal/version, etc.
- Add project infrastructure: Makefile, systemd units, CI
- Create dual licensing: Apache-2.0 (Leger) + BSD-3-Clause (setec)
- Preserve attribution: NOTICE, LICENSE.setec
- Document upstream sync: docs/SETEC-SYNC.md

Repository structure:
- cmd/legerd/ - secrets daemon (setec fork, BSD-3-Clause)
- cmd/leger/ - CLI (new, Apache-2.0)
- internal/ - Leger internals (new, Apache-2.0)
- client/, server/, db/, etc. - setec packages (BSD-3-Clause)

All setec tests passing. Ready for Leger development.

Original-Source: github.com/tailscale/setec
Fork-Date: 2025-01-XX
Upstream-Commit: <current setec commit hash>

Issue: #1
```
```

</details>

---

### Acceptance Criteria for Issue #1

After Claude Code completes the refactoring:

**Structure:**
- [ ] `cmd/legerd/legerd.go` exists (renamed from setec.go)
- [ ] `cmd/leger/main.go` exists (placeholder)
- [ ] `internal/version/version.go` exists
- [ ] All systemd units created
- [ ] Makefile present and functional
- [ ] Attribution files present (NOTICE, LICENSE, LICENSE.setec)

**Build:**
- [ ] `make build` succeeds
- [ ] `./legerd --version` shows "legerd" not "setec"
- [ ] `./leger --version` runs (placeholder message OK)

**Tests:**
- [ ] All upstream setec tests pass: `go test ./...`
- [ ] No test failures introduced

**Git:**
- [ ] Git history preserved from setec
- [ ] Single commit with all changes
- [ ] Upstream remote configured

**Documentation:**
- [ ] README.md updated with Leger content
- [ ] docs/SETEC-SYNC.md explains upstream sync
- [ ] docs/DEVELOPMENT.md has build instructions
- [ ] docs/ARCHITECTURE.md explains structure

**Manual Verification:**
```bash
# Run after Claude Code completes
make build
./leger --version
./legerd --version
go test ./...
git log --oneline | head -5
git remote -v
```


