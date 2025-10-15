# Issue #4: ci: add RPM build and release workflow

## Context

Automate RPM building and distribution in GitHub Actions. On release tags, build multi-architecture RPMs and optionally deploy to Cloudflare R2 for public repository.

**Implementation Guide**: `/docs/rpm-packaging/RPM-PACKAGING.md` (Section 9)  
**Cloudflare Setup**: `/docs/rpm-packaging/CLOUDFLARE-SETUP.md`  
**Reference Workflow**: `/docs/rpm-packaging/.github/workflows/release-cloudflare.yml`

## Sprint Goal

Part of v0.1.0 sprint. This issue enables automated releases so that when authentication features are complete, users can immediately install via RPM repository.

## Dependencies

- ✅ Issue #3: RPM packaging must be working locally

## Tasks

### 1. Create GitHub Actions Workflow

- [ ] Create `.github/workflows/release.yml`
  - Trigger on git tags: `v*`
  - Support manual dispatch for testing
  - Matrix build: amd64, arm64
  - Upload RPMs to GitHub releases

### 2. Configure Build Steps

```yaml
# Key workflow elements:
- Checkout with full history (for git describe)
- Set up Go 1.23
- Extract version from tag
- Build binaries with version stamping
- Install nfpm
- Create RPMs
- Upload artifacts
- Create GitHub release
```

### 3. Test Workflow Manually

```bash
# Test via workflow_dispatch first
# GitHub → Actions → Release → Run workflow
# Input: v0.1.0-test

# Verify:
# - Both amd64 and arm64 RPMs created
# - Artifacts uploaded
# - No errors in logs
```

### 4. Test with Real Tag

```bash
# Create release tag
git tag -a v0.1.0-rc1 -m "Release candidate 1"
git push origin v0.1.0-rc1

# Verify:
# - Workflow triggers automatically
# - GitHub release created
# - RPMs attached to release
```

### 5. Optional: Cloudflare R2 Setup

⚠️ **Skip for v0.1.0** - Can be added later

If pursuing R2 deployment:
- [ ] Create Cloudflare R2 bucket
- [ ] Enable public access (pkgs.leger.run)
- [ ] Generate API token
- [ ] Add GitHub secrets:
  - `CLOUDFLARE_API_TOKEN`
  - `CLOUDFLARE_ACCOUNT_ID`
  - `CLOUDFLARE_ZONE_ID`
- [ ] Update workflow to upload to R2
- [ ] Generate repository metadata (createrepo_c)

**Reference**: `/docs/rpm-packaging/CLOUDFLARE-SETUP.md`

## Workflow File Structure

```yaml
name: Release

on:
  push:
    tags: ['v*']
  workflow_dispatch:
    inputs:
      version:
        description: 'Version to release'
        required: true

jobs:
  build:
    name: Build RPM (${{ matrix.arch }})
    strategy:
      matrix:
        include:
          - arch: amd64
            goarch: amd64
          - arch: arm64
            goarch: arm64
    steps:
      - Checkout
      - Setup Go
      - Get version
      - Build binaries
      - Install nfpm
      - Create RPM
      - Upload artifacts

  release:
    name: Create GitHub Release
    needs: build
    steps:
      - Download artifacts
      - Create release
      - Upload RPMs
```

## Acceptance Criteria

- [ ] Workflow file created: `.github/workflows/release.yml`
- [ ] Manual trigger works (workflow_dispatch)
- [ ] Tag trigger works (push tags)
- [ ] Both architectures build:
  - [ ] `leger-X.Y.Z-1.amd64.rpm`
  - [ ] `leger-X.Y.Z-1.arm64.rpm`
- [ ] GitHub release created automatically
- [ ] RPMs attached to release
- [ ] Release body includes installation instructions
- [ ] Version stamping works (git describe)

## Testing Plan

### Test 1: Manual Dispatch
```bash
# Via GitHub UI:
# Actions → Release → Run workflow
# Version: v0.1.0-test

# Expected:
# - Workflow completes successfully
# - Artifacts available for download
# - No release created (workflow_dispatch doesn't create releases)
```

### Test 2: Tag Push
```bash
# Create and push tag
git tag -a v0.1.0-rc1 -m "Release candidate 1"
git push origin v0.1.0-rc1

# Expected:
# - Workflow triggers automatically
# - GitHub release created: "Release v0.1.0-rc1"
# - RPMs attached to release
```

### Test 3: Installation from Release
```bash
# Download from GitHub releases
curl -LO https://github.com/leger-labs/leger/releases/download/v0.1.0-rc1/leger-0.1.0-1.amd64.rpm

# Install
sudo dnf install ./leger-0.1.0-1.amd64.rpm

# Verify
leger --version  # Should show v0.1.0-rc1
```

## Reference Documentation

- **Workflow**: `/docs/rpm-packaging/.github/workflows/release-cloudflare.yml`
- **Cloudflare Setup**: `/docs/rpm-packaging/CLOUDFLARE-SETUP.md`
- **Implementation**: `/docs/rpm-packaging/RPM-PACKAGING.md` (Section 9)

## Notes

- Start with GitHub releases only (simpler)
- Cloudflare R2 can be added in future issue
- Version stamping uses git tags (already implemented)
- All commits must follow conventional commits
- Test with `-rc` tags before final release

## Expected Outcome

After this issue:
- ✅ Automated RPM builds on git tags
- ✅ Multi-architecture support (amd64, arm64)
- ✅ GitHub releases with RPM downloads
- ✅ Foundation ready for public distribution
- ✅ Sprint can proceed with CLI features

## Issue Labels

- `type:ci`
- `area:ci`
- `priority:high`
- `sprint:v0.1.0`

## Deferred Work

- Cloudflare R2 deployment (future issue)
- Package signing (future issue)
- COPR repository (future issue)
