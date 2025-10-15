# leger - Executive Summary

## The Vision

**leger** is a single Go binary that brings together the best ideas from three distinct projects:

1. **pq** - Git-based quadlet management CLI
2. **BlueBuild quadlets module** - Advanced features (staged updates, backups, validation)
3. **Setec** - Tailscale-authenticated secrets management

The result: A comprehensive Podman Quadlet manager with integrated secrets, distributed as an RPM for Fedora.

## The Problem

Current solutions are fragmented:

- **pq**: Great CLI but uses manual file operations, no staged updates, no backup/restore, no secrets
- **BlueBuild module**: Advanced features but only works on immutable systems, build-time only
- **Manual Setec + Podman**: Works but requires gluing together multiple tools

## The Solution

### leger CLI

```bash
# Install from Git
leger install https://github.com/org/quadlets/tree/main/myapp

# Staged updates (safe workflow)
leger stage all
leger diff myapp
leger apply myapp

# Backup & restore
leger backup all
leger restore myapp 20241012-120000

# Service management
leger status myapp
leger logs myapp
```

### leger Daemon (legerd)

```bash
# Start daemon
systemctl --user start leger-daemon

# Daemon handles:
# - Fetching secrets from Setec (via Tailscale auth)
# - Syncing to Podman secrets
# - Secret rotation monitoring
```

## Key Innovations

### 1. Native Podman Integration

**Old (pq):**
```go
// Manual file operations
copyDir(quadletPath, installDir)
systemdDaemonReload()
```

**New (leger):**
```go
// Use native Podman commands
exec.Command("podman", "quadlet", "install", quadletPath)
```

**Benefits:**
- 70% less code
- Better error handling
- Automatic systemd integration
- Future-proof

### 2. Staged Updates with Preview

**From BlueBuild module:**
```bash
leger stage myapp         # Download but don't apply
leger diff myapp          # See exact changes
leger apply myapp         # Apply when ready
```

**Benefits:**
- Preview changes before applying
- Discard unwanted updates
- No risk to running services

### 3. Backup with Volumes

**From BlueBuild module:**
```bash
leger backup myapp        # Backup quadlet + volumes
leger restore myapp       # Full restore
```

**Benefits:**
- Complete backups including data
- Point-in-time restore
- Automatic backup before updates

### 4. Secrets Integration

**New feature inspired by your requirements:**
```yaml
# .leger.yaml
secrets:
  - name: leger/myapp/api-key
    podman-secret: myapp-api-key
    env: API_KEY
```

**Workflow:**
1. **leger-daemon** fetches from Setec (Tailscale auth)
2. **leger-daemon** syncs to Podman secrets
3. **Quadlet** references Podman secret
4. **Container** gets secret as environment variable

**Benefits:**
- Zero-trust authentication via Tailscale
- Automatic secret rotation
- Audit logs from Setec
- No secrets in Git repos

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      leger (single binary)                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  CLI Mode (leger)              Daemon Mode (leger daemon)      │
│  ├─ Install from Git           ├─ Setec client                 │
│  ├─ Stage/Apply/Rollback       ├─ Secrets sync                 │
│  ├─ Backup/Restore             ├─ Podman secrets injection     │
│  └─ Native Podman integration  └─ Tailscale auth               │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
         ↓                              ↓
    ┌────────┐                    ┌──────────┐
    │ Podman │←───────────────────│  Setec   │
    │Quadlet │   Podman Secrets   │ Service  │
    └────────┘                    └──────────┘
```

## Why This is Better

### vs pq alone:
✅ Native Podman commands (not manual file operations)
✅ Staged updates with preview
✅ Backup/restore with volumes
✅ Secrets integration
✅ Enhanced validation

### vs BlueBuild module alone:
✅ Works on any Linux (not just immutable)
✅ Standalone RPM (no build-time dependency)
✅ Direct CLI control
✅ Secrets management
✅ Runtime updates

### vs Manual Setec + Podman:
✅ Integrated workflow
✅ Automatic secret injection
✅ Git-based quadlet management
✅ Backup/restore capability
✅ One tool to rule them all

## Technical Highlights

### Single Binary
- Go 1.23+
- Uses Cobra for CLI
- ~5000 lines of code
- Packages as RPM

### Native Podman Commands
- `podman quadlet install` - Installation
- `podman quadlet list` - Listing
- `podman quadlet rm` - Removal
- `podman secret create` - Secret injection

### Setec Client
- Uses official Tailscale/Setec Go library
- Automatic Tailscale authentication
- Background polling for updates
- Automatic secret sync

### File Structure
```
/usr/bin/leger                          # Binary
/usr/lib/systemd/user/leger-daemon.service
/var/lib/leger/
├── staged/                             # Staged updates
├── backups/                            # Backups with volumes
└── manifests/                          # Metadata
~/.config/leger/config.yaml             # User config
```

## Example Workflow

### Install with Secrets

```bash
# 1. Ensure daemon is running
systemctl --user start leger-daemon

# 2. Install quadlet that needs secrets
leger install https://github.com/org/quadlets/tree/main/openwebui

# Output:
# Cloning repo...
# Validating...
# 📋 Secrets required:
#   - leger/openwebui/api-key
# ✓ Secrets prepared
# Installing...
# ✓ Started openwebui.service
```

Behind the scenes:
1. leger CLI detects secrets in `.leger.yaml`
2. leger-daemon fetches from Setec
3. leger-daemon creates Podman secrets
4. leger installs using `podman quadlet install`
5. Quadlet starts with secrets injected

### Safe Update

```bash
# Stage update
leger stage openwebui

# Preview changes
leger diff openwebui
# Shows: Image updated, new env var, port change

# Backup first
leger backup openwebui

# Apply update
leger apply openwebui

# If broken, restore
leger restore openwebui
```

## Implementation Timeline

| Phase | Duration | Deliverable |
|-------|----------|-------------|
| Core CLI | 2 weeks | Install/list/remove with native Podman |
| Metadata & Validation | 1 week | Enhanced validation |
| Staged Updates | 2 weeks | Stage/diff/apply workflow |
| Backup/Restore | 1 week | Full backup with volumes |
| Setec Integration | 2 weeks | Daemon + secrets sync |
| Daemon Service | 1 week | Systemd integration |
| RPM Packaging | 1 week | Fedora package |
| Documentation | 2 weeks | Complete docs |
| **Total** | **12 weeks** | **Production ready** |

## Value Proposition

### For Developers
- GitOps workflow for containers
- Safe updates with preview
- Integrated secrets management
- No manual configuration

### For DevOps
- Single tool for quadlet management
- Backup/restore for disaster recovery
- Audit logs via Setec
- Works everywhere (not just immutable)

### For Fedora Users
- Native RPM package
- Systemd integration
- Rootless by default
- Follows Fedora best practices

## Comparison Matrix

| Feature | pq | BlueBuild | leger |
|---------|-----|-----------|--------|
| Git install | ✅ | ✅ | ✅ |
| Native Podman | ❌ | ❌ | ✅ |
| Staged updates | ❌ | ✅ | ✅ |
| Backup/restore | ❌ | ✅ | ✅ |
| Volume backup | ❌ | ✅ | ✅ |
| Secrets | ❌ | Partial | ✅ Full |
| Validation | Basic | Advanced | Advanced |
| Works everywhere | ✅ | ❌ | ✅ |
| Single binary | ✅ | ❌ | ✅ |
| RPM package | ✅ | ❌ | ✅ |

## Success Metrics

### Code Quality
- [ ] <5000 lines of Go
- [ ] 70% less code than manual operations
- [ ] >80% test coverage

### Functionality
- [ ] All pq features
- [ ] All BlueBuild advanced features
- [ ] Setec integration
- [ ] Works on Fedora 40+

### User Experience
- [ ] Simple CLI
- [ ] Clear error messages
- [ ] Comprehensive documentation
- [ ] Example quadlets

## Conclusion

**leger** combines:
- pq's Git-based quadlet management
- BlueBuild's advanced features (staged updates, backups)
- Setec's secrets management
- Native Podman commands (reducing complexity)

Into a single, cohesive tool that provides:
- ✅ Complete quadlet lifecycle management
- ✅ Safe update workflows
- ✅ Integrated secrets management
- ✅ Disaster recovery capabilities
- ✅ Works everywhere
- ✅ Single Go binary

This is **the** tool for managing Podman Quadlets on Fedora.

---

## Documentation

Complete documentation available:

- **[leger-architecture.md](./leger-architecture.md)** - System architecture and design decisions
- **[leger-implementation.go](./leger-implementation.go)** - Key Go code implementations
- **[leger-usage-guide.md](./leger-usage-guide.md)** - Complete usage examples
- **[leger-roadmap.md](./leger-roadmap.md)** - Detailed implementation plan

## Next Steps

1. **Week 1-2**: Build Phase 1 (Core CLI)
2. **Week 3**: Add validation (Phase 2)
3. **Week 4-5**: Staged updates (Phase 3)
4. **Week 6**: Backup/restore (Phase 4)
5. **Week 7-8**: Setec integration (Phase 5)
6. **Week 9**: Daemon service (Phase 6)
7. **Week 10**: RPM packaging (Phase 7)
8. **Week 11-12**: Documentation & polish (Phase 8)

**Result**: Production-ready leger in 12 weeks!
