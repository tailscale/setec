# System Prompt: Extract Tailscale's RPM Packaging Pattern for leger

## Context

You have access to the Tailscale codebase. I'm building **leger**, a Podman Quadlet manager written in Go that will be distributed as an RPM for Fedora. I want to adopt Tailscale's RPM packaging approach because:

1. **Professional quality**: Tailscale's packaging is production-grade
2. **Similar technology**: Both are Go binaries distributed as RPMs
3. **Signing infrastructure**: Need to learn their digital signing approach
4. **CI/CD patterns**: Want to automate the entire release process
5. **Version management**: Need proper version stamping like they do

## leger Project Overview

### What is leger?

- **Single Go binary** (two modes: CLI + daemon)
- **Manages Podman Quadlets** from Git repositories
- **Integrates with Setec** for secrets (uses Tailscale for auth, ironically)
- **Distributed as RPM** for Fedora/RHEL

### Package Requirements

```
# Binary locations
/usr/bin/leger                          # Main binary (CLI + daemon modes)

# Systemd units
/usr/lib/systemd/user/leger-daemon.service
/usr/lib/systemd/system/leger-daemon.service

# Directories
/var/lib/leger/staged/                  # Staged updates
/var/lib/leger/backups/                 # Backups
/var/lib/leger/manifests/               # Metadata

# Config
/etc/leger/config.yaml                  # System config
```

### Key Differences from Tailscale

- **Simpler**: Just one binary, not multiple daemons
- **User-focused**: Primarily user-scope systemd services
- **No kernel modules**: Pure userspace
- **No complex networking**: Just calls Podman and Setec

## Your Task

Analyze Tailscale's RPM packaging infrastructure and extract the relevant patterns for leger. Focus on these specific areas:

### 1. Build Orchestration

**Examine:**
- `Makefile` - How they orchestrate builds
- `build_dist.sh` - Distribution-specific build logic

**Extract:**
- How they structure the build process
- How they handle different architectures (amd64, arm64)
- How they pass version information through the build
- Integration with GoReleaser (if any)

**Produce for leger:**
```makefile
# Makefile targets we need:
make build              # Build binary
make rpm                # Build RPM
make sign              # Sign packages
make release           # Full release process
make clean             # Cleanup
```

### 2. Packaging Logic

**Examine:**
- `cmd/mkpkg/main.go` - The Go-based packaging orchestrator

**Questions to answer:**
1. Why did they write a Go tool instead of pure shell scripts?
2. How does mkpkg structure the RPM build process?
3. How does it handle file installation?
4. How does it manage systemd unit installation?
5. What's the flow: Go binary → mkpkg → rpmbuild?

**Extract patterns for leger:**
- Do we need our own `cmd/mkpkg`?
- Or can we simplify and use rpmbuild directly?
- What's the minimum viable approach?

### 3. RPM Metadata

**Examine:**
- `release/rpm/*` - All RPM-related files
- Specifically: `.spec` file structure, pre/post install scripts

**Extract:**
1. **Spec file structure**
   - How they define dependencies
   - How they handle systemd units (enable/disable)
   - Pre/post install/remove scripts
   - File listings

2. **Install scripts**
   - What happens on install?
   - What happens on upgrade?
   - What happens on remove?
   - User/group creation (if any)

**Produce for leger:**
```spec
# leger.spec structure:
# - Metadata (Name, Version, Release, Summary)
# - BuildRequires
# - Requires
# - %prep, %build, %install
# - %pre, %post, %preun, %postun scripts
# - %files list
```

### 4. Package Signing

**Examine:**
- `clientupdate/distsign/*` - Digital signing implementation

**Questions:**
1. What signing approach do they use? (GPG? Custom?)
2. How do they integrate signing into the build pipeline?
3. Where are keys stored? (GitHub secrets? HSM?)
4. What gets signed? (Just RPM? Binaries too?)

**Extract for leger:**
- Simplified signing approach for initial releases
- Path to production signing setup
- Key management strategy

### 5. Version Stamping

**Examine:**
- `version-embed.go` - How version info is embedded
- `cmd/mkversion/mkversion.go` - Version generation tool

**How it works:**
1. Extract version from git tag
2. Embed in Go binary at compile time
3. Make available via `--version` flag

**Extract patterns:**
```go
// We need similar for leger:
var (
    Version   = "development"
    Commit    = "unknown"
    BuildDate = "unknown"
)

// Embedded at build time via -ldflags
```

**Questions:**
- How do they extract version from git?
- How do they handle dev builds vs releases?
- How do they pass version to rpmbuild?

### 6. CI/Release Automation

**Examine:**
- `.github/workflows/*.yml` - GitHub Actions workflows
- Specifically: workflows that build and publish RPMs

**Extract:**
1. **Build workflow**
   - Trigger: On push to main? On tag?
   - Matrix: Different arch/distro combinations?
   - Artifacts: What gets uploaded?

2. **Release workflow**
   - Trigger: Git tag
   - Steps: Build → Sign → Publish
   - Where do packages go? (GitHub releases? Repository?)

**Produce for leger:**
```yaml
# .github/workflows/release.yml
# - Trigger on git tag (v*)
# - Build for multiple architectures
# - Sign packages
# - Create GitHub release
# - Upload RPMs as release assets
```

## Specific Questions to Answer

### Architecture Questions

1. **Why mkpkg?**
   - What problem does the Go-based packaging tool solve?
   - Could leger use a simpler approach?
   - What's the trade-off?

2. **Systemd integration**
   - How do they install systemd units?
   - How do they enable/disable services?
   - User vs system units handling?

3. **Multi-arch**
   - How do they build for amd64 and arm64?
   - Cross-compilation setup?
   - Testing on different architectures?

### Practical Questions

4. **RPM repository**
   - Do they host their own RPM repository?
   - Or just GitHub releases?
   - COPR integration?

5. **Upgrades**
   - How do they handle package upgrades?
   - Data migration scripts?
   - Service restart logic?

6. **Dependencies**
   - What dependencies do they declare in the spec?
   - Runtime vs build-time dependencies?
   - How to handle Podman requirement?

## Deliverables

Please produce the following for leger:

### 1. Makefile
```makefile
# Complete Makefile with:
# - Version extraction from git
# - Build targets (binary, RPM)
# - Signing integration
# - Release automation
```

### 2. leger.spec
```spec
# Complete RPM spec file:
# - Proper metadata
# - Install/remove scripts for systemd
# - File listings
# - Dependencies
```

### 3. cmd/mkpkg Equivalent (if needed)
```go
// If Tailscale's approach justifies it:
// Go tool to orchestrate packaging
// Otherwise, explain why simpler approach is better
```

### 4. Build Scripts
```bash
# build_rpm.sh or equivalent
# Version stamping logic
# Integration with rpmbuild
```

### 5. GitHub Actions Workflow
```yaml
# .github/workflows/release.yml
# Complete CI/CD for RPM releases
```

### 6. Signing Documentation
```markdown
# docs/SIGNING.md
# - How to set up signing
# - Key management
# - Integration with CI
```

### 7. Implementation Guide
```markdown
# docs/RPM-PACKAGING.md
# Step-by-step guide:
# 1. How to build locally
# 2. How to test the RPM
# 3. How to release
# 4. How signing works
```

## Analysis Approach

For each area:

1. **Read and understand** Tailscale's implementation
2. **Extract the pattern** - what problem does it solve?
3. **Assess complexity** - is this necessary for leger?
4. **Adapt or simplify** - create leger's version
5. **Provide rationale** - explain your decisions

## Output Format

Structure your response as:

```markdown
# Tailscale RPM Packaging Analysis for leger

## 1. Build Orchestration
### Tailscale's Approach
[What they do]

### Key Insights
[What we can learn]

### leger Adaptation
[What we'll do differently/similarly]

### Code/Config
[Actual Makefile/script for leger]

## 2. Packaging Logic
[Same structure]

## 3. RPM Metadata
[Same structure]

## 4. Package Signing
[Same structure]

## 5. Version Stamping
[Same structure]

## 6. CI/Release Automation
[Same structure]

## 7. Recommendations
[Overall recommendations for leger]

## 8. Implementation Checklist
- [ ] Task 1
- [ ] Task 2
[...]
```

## Constraints

1. **Simplicity**: leger is simpler than Tailscale - prefer simple solutions
2. **Standard tools**: Use standard RPM tools when possible
3. **Fedora-first**: Focus on Fedora/RHEL (no Debian/Arch needed initially)
4. **Single binary**: Unlike Tailscale's multiple components
5. **User-focused**: Primarily user systemd units, not system services

## Context You Have

You already have access to the Tailscale codebase with these key files:

- `Makefile` and `build_dist.sh`
- `cmd/mkpkg/main.go`
- `release/rpm/*`
- `clientupdate/distsign/*`
- `version-embed.go` and `cmd/mkversion/mkversion.go`
- `.github/workflows/*.yml`

Please analyze these and produce practical, production-ready packaging infrastructure for leger.

## Success Criteria

Your output should enable me to:

1. ✅ Run `make rpm` and get a working RPM
2. ✅ Install the RPM on Fedora and have leger work immediately
3. ✅ Have systemd units properly installed and enabled
4. ✅ Sign packages for distribution
5. ✅ Automate releases via GitHub Actions
6. ✅ Handle upgrades gracefully

---

**Start your analysis now. Focus on practical, copy-paste-ready solutions for leger.**
