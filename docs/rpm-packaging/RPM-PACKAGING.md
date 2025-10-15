# RPM Packaging Implementation Guide for leger

This guide walks you through implementing production-ready RPM packaging for leger, based on Tailscale's battle-tested approach.

---

## Prerequisites

- Go 1.21 or later
- Git
- Linux system (Fedora, RHEL, Rocky, or similar recommended for testing)
- GitHub account (for CI/CD)

---

## Step 1: Project Structure Setup

### 1.1 Create Directory Structure

```bash
# Navigate to your leger project root
cd /path/to/leger

# Create required directories
mkdir -p config
mkdir -p systemd
mkdir -p release/rpm
mkdir -p version
mkdir -p docs
mkdir -p .github/workflows

# Create placeholder files
touch config/leger.yaml
touch systemd/leger-daemon.service
touch systemd/leger-daemon@.service
```

### 1.2 File Tree

Your project should look like this:

```
leger/
├── cmd/
│   └── leger/
│       └── main.go
├── version/
│   └── version.go              # ← Create from provided file
├── config/
│   └── leger.yaml              # ← Your default config
├── systemd/
│   ├── leger-daemon.service    # ← User service unit
│   └── leger-daemon@.service   # ← System service unit
├── release/
│   └── rpm/
│       ├── postinst.sh         # ← Create from provided file
│       ├── prerm.sh            # ← Create from provided file
│       └── postrm.sh           # ← Create from provided file
├── docs/
│   └── SIGNING.md              # ← Create from provided file
├── .github/
│   └── workflows/
│       └── release.yml         # ← Create from provided file
├── Makefile                     # ← Create from provided file
├── nfpm.yaml                    # ← Create from provided file
├── go.mod
└── README.md
```

---

## Step 2: Version Stamping

### 2.1 Create version/version.go

Copy the `version/version.go` file provided in the outputs.

### 2.2 Update main.go

Add version flag to your main application:

```go
package main

import (
    "flag"
    "fmt"
    "os"
    
    "github.com/yourname/leger/version"
)

var (
    showVersion = flag.Bool("version", false, "Show version information")
    showLong    = flag.Bool("version-long", false, "Show detailed version information")
)

func main() {
    flag.Parse()
    
    if *showLong {
        fmt.Println(version.Full())
        os.Exit(0)
    }
    
    if *showVersion {
        fmt.Println(version.Short())
        os.Exit(0)
    }
    
    // Your application logic here
    fmt.Println("leger daemon starting...")
}
```

### 2.3 Update go.mod

Update module path if needed:

```bash
# In go.mod, ensure your module path is correct
module github.com/yourname/leger

go 1.21
```

### 2.4 Test Version Stamping

```bash
# Build with version info
make build

# Test
./leger --version
# Should show: development

./leger --version-long
# Should show: development (commit unknown, built unknown)
```

---

## Step 3: Systemd Units

### 3.1 Create User Service (systemd/leger-daemon.service)

```ini
[Unit]
Description=leger Podman Quadlet Manager (User)
Documentation=https://github.com/yourname/leger
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/leger daemon --config=%E/leger/config.yaml
Restart=on-failure
RestartSec=5s

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ReadWritePaths=%S/leger

[Install]
WantedBy=default.target
```

### 3.2 Create System Service (systemd/leger-daemon@.service)

```ini
[Unit]
Description=leger Podman Quadlet Manager (System)
Documentation=https://github.com/yourname/leger
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/bin/leger daemon --config=/etc/leger/config.yaml
Restart=on-failure
RestartSec=5s

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/var/lib/leger

# Run as specific user if needed
# User=leger
# Group=leger

[Install]
WantedBy=multi-user.target
```

---

## Step 4: Default Configuration

### 4.1 Create config/leger.yaml

```yaml
# leger default configuration
# Copy to /etc/leger/config.yaml and customize

# Git repository containing Quadlet definitions
repository:
  url: ""
  branch: "main"
  poll_interval: "5m"

# Setec configuration for secrets
setec:
  enabled: false
  address: ""
  # auth_method: "tailscale"

# Quadlet management
quadlets:
  # Directory for staging updates
  staging_dir: "/var/lib/leger/staged"
  
  # Directory for backups
  backup_dir: "/var/lib/leger/backups"
  
  # Keep last N backups
  backup_count: 5
  
  # Podman configuration
  podman:
    socket: "unix:///run/podman/podman.sock"

# Logging
logging:
  level: "info"
  format: "text"
```

---

## Step 5: RPM Scriptlets

Copy the three scriptlet files to `release/rpm/`:
- `postinst.sh`
- `prerm.sh`  
- `postrm.sh`

Make them executable:

```bash
chmod +x release/rpm/*.sh
```

---

## Step 6: Install Development Tools

### 6.1 Install nfpm

```bash
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

# Verify installation
nfpm --version
```

### 6.2 Install Other Tools (Optional)

```bash
# golangci-lint for linting
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# For RPM signing (if on Fedora/RHEL)
sudo dnf install rpm-sign
```

---

## Step 7: Test Local Build

### 7.1 Build Binary

```bash
make build

# Verify
./leger --version
```

### 7.2 Build RPM

```bash
make rpm

# Output should show:
# Building RPM for amd64...
# Created: leger-0.0.0-dev-1.amd64.rpm

# Verify RPM contents
rpm -qilp leger-*.rpm
```

### 7.3 Test Installation

```bash
# Install locally
make install-rpm

# Verify installation
which leger
leger --version

# Check systemd units
systemctl --user list-unit-files | grep leger
systemctl list-unit-files | grep leger

# Try to start (will fail without proper config, but tests installation)
systemctl --user status leger-daemon.service
```

### 7.4 Test Uninstallation

```bash
make uninstall-rpm

# Verify removal
which leger  # Should show: not found
```

---

## Step 8: Git Tag for Versioning

### 8.1 Create First Tag

```bash
# Commit all changes
git add .
git commit -m "Add RPM packaging infrastructure"

# Create version tag
git tag -a v0.1.0 -m "Release v0.1.0"

# Build with real version
make build

# Check version
./leger --version
# Should now show: v0.1.0
```

### 8.2 Build Release RPM

```bash
make rpm

# Filename will include version
# leger-0.1.0-1.amd64.rpm
```

---

## Step 9: GitHub Actions Setup

### 9.1 Copy Workflow File

Copy `.github/workflows/release.yml` from the outputs to your repository.

### 9.2 Update Module Path

Edit `.github/workflows/release.yml` and replace:
```yaml
-ldflags="-X github.com/yourname/leger/version.Version=${VERSION} ...
```

With your actual module path from `go.mod`.

### 9.3 Commit and Push

```bash
git add .github/workflows/release.yml
git commit -m "Add GitHub Actions release workflow"
git push origin main
```

### 9.4 Test Workflow Manually

1. Go to your GitHub repository
2. Click "Actions" tab
3. Select "Release" workflow
4. Click "Run workflow"
5. Enter version: `v0.1.0-test`
6. Click "Run workflow"

This will test the full build pipeline without creating a real release.

### 9.5 Check Results

- Go to the workflow run
- Verify both amd64 and arm64 RPMs were created
- Download artifacts and test locally

---

## Step 10: Create First Real Release

### 10.1 Push Tag

```bash
# Create release tag
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

### 10.2 Monitor Build

1. Go to GitHub Actions
2. Watch the "Release" workflow execute
3. It should:
   - Build binaries for amd64 and arm64
   - Create RPMs
   - Create a GitHub release
   - Upload RPM files

### 10.3 Verify Release

1. Go to your repository's "Releases" page
2. You should see "Release v0.1.0"
3. It should have two attachments:
   - `leger-0.1.0-1.amd64.rpm`
   - `leger-0.1.0-1.arm64.rpm`

---

## Step 11: Package Signing (Optional but Recommended)

See [docs/SIGNING.md](./SIGNING.md) for detailed instructions.

### Quick Start (GPG Signing)

```bash
# Generate key
gpg --full-generate-key

# Export public key
gpg --export --armor "your@email.com" > RPM-GPG-KEY-leger

# Commit public key
git add RPM-GPG-KEY-leger
git commit -m "Add RPM signing public key"
git push

# Sign RPMs locally
make sign GPG_KEY=your@email.com

# Verify
rpm --checksig leger-*.rpm
```

---

## Step 12: Documentation

### 12.1 Update README.md

Add installation section:

```markdown
## Installation

### Fedora/RHEL/Rocky Linux

Download the RPM for your architecture from the [releases page](https://github.com/yourname/leger/releases):

```bash
# Download (replace X.Y.Z with actual version)
curl -LO https://github.com/yourname/leger/releases/download/vX.Y.Z/leger-X.Y.Z-1.amd64.rpm

# Install
sudo dnf install ./leger-X.Y.Z-1.amd64.rpm

# Configure
sudo vim /etc/leger/config.yaml

# Start (user service)
systemctl --user enable --now leger-daemon.service

# Or start (system service)
sudo systemctl enable --now leger-daemon.service
```

### Verify Installation

```bash
leger --version
systemctl --user status leger-daemon.service
```
```

### 12.2 Create CHANGELOG.md

```markdown
# Changelog

All notable changes to leger will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2024-01-XX

### Added
- Initial release
- Podman Quadlet management from Git
- Setec integration for secrets
- RPM packaging for Fedora/RHEL
- User and system systemd services
- Automated releases via GitHub Actions

[Unreleased]: https://github.com/yourname/leger/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/yourname/leger/releases/tag/v0.1.0
```

---

## Step 13: Testing Matrix

### 13.1 Test on Different Systems

Create a test matrix:

| System | Version | Status | Notes |
|--------|---------|--------|-------|
| Fedora | 39 | ✅ | |
| Fedora | 40 | ✅ | |
| RHEL | 9 | ⏳ | |
| Rocky | 9 | ⏳ | |
| Alma | 9 | ⏳ | |

### 13.2 Test Scenarios

For each system:

```bash
# 1. Fresh install
sudo dnf install ./leger-*.rpm
systemctl --user status leger-daemon.service

# 2. Upgrade
sudo dnf install ./leger-*-v0.2.0-*.rpm
systemctl --user status leger-daemon.service

# 3. Downgrade
sudo dnf downgrade ./leger-*-v0.1.0-*.rpm
systemctl --user status leger-daemon.service

# 4. Uninstall
sudo dnf remove leger
ls /var/lib/leger  # Should still exist
ls /usr/bin/leger  # Should NOT exist
```

---

## Step 14: Continuous Improvement

### 14.1 Monitor Issues

Track common installation problems:
- Permission issues
- Config file problems
- Systemd service failures
- Upgrade path issues

### 14.2 Add Automated Tests

Create `.github/workflows/test-rpm.yml`:

```yaml
name: Test RPM Installation

on:
  pull_request:
  push:
    branches: [main]

jobs:
  test:
    strategy:
      matrix:
        os:
          - fedora:39
          - fedora:40
    runs-on: ubuntu-latest
    container: ${{ matrix.os }}
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Install dependencies
        run: dnf install -y go rpm-build
      
      - name: Build RPM
        run: make rpm
      
      - name: Install RPM
        run: dnf install -y ./leger-*.rpm
      
      - name: Test binary
        run: leger --version
      
      - name: Check files
        run: |
          test -f /usr/bin/leger
          test -f /etc/leger/config.yaml
          test -d /var/lib/leger
```

---

## Troubleshooting

### Common Issues

#### Issue: `nfpm: command not found`

**Solution:**
```bash
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

#### Issue: RPM build fails with "file not found"

**Solution:** Ensure all files in `nfpm.yaml` exist:
```bash
# Check binary
ls -l leger-amd64

# Check systemd units
ls -l systemd/*.service

# Check config
ls -l config/leger.yaml

# Check scripts
ls -l release/rpm/*.sh
```

#### Issue: Version shows as "development"

**Solution:** Make sure you have a git tag:
```bash
git tag v0.1.0
make build
./leger --version
```

#### Issue: Service fails to start after install

**Solution:** Check the service logs:
```bash
# User service
systemctl --user status leger-daemon.service
journalctl --user -u leger-daemon.service -n 50

# System service
sudo systemctl status leger-daemon.service
sudo journalctl -u leger-daemon.service -n 50
```

---

## Quick Reference

### Build Commands

```bash
make build              # Build binary
make build-all          # Build all architectures
make rpm                # Build RPM for current arch
make rpm-all            # Build RPMs for all architectures
make install-rpm        # Install RPM locally
make uninstall-rpm      # Uninstall RPM
make clean              # Clean build artifacts
```

### Release Process

```bash
# 1. Update changelog
vim CHANGELOG.md

# 2. Commit changes
git add .
git commit -m "Release v1.0.0"

# 3. Create and push tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin main
git push origin v1.0.0

# 4. GitHub Actions will build and publish
# 5. Verify release on GitHub
```

### User Installation

```bash
# Install
sudo dnf install ./leger-*.rpm

# Configure
sudo vim /etc/leger/config.yaml

# Start (choose one)
systemctl --user enable --now leger-daemon.service
sudo systemctl enable --now leger-daemon.service

# Check status
systemctl --user status leger-daemon.service

# Logs
journalctl --user -u leger-daemon.service -f
```

---

## Success Checklist

- [ ] Directory structure created
- [ ] version/version.go implemented
- [ ] Systemd units created
- [ ] Default config created
- [ ] RPM scriptlets created and executable
- [ ] Makefile copied and tested
- [ ] nfpm.yaml configured
- [ ] nfpm installed
- [ ] Local RPM build successful
- [ ] Local RPM installation successful
- [ ] Git tag created
- [ ] Version stamping working
- [ ] GitHub Actions workflow added
- [ ] Test release created
- [ ] Documentation updated
- [ ] GPG signing configured (optional)
- [ ] Tested on target systems

---

## Next Steps

After completing this guide:

1. **COPR Repository** - Set up COPR for easier user installation
2. **Advanced Signing** - Consider Tailscale's distsign for key rotation
3. **Homebrew** - Add macOS support via Homebrew
4. **Documentation Site** - Create comprehensive docs
5. **Monitoring** - Track installation metrics and issues

---

## Support

If you encounter issues:

1. Check this guide's troubleshooting section
2. Review [docs/SIGNING.md](./SIGNING.md) for signing issues
3. Check GitHub Actions logs for build failures
4. Open an issue on GitHub with:
   - Your OS and version
   - RPM filename
   - Error messages
   - Steps to reproduce

---

## Resources

- [nfpm Documentation](https://nfpm.goreleaser.com/)
- [RPM Packaging Guide](https://rpm-packaging-guide.github.io/)
- [Fedora Packaging Guidelines](https://docs.fedoraproject.org/en-US/packaging-guidelines/)
- [systemd Service Files](https://www.freedesktop.org/software/systemd/man/systemd.service.html)
- [Tailscale Source Code](https://github.com/tailscale/tailscale)
