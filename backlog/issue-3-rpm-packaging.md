# Issue #3: chore(rpm): add nfpm configuration and build scripts

## Context

Implement RPM packaging for leger (CLI + legerd daemon) using nfpm library, following Tailscale's proven approach. This enables distribution via `dnf install leger`.

**Complete Implementation Guide**: `/docs/rpm-packaging/RPM-PACKAGING.md`  
**Quick Start**: `/docs/rpm-packaging/README-QUICKSTART.md`  
**Reference Analysis**: `/docs/rpm-packaging/RPM-PACKAGING-ANALYSIS.md`

## Sprint Goal

This issue is part of v0.1.0 sprint focused on achieving functional Tailscale authentication. RPM packaging must be completed first to enable local testing of CLI features.

## Tasks

### 1. Create nfpm Configuration

- [ ] Create `nfpm.yaml` in repository root
  - Based on: `/docs/rpm-packaging/nfpm-dual.yaml`
  - Update module paths to `github.com/leger-labs/leger`
  - Include both binaries: `leger` and `legerd`
  - Configure systemd units (user + system scope)
  - Set up config files and directories

### 2. Update Makefile

- [ ] Add version stamping (already present in `version/version.go`)
- [ ] Add rpm build target: `make rpm`
- [ ] Add rpm install target: `make install-rpm`
- [ ] Add rpm uninstall target: `make uninstall-rpm`
- [ ] Add clean target: `make clean`
- [ ] Reference: `/docs/rpm-packaging/Makefile`

### 3. Create RPM Scriptlets

- [ ] Create `release/rpm/postinst.sh`
  - Create state directories
  - Reload systemd
  - systemd preset (don't auto-start)
- [ ] Create `release/rpm/prerm.sh`
  - Stop services only on uninstall ($1 == 0)
  - Preserve services on upgrade ($1 >= 1)
- [ ] Create `release/rpm/postrm.sh`
  - Reload systemd
  - Restart services on upgrade ($1 >= 1)
- [ ] Make all scripts executable: `chmod +x release/rpm/*.sh`
- [ ] Reference: `/docs/rpm-packaging/release/rpm/*.sh`

### 4. Verify Existing Files

These should already exist from Phase 1:
- [ ] `systemd/legerd.service` (user scope)
- [ ] `systemd/legerd@.service` (system scope)
- [ ] `config/leger.yaml` (default config)
- [ ] `version/version.go` (version stamping)

### 5. Test Local Build

```bash
# Install nfpm if not present
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

# Build binaries
make build

# Verify versions
./leger --version
./legerd --version

# Build RPM
make rpm

# Expected output: leger-X.Y.Z-1.x86_64.rpm
```

### 6. Test Local Installation

```bash
# Install
sudo dnf install ./leger-*.rpm

# Verify binaries
which leger
which legerd
leger --version
legerd --version

# Verify systemd units
systemctl --user list-unit-files | grep legerd
systemctl list-unit-files | grep legerd

# Verify config
cat /etc/leger/config.yaml

# Verify directories
ls -la /var/lib/leger/

# Test start (will fail without config, but verifies installation)
systemctl --user status legerd.service
```

### 7. Test Upgrade Path

```bash
# Build new version
git tag v0.1.1-test
make rpm

# Upgrade
sudo dnf upgrade ./leger-*.rpm

# Verify service stays running (if it was running)
systemctl --user status legerd.service
```

### 8. Test Uninstall

```bash
sudo dnf remove leger

# Verify binaries removed
which leger  # Should show: not found

# Verify state preserved (for rollback)
ls /var/lib/leger/  # Should still exist
```

## Acceptance Criteria

- [ ] `make rpm` produces `leger-X.Y.Z-1.x86_64.rpm`
- [ ] RPM installs successfully: `sudo dnf install ./leger-*.rpm`
- [ ] Both binaries present and functional:
  - [ ] `/usr/bin/leger --version` works
  - [ ] `/usr/bin/legerd --version` works
- [ ] Systemd units installed:
  - [ ] `/usr/lib/systemd/user/legerd.service`
  - [ ] `/usr/lib/systemd/system/legerd.service`
- [ ] Config file present: `/etc/leger/config.yaml`
- [ ] State directories created: `/var/lib/leger/`
- [ ] Post-install scripts execute without errors
- [ ] Upgrade preserves running service (if applicable)
- [ ] Uninstall removes binaries but preserves state

## Reference Documentation

### Primary Guides
- **Implementation**: `/docs/rpm-packaging/RPM-PACKAGING.md` (Sections 1-7)
- **Quick Start**: `/docs/rpm-packaging/README-QUICKSTART.md`
- **Tailscale Analysis**: `/docs/rpm-packaging/RPM-PACKAGING-ANALYSIS.md`

### Configuration Examples
- **nfpm**: `/docs/rpm-packaging/nfpm-dual.yaml`
- **Makefile**: `/docs/rpm-packaging/Makefile`
- **Scriptlets**: `/docs/rpm-packaging/release/rpm/*.sh`

### Systemd & Config
- **Units**: `/systemd/*.service` (already present)
- **Config**: `/config/leger.yaml` (already present)

## Notes

- Follow Tailscale's nfpm approach (not rpmbuild)
- Use git tags for version stamping (already implemented in `version/version.go`)
- All commits must follow conventional commits format
- Scriptlets must handle both user and system scope services
- RPM should work on Fedora 42+

## Testing Environment

```bash
# Recommended test environments:
# - Fedora 42 (primary target)
# - Fedora 41 (compatibility check)
# - Rocky Linux 9 (RHEL compatibility)

# Test in container:
podman run -it --rm fedora:42 bash
# Inside container:
dnf install -y ./leger-*.rpm
leger --version
```

## Dependencies

- Go 1.23+ (already present)
- nfpm tool: `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest`
- Fedora-based system for testing

## Expected Outcome

After this issue:
- ✅ `make rpm` produces working RPM packages
- ✅ Users can install leger via `dnf install`
- ✅ Both `leger` and `legerd` binaries are available
- ✅ Foundation ready for Issue #4 (CI/CD automation)
- ✅ Foundation ready for Issue #5 (CLI implementation)

## Issue Labels

- `type:chore`
- `area:rpm`
- `priority:critical`
- `sprint:v0.1.0`
