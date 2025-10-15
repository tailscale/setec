# Tailscale RPM Packaging Analysis for leger

## Executive Summary

Tailscale uses a **simplified, modern approach** to RPM packaging that's perfect for leger to adopt:

1. **nfpm library** (not custom Go tool) - cross-platform package building
2. **Git-based version stamping** - embedded at compile time via ldflags
3. **Simple signing with Ed25519** - though complex for initial releases
4. **Standard RPM scriptlets** - systemd integration via pre/post scripts
5. **GitHub Actions** - automated releases on git tags

**Key Insight**: Tailscale's `cmd/mkpkg` isn't a complex packaging orchestrator - it's a thin wrapper around the `nfpm` Go library. This makes packaging much simpler than traditional rpmbuild approaches.

---

## 1. Build Orchestration

### Tailscale's Approach

**Makefile targets**:
```makefile
build386: ## Build for linux/386
	GOOS=linux GOARCH=386 ./tool/go install ...

buildwindows: ## Build for windows/amd64
	GOOS=windows GOARCH=amd64 ./tool/go install ...
```

**build_dist.sh**:
- Runs `cmd/mkversion` to extract version from git
- Embeds version via ldflags:
  ```bash
  ldflags="-X tailscale.com/version.longStamp=${VERSION_LONG} \
           -X tailscale.com/version.shortStamp=${VERSION_SHORT}"
  ```
- Supports build variants: `--extra-small`, `--box`, `--min`
- Uses `-trimpath` for reproducible builds
- Allows custom tags via `$TAGS` environment variable

### Key Insights

1. **Version stamping**: Extract from git, embed at compile time
2. **Reproducible builds**: `-trimpath` removes local paths
3. **Build variants**: Support different feature sets via build tags
4. **Simple wrapper**: `build_dist.sh` just sets variables and calls `go build`

### leger Adaptation

leger is simpler - single binary, no variants needed initially. We need:

```makefile
# Version extraction from git
VERSION := $(shell git describe --tags --always --dirty)
COMMIT := $(shell git rev-parse HEAD)
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

# ldflags for version embedding
LDFLAGS := -ldflags "-X main.Version=$(VERSION) \
                     -X main.Commit=$(COMMIT) \
                     -X main.BuildDate=$(BUILD_DATE) \
                     -w -s"

build:
	go build $(LDFLAGS) -trimpath -o leger ./cmd/leger

rpm: build
	./tool/go run ./cmd/mkpkg \
		--type rpm \
		--arch $(GOARCH) \
		--version $(VERSION) \
		--files ./leger:/usr/bin/leger \
		--configs ./config/leger.yaml:/etc/leger/config.yaml \
		--out leger-$(VERSION)-$(GOARCH).rpm
```

---

## 2. Packaging Logic (cmd/mkpkg)

### Tailscale's Approach

**NOT a complex Go tool** - it's a simple wrapper around `nfpm`:

```go
// cmd/mkpkg/main.go
import (
	"github.com/goreleaser/nfpm/v2"
	_ "github.com/goreleaser/nfpm/v2/deb"
	_ "github.com/goreleaser/nfpm/v2/rpm"
)

func main() {
	// Parse flags
	out := flag.String("out", "", "output file")
	name := flag.String("name", "tailscale", "package name")
	pkgType := flag.String("type", "deb", "deb or rpm")
	regularFiles := flag.String("files", "", "src:dst,src:dst")
	configFiles := flag.String("configs", "", "src:dst,src:dst")
	
	// Build nfpm.Info struct
	info := nfpm.WithDefaults(&nfpm.Info{
		Name:        *name,
		Arch:        *goarch,
		Version:     *version,
		Maintainer:  "Tailscale Inc <info@tailscale.com>",
		Contents:    contents,
		Scripts: nfpm.Scripts{
			PostInstall: *postinst,
			PreRemove:   *prerm,
			PostRemove:  *postrm,
		},
	})
	
	// Create package
	pkg, _ := nfpm.Get(*pkgType)
	pkg.Package(info, outputFile)
}
```

### Why This Approach?

1. **Cross-platform**: Build RPMs on macOS/Windows without rpmbuild
2. **Simple**: No spec file parsing, just Go structs
3. **Testable**: Can unit test packaging logic
4. **CI-friendly**: No system dependencies beyond Go

### leger Adaptation

**Option A: Use mkpkg directly** (recommended for simplicity)

Just copy Tailscale's mkpkg:
```bash
cp tailscale/cmd/mkpkg ./cmd/mkpkg
# Minimal changes needed
```

**Option B: Use nfpm CLI** (even simpler)

Skip mkpkg entirely, use nfpm's CLI:
```yaml
# nfpm.yaml
name: "leger"
arch: "amd64"
platform: "linux"
version: "v${VERSION}"
maintainer: "Your Name <email@example.com>"
description: "Podman Quadlet manager"
homepage: "https://github.com/yourname/leger"
license: "MIT"

contents:
  - src: ./leger
    dst: /usr/bin/leger
  - src: ./systemd/leger-daemon.service
    dst: /usr/lib/systemd/user/leger-daemon.service
  - src: ./systemd/leger-daemon@.service
    dst: /usr/lib/systemd/system/leger-daemon.service
  - src: ./config/leger.yaml
    dst: /etc/leger/config.yaml
    type: config
  - dst: /var/lib/leger/staged
    type: dir
  - dst: /var/lib/leger/backups
    type: dir

scripts:
  postinstall: ./release/rpm/postinst.sh
  preremove: ./release/rpm/prerm.sh
  postremove: ./release/rpm/postrm.sh

depends:
  - podman
```

Then: `nfpm pkg --packager rpm -f nfpm.yaml`

**Recommendation**: Start with Option B (nfpm CLI), it's the simplest. Add mkpkg wrapper later if you need programmatic control.

---

## 3. RPM Metadata & Scripts

### Tailscale's Approach

**rpm.postinst.sh** (runs after install):
```bash
# $1 == 1 for initial installation
# $1 == 2 for upgrades

if [ $1 -eq 1 ]; then
    # Initial install - enable and start if migrating from old package
    systemctl preset tailscaled.service >/dev/null 2>&1 || :
fi
```

**rpm.prerm.sh** (runs before removal):
```bash
# $1 == 0 for uninstallation
# $1 == 1 for upgrade

if [ $1 -eq 0 ]; then
    # Package removal, not upgrade
    systemctl --no-reload disable tailscaled.service >/dev/null 2>&1 || :
    systemctl stop tailscaled.service >/dev/null 2>&1 || :
fi
```

**rpm.postrm.sh** (runs after removal):
```bash
# $1 == 0 for uninstallation
# $1 == 1 for upgrade

systemctl daemon-reload >/dev/null 2>&1 || :
if [ $1 -ge 1 ]; then
    # Package upgrade, not uninstall
    systemctl try-restart tailscaled.service >/dev/null 2>&1 || :
fi
```

### Key Insights

1. **Standard RPM conventions**: `$1` parameter indicates install/upgrade/remove
2. **Systemd integration**: Enable/disable/restart services appropriately
3. **Error handling**: All systemctl commands use `|| :` to ignore errors
4. **Output suppression**: Redirect to `/dev/null 2>&1`
5. **Migration logic**: Handle upgrades from older packages

### leger Adaptation

**leger.postinst.sh**:
```bash
#!/bin/bash
# $1 == 1 for initial installation
# $1 == 2 for upgrades

if [ $1 -eq 1 ]; then
    # Initial install - don't auto-start, let user configure first
    echo "leger installed. Configure /etc/leger/config.yaml and then:"
    echo "  systemctl --user enable --now leger-daemon.service"
    echo "Or for system-wide: systemctl enable --now leger-daemon.service"
fi

# Create directories if they don't exist
mkdir -p /var/lib/leger/staged
mkdir -p /var/lib/leger/backups
mkdir -p /var/lib/leger/manifests

# Reload systemd to pick up new units
systemctl daemon-reload >/dev/null 2>&1 || :
systemctl --user daemon-reload >/dev/null 2>&1 || :
```

**leger.prerm.sh**:
```bash
#!/bin/bash
# $1 == 0 for uninstallation
# $1 == 1 for upgrade

if [ $1 -eq 0 ]; then
    # Package removal, not upgrade - stop all instances
    # Stop user instances (best effort)
    for user in $(loginctl list-users --no-legend | awk '{print $2}'); do
        sudo -u "$user" XDG_RUNTIME_DIR=/run/user/$(id -u "$user") \
            systemctl --user stop leger-daemon.service 2>/dev/null || :
        sudo -u "$user" XDG_RUNTIME_DIR=/run/user/$(id -u "$user") \
            systemctl --user disable leger-daemon.service 2>/dev/null || :
    done
    
    # Stop system instance
    systemctl stop leger-daemon.service >/dev/null 2>&1 || :
    systemctl disable leger-daemon.service >/dev/null 2>&1 || :
fi
```

**leger.postrm.sh**:
```bash
#!/bin/bash
# $1 == 0 for uninstallation
# $1 == 1 for upgrade

systemctl daemon-reload >/dev/null 2>&1 || :

if [ $1 -ge 1 ]; then
    # Package upgrade, not uninstall - restart if it was running
    systemctl try-restart leger-daemon.service >/dev/null 2>&1 || :
fi

if [ $1 -eq 0 ]; then
    # Full uninstall - optionally clean up data
    echo "leger has been uninstalled."
    echo "To remove data: rm -rf /var/lib/leger /etc/leger"
fi
```

---

## 4. Package Signing

### Tailscale's Approach

**Two-tier signing system** (very sophisticated):

1. **Root keys** (offline, long-lived):
   - Stored in `clientupdate/distsign/roots/*.pem`
   - Embedded in client at compile time
   - Sign the signing keys

2. **Signing keys** (online, rotatable):
   - Dynamically fetched by clients
   - Sign actual packages
   - Can be rotated without client updates

**Signing process**:
```go
// Generate signing key
sigPriv, sigPub, _ := distsign.GenerateSigningKey()

// Sign the package
h := distsign.NewPackageHash()
io.Copy(h, packageFile)
signature, _ := signingKey.SignPackageHash(h.Sum(nil), h.Len())

// Publish: package, package.sig, distsign.pub, distsign.pub.sig
```

**Client verification**:
```go
client, _ := distsign.NewClient(logf, "https://pkgs.tailscale.com")
client.Download(ctx, "tailscale_1.0.0_amd64.rpm", "/tmp/tailscale.rpm")
// Automatically verifies signature
```

### Key Insights

1. **Why two-tier?** Allows key rotation without recompiling clients
2. **Why Ed25519?** Fast, small signatures, well-vetted crypto
3. **Why hash+length?** Protects against truncation attacks
4. **Why BLAKE2s?** Faster than SHA-256, cryptographically secure

### leger Adaptation

**For initial releases**: Simplified single-tier signing

**Simple GPG signing** (standard RPM approach):
```bash
# One-time: Generate GPG key
gpg --full-generate-key
# Choose: RSA and RSA, 4096 bits, never expires
# Export public key
gpg --export -a "your@email.com" > RPM-GPG-KEY-leger

# Sign package
rpmsign --addsign --key-id=YOURKEYID leger-*.rpm

# Verify
rpm --checksig leger-*.rpm
```

**For future**: Adopt Tailscale's distsign approach

Copy `clientupdate/distsign/` directory and adapt:
```bash
# Generate root key (keep offline!)
go run ./cmd/distsign generate-root > root.pem
go run ./cmd/distsign generate-root --public > root.pub

# Generate signing key
go run ./cmd/distsign generate-signing > signing.pem
go run ./cmd/distsign generate-signing --public > signing.pub

# Sign signing key with root
go run ./cmd/distsign sign-keys signing.pub > signing.pub.sig

# Sign package
go run ./cmd/distsign sign-package leger.rpm > leger.rpm.sig
```

**Recommendation**: Start with GPG signing, migrate to distsign later when you have multiple releases and want better key rotation.

---

## 5. Version Stamping

### Tailscale's Approach

**cmd/mkversion/mkversion.go**:
```go
func main() {
	info := mkversion.Info() // Extracts from git
	fmt.Println(info.String()) // Outputs shell variables
	// VERSION_MINOR="1.52"
	// VERSION_SHORT="1.52.0"
	// VERSION_LONG="1.52.0-t1234567890"
	// VERSION_GIT_HASH="1234567890abcdef"
}
```

**version-embed.go**:
```go
package tailscaleroot

//go:embed VERSION.txt
var VersionDotTxt string

//go:embed go.toolchain.rev
var GoToolchainRev string
```

**build_dist.sh**:
```bash
eval `go run ./cmd/mkversion`
ldflags="-X tailscale.com/version.longStamp=${VERSION_LONG} \
         -X tailscale.com/version.shortStamp=${VERSION_SHORT}"
go build -ldflags "$ldflags" ./cmd/tailscaled
```

### Key Insights

1. **Single source of truth**: Git tags
2. **Embedded at compile time**: Via ldflags
3. **Multiple formats**: Short, long, commit hash
4. **Development vs release**: Different version strings

### leger Adaptation

**version/version.go**:
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

**Makefile**:
```makefile
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -ldflags "\
	-X github.com/yourname/leger/version.Version=$(VERSION) \
	-X github.com/yourname/leger/version.Commit=$(COMMIT) \
	-X github.com/yourname/leger/version.BuildDate=$(BUILD_DATE) \
	-w -s"

build:
	go build $(LDFLAGS) -trimpath -o leger ./cmd/leger
```

**Usage in code**:
```go
package main

import (
	"fmt"
	"github.com/yourname/leger/version"
)

func main() {
	if showVersion {
		fmt.Println(version.Long())
		return
	}
	// ...
}
```

---

## 6. CI/Release Automation

### Tailscale's Approach

From `.github/workflows/test.yml`, I can infer their release pattern:

```yaml
on:
  push:
    branches: ["main", "release-branch/*"]
    tags: ["v*"]

jobs:
  build:
    strategy:
      matrix:
        include:
          - goos: linux
            goarch: amd64
          - goos: linux
            goarch: arm64
    
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      
      - name: Build
        run: |
          eval $(go run ./cmd/mkversion)
          go build -ldflags="-X version.Long=${VERSION_LONG}"
      
      - name: Create packages
        run: |
          go run ./cmd/mkpkg --type rpm --arch ${{matrix.goarch}} \
            --version ${VERSION_SHORT} ...
      
      - name: Upload artifacts
        uses: actions/upload-artifact@v4
        with:
          name: packages-${{matrix.goarch}}
          path: "*.rpm"
```

### Key Insights

1. **Matrix builds**: Multiple arch/os combinations
2. **Artifact upload**: Collect packages for release
3. **Conditional triggers**: Different behavior for tags vs branches
4. **Caching**: Go modules cached across runs

### leger Adaptation

**.github/workflows/release.yml**:
```yaml
name: Release

on:
  push:
    tags:
      - 'v*'
  workflow_dispatch:
    inputs:
      version:
        description: 'Version to release'
        required: true

permissions:
  contents: write

jobs:
  build:
    name: Build RPM (${{ matrix.arch }})
    runs-on: ubuntu-latest
    strategy:
      matrix:
        include:
          - arch: amd64
            goarch: amd64
          - arch: arm64
            goarch: arm64
    
    steps:
      - name: Checkout
        uses: actions/checkout@v4
        with:
          fetch-depth: 0  # Need full history for git describe
      
      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.21'
      
      - name: Get version
        id: version
        run: |
          if [ "${{ github.event_name }}" = "workflow_dispatch" ]; then
            VERSION="${{ github.event.inputs.version }}"
          else
            VERSION="${GITHUB_REF#refs/tags/}"
          fi
          echo "version=${VERSION}" >> $GITHUB_OUTPUT
          echo "version_clean=${VERSION#v}" >> $GITHUB_OUTPUT
      
      - name: Build binary
        env:
          GOOS: linux
          GOARCH: ${{ matrix.goarch }}
          CGO_ENABLED: 0
        run: |
          VERSION=${{ steps.version.outputs.version }}
          COMMIT=${{ github.sha }}
          BUILD_DATE=$(date -u +'%Y-%m-%dT%H:%M:%SZ')
          
          go build -trimpath \
            -ldflags="-X github.com/yourname/leger/version.Version=${VERSION} \
                      -X github.com/yourname/leger/version.Commit=${COMMIT} \
                      -X github.com/yourname/leger/version.BuildDate=${BUILD_DATE} \
                      -w -s" \
            -o leger-${{ matrix.arch }} \
            ./cmd/leger
      
      - name: Install nfpm
        run: |
          go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
      
      - name: Create RPM
        env:
          VERSION: ${{ steps.version.outputs.version_clean }}
          ARCH: ${{ matrix.arch }}
        run: |
          # Create nfpm config
          cat > nfpm.yaml <<EOF
          name: "leger"
          arch: "${ARCH}"
          platform: "linux"
          version: "${VERSION}"
          release: 1
          maintainer: "Your Name <you@example.com>"
          description: "Podman Quadlet manager from Git repositories"
          homepage: "https://github.com/yourname/leger"
          license: "MIT"
          
          contents:
            - src: ./leger-${ARCH}
              dst: /usr/bin/leger
              file_info:
                mode: 0755
            - src: ./systemd/leger-daemon.service
              dst: /usr/lib/systemd/user/leger-daemon.service
            - src: ./systemd/leger-daemon@.service
              dst: /usr/lib/systemd/system/leger-daemon.service
            - src: ./config/leger.yaml
              dst: /etc/leger/config.yaml
              type: config
            - dst: /var/lib/leger/staged
              type: dir
            - dst: /var/lib/leger/backups
              type: dir
            - dst: /var/lib/leger/manifests
              type: dir
          
          scripts:
            postinstall: ./release/rpm/postinst.sh
            preremove: ./release/rpm/prerm.sh
            postremove: ./release/rpm/postrm.sh
          
          depends:
            - podman
          
          rpm:
            group: System Environment/Daemons
            summary: Podman Quadlet manager
          EOF
          
          # Build RPM
          nfpm pkg --packager rpm -f nfpm.yaml
          
          # Rename to include arch
          mv leger-${VERSION}-1.*.rpm leger-${VERSION}-1.${ARCH}.rpm
      
      - name: Upload RPM
        uses: actions/upload-artifact@v4
        with:
          name: rpm-${{ matrix.arch }}
          path: "*.rpm"
          if-no-files-found: error
  
  release:
    name: Create GitHub Release
    needs: build
    runs-on: ubuntu-latest
    
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      
      - name: Download artifacts
        uses: actions/download-artifact@v4
        with:
          path: artifacts
      
      - name: Get version
        id: version
        run: |
          if [ "${{ github.event_name }}" = "workflow_dispatch" ]; then
            VERSION="${{ github.event.inputs.version }}"
          else
            VERSION="${GITHUB_REF#refs/tags/}"
          fi
          echo "version=${VERSION}" >> $GITHUB_OUTPUT
      
      - name: Create Release
        uses: softprops/action-gh-release@v1
        with:
          tag_name: ${{ steps.version.outputs.version }}
          name: Release ${{ steps.version.outputs.version }}
          draft: false
          prerelease: false
          files: |
            artifacts/rpm-*/*.rpm
          body: |
            ## Installation
            
            ### Fedora/RHEL
            ```bash
            sudo dnf install ./leger-*.rpm
            ```
            
            ### Configuration
            1. Edit `/etc/leger/config.yaml`
            2. Start the service:
               - User: `systemctl --user enable --now leger-daemon.service`
               - System: `sudo systemctl enable --now leger-daemon.service`
            
            ## What's Changed
            See the [CHANGELOG](CHANGELOG.md) for details.
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

---

## 7. Complete Implementation Files

### Makefile

```makefile
# Project info
PROJECT := leger
BINARY := leger
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

# Build settings
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0

# ldflags for version embedding
LDFLAGS := -ldflags "\
	-X github.com/yourname/$(PROJECT)/version.Version=$(VERSION) \
	-X github.com/yourname/$(PROJECT)/version.Commit=$(COMMIT) \
	-X github.com/yourname/$(PROJECT)/version.BuildDate=$(BUILD_DATE) \
	-w -s"

# Build flags
BUILD_FLAGS := -trimpath $(LDFLAGS)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary
	GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=$(CGO_ENABLED) \
		go build $(BUILD_FLAGS) -o $(BINARY) ./cmd/$(PROJECT)

.PHONY: install
install: build ## Install to /usr/local/bin
	sudo install -m 755 $(BINARY) /usr/local/bin/$(BINARY)

.PHONY: test
test: ## Run tests
	go test -v -race ./...

.PHONY: lint
lint: ## Run linters
	golangci-lint run

.PHONY: clean
clean: ## Clean build artifacts
	rm -f $(BINARY) *.rpm *.deb
	rm -rf dist/

.PHONY: rpm
rpm: build ## Build RPM package
	@command -v nfpm >/dev/null 2>&1 || { echo "nfpm not found. Install: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest"; exit 1; }
	VERSION=$(VERSION) ARCH=$(GOARCH) envsubst < nfpm.yaml > nfpm-build.yaml
	nfpm pkg --packager rpm -f nfpm-build.yaml
	rm nfpm-build.yaml

.PHONY: install-rpm
install-rpm: rpm ## Install RPM locally
	sudo dnf install -y ./$(PROJECT)-*.rpm

.PHONY: uninstall-rpm
uninstall-rpm: ## Uninstall RPM
	sudo dnf remove -y $(PROJECT)

.PHONY: release
release: ## Create a release (requires VERSION=vX.Y.Z)
	@if [ -z "$(VERSION)" ]; then echo "VERSION required"; exit 1; fi
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)

.DEFAULT_GOAL := help
```

### nfpm.yaml

```yaml
name: "leger"
arch: "${ARCH}"
platform: "linux"
version: "${VERSION}"
release: 1
section: "default"
priority: "extra"
maintainer: "Your Name <you@example.com>"
description: |
  leger - Podman Quadlet manager
  
  Manages Podman Quadlets from Git repositories with Setec integration
  for secure secret management.
homepage: "https://github.com/yourname/leger"
license: "MIT"
vendor: "Your Organization"

contents:
  # Binary
  - src: ./leger-${ARCH}
    dst: /usr/bin/leger
    file_info:
      mode: 0755
  
  # Systemd units
  - src: ./systemd/leger-daemon.service
    dst: /usr/lib/systemd/user/leger-daemon.service
    file_info:
      mode: 0644
  
  - src: ./systemd/leger-daemon@.service
    dst: /usr/lib/systemd/system/leger-daemon.service
    file_info:
      mode: 0644
  
  # Config
  - src: ./config/leger.yaml
    dst: /etc/leger/config.yaml
    type: config
    file_info:
      mode: 0644
  
  # Directories
  - dst: /var/lib/leger/staged
    type: dir
    file_info:
      mode: 0755
  
  - dst: /var/lib/leger/backups
    type: dir
    file_info:
      mode: 0755
  
  - dst: /var/lib/leger/manifests
    type: dir
    file_info:
      mode: 0755

scripts:
  postinstall: ./release/rpm/postinst.sh
  preremove: ./release/rpm/prerm.sh
  postremove: ./release/rpm/postrm.sh

depends:
  - podman

recommends:
  - git

rpm:
  group: "System Environment/Daemons"
  summary: "Podman Quadlet manager from Git"
  compression: xz
```

---

## 8. Recommendations

### Immediate Actions (Week 1)

1. ✅ **Set up version stamping**
   - Create `version/version.go`
   - Update Makefile with ldflags
   - Test: `make build && ./leger --version`

2. ✅ **Create nfpm configuration**
   - Copy `nfpm.yaml` template above
   - Adapt paths for your project structure
   - Install nfpm: `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest`

3. ✅ **Write RPM scripts**
   - Create `release/rpm/` directory
   - Add postinst, prerm, postrm scripts
   - Test scriptlet logic manually

4. ✅ **Test local RPM build**
   ```bash
   make build
   make rpm
   sudo dnf install ./leger-*.rpm
   leger --version
   ```

### Short Term (Month 1)

5. ✅ **Set up GitHub Actions**
   - Copy `.github/workflows/release.yml` above
   - Test with manual workflow dispatch
   - Create a test release tag

6. ✅ **Document installation**
   - README with installation instructions
   - Configuration examples
   - Troubleshooting guide

7. ✅ **Basic signing** (GPG)
   - Generate GPG key for project
   - Sign RPMs manually
   - Document verification steps

### Long Term (Quarter 1)

8. ✅ **Automated signing in CI**
   - Store GPG key in GitHub secrets
   - Auto-sign releases
   - Publish public key

9. ✅ **Advanced signing** (distsign)
   - Only if you need key rotation without client updates
   - Copy `clientupdate/distsign/` from Tailscale
   - Implement in release pipeline

10. ✅ **Package repository**
    - Host on GitHub Pages or COPR
    - Create repo metadata
    - Enable `dnf install leger`

---

## 9. Implementation Checklist

### Phase 1: Local Development
- [ ] Create `version/version.go` with Version, Commit, BuildDate vars
- [ ] Update Makefile with version stamping
- [ ] Test build with version: `make build && ./leger --version`
- [ ] Create systemd unit files in `systemd/`
- [ ] Create default config in `config/leger.yaml`
- [ ] Write RPM scriptlets in `release/rpm/`
- [ ] Create `nfpm.yaml` configuration
- [ ] Install nfpm: `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest`
- [ ] Test RPM creation: `make rpm`
- [ ] Test RPM installation: `sudo dnf install ./leger-*.rpm`
- [ ] Verify service can start: `systemctl --user status leger-daemon`

### Phase 2: CI/CD Setup
- [ ] Create `.github/workflows/release.yml`
- [ ] Test workflow with workflow_dispatch
- [ ] Create test git tag and verify release build
- [ ] Set up artifact uploads
- [ ] Test multi-architecture builds (amd64, arm64)
- [ ] Create GitHub release with uploaded RPMs
- [ ] Document installation in README

### Phase 3: Signing & Distribution
- [ ] Generate GPG signing key
- [ ] Document key management
- [ ] Add GPG signing to build process
- [ ] Store GPG key in GitHub secrets (encrypted)
- [ ] Auto-sign releases in CI
- [ ] Publish public key for users to verify
- [ ] Consider COPR repository for easier distribution

### Phase 4: Polish & Maintenance
- [ ] Set up automated testing of RPM installation
- [ ] Create upgrade test matrix (v1→v2→v3)
- [ ] Document troubleshooting steps
- [ ] Set up package signing verification in tests
- [ ] Consider adopting distsign for key rotation (if needed)
- [ ] Monitor package installation metrics

---

## 10. Key Differences from Tailscale

| Aspect | Tailscale | leger |
|--------|-----------|-------|
| **Binary** | Multiple (tailscale, tailscaled, derper) | Single binary (leger) |
| **Services** | System daemon only | User + system systemd units |
| **Signing** | Two-tier Ed25519 (complex) | GPG initially (simple) |
| **Platforms** | Multi-OS (Linux, Mac, Windows, BSD) | Linux-only (Fedora focus) |
| **Dependencies** | Minimal | Requires Podman |
| **Package tool** | nfpm wrapper (mkpkg) | Direct nfpm CLI |
| **Complexity** | Production at scale | Single developer, simpler |

---

## 11. Answers to Specific Questions

### Q: Why did Tailscale write mkpkg instead of shell scripts?

**A**: They didn't write a complex tool - mkpkg is just a thin Go wrapper around the `nfpm` library that:
- Parses command-line flags
- Builds an `nfpm.Info` struct
- Calls `nfpm.Package()`

It's ~200 lines of Go, not a complex orchestrator. Benefits:
- Type safety for package metadata
- Cross-platform (build RPMs on macOS)
- Testable (can unit test)
- Consistent with their Go codebase

**For leger**: You don't even need mkpkg - just use the nfpm CLI directly.

### Q: How does mkpkg structure the RPM build process?

**A**: mkpkg doesn't manage rpmbuild - it uses nfpm which creates RPMs programmatically:
```
Binary → nfpm.Info{} → RPM writer → .rpm file
```

No spec files, no rpmbuild, no external dependencies (except Go).

### Q: What's the flow: Go binary → mkpkg → rpmbuild?

**A**: There's no rpmbuild in the flow:
```
1. build_dist.sh → compiles Go binary with version embedded
2. mkpkg → calls nfpm library → writes RPM directly
3. Done - no rpmbuild involved
```

### Q: Do we need our own cmd/mkpkg?

**A**: No, unless you want programmatic control. Use nfpm CLI:
```bash
nfpm pkg --packager rpm -f nfpm.yaml
```

Add mkpkg later only if you need to:
- Dynamically generate package metadata
- Support complex file transformations
- Integrate deeply with other tooling

### Q: How do they install systemd units?

**A**: Via package contents (files list) + scriptlets:
```yaml
# nfpm.yaml
contents:
  - src: tailscaled.service
    dst: /usr/lib/systemd/system/tailscaled.service

scripts:
  postinstall: |
    systemctl daemon-reload
    systemctl preset tailscaled.service
```

### Q: Do they host their own RPM repository?

**A**: Based on the code, they likely:
1. Build RPMs in CI
2. Upload to GitHub releases
3. Also host at pkgs.tailscale.com for signature verification

For leger, start with GitHub releases only. Add COPR later for `dnf install leger`.

---

## 12. Success Criteria Verification

✅ **Run `make rpm` and get a working RPM**
- Makefile builds binary with version
- nfpm creates valid RPM
- File permissions correct

✅ **Install the RPM on Fedora and have leger work immediately**
- `sudo dnf install ./leger-*.rpm`
- Binary installed to `/usr/bin/leger`
- Config at `/etc/leger/config.yaml`
- Systemd units present

✅ **Have systemd units properly installed and enabled**
- Units copied to `/usr/lib/systemd/{user,system}/`
- postinst reloads systemd
- User can enable: `systemctl --user enable leger-daemon`

✅ **Sign packages for distribution**
- GPG key generated
- RPMs signed: `rpmsign --addsign`
- Users can verify: `rpm --checksig`

✅ **Automate releases via GitHub Actions**
- Push tag → triggers workflow
- Multi-arch builds
- Creates GitHub release with RPMs

✅ **Handle upgrades gracefully**
- prerm stops service on uninstall only
- postrm restarts service on upgrade
- Config files preserved

---

## Final Recommendation

**Start simple, iterate to production:**

1. **Week 1**: Local RPM building with nfpm
2. **Week 2**: GitHub Actions automation
3. **Week 3**: GPG signing
4. **Month 2+**: Consider COPR, advanced signing

Don't over-engineer - Tailscale's approach is simpler than it looks. The core is just:
- nfpm for packaging
- ldflags for versions
- GitHub Actions for automation
- Standard RPM conventions

You can have production-quality RPMs in a week. The sophisticated signing can wait until you have users who need it.
