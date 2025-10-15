# leger - Implementation Roadmap

## Project Goals

Build a single Go binary that:
✅ Manages Podman Quadlets via Git repositories
✅ Uses native Podman commands (not manual file operations)
✅ Provides staged updates with preview
✅ Supports backup/restore with full volume support
✅ Integrates with Setec for secrets management
✅ Packages as RPM for Fedora

## Development Phases

### Phase 1: Core CLI Foundation (Week 1-2)

**Goal**: Replace pq with modern implementation using native Podman

#### Tasks

1. **Project Setup**
   - [x] Create Go module structure
   - [ ] Set up Cobra for CLI
   - [ ] Add Makefile for builds
   - [ ] Initialize Git repository

2. **Git Integration** (`internal/git/`)
   - [ ] URL parser (GitHub/GitLab/generic)
   - [ ] Clone with sparse checkout
   - [ ] Branch support
   - [ ] Subdirectory extraction

3. **Native Podman Integration** (`internal/podman/`)
   - [ ] `podman quadlet install` wrapper
   - [ ] `podman quadlet list` wrapper
   - [ ] `podman quadlet rm` wrapper
   - [ ] `podman quadlet print` wrapper
   - [ ] Detect Podman version
   - [ ] Handle --user vs system scope

4. **Basic CLI Commands**
   - [ ] `leger install <repo-url>`
   - [ ] `leger list [--installed]`
   - [ ] `leger remove <n>`
   - [ ] `leger inspect <n>`

5. **Service Management** (`internal/podman/systemd.go`)
   - [ ] `systemctl status` wrapper
   - [ ] `systemctl start/stop/restart` wrapper
   - [ ] `journalctl` wrapper for logs
   - [ ] Service discovery

6. **Basic Validation** (`internal/validation/`)
   - [ ] Quadlet syntax validation
   - [ ] Required section checks
   - [ ] Basic port conflict detection

**Deliverables:**
- Working CLI that can install/list/remove quadlets
- Uses native Podman commands (no manual file operations)
- ~70% less code than original pq

**Testing:**
```bash
# Should work
leger install https://github.com/rgolangh/podman-quadlets/tree/main/nginx
leger list --installed
leger status nginx
leger logs nginx
leger remove nginx
```

---

### Phase 2: Metadata & Validation (Week 3)

**Goal**: Add structured metadata and advanced validation

#### Tasks

1. **Metadata System** (`pkg/types/`)
   - [ ] Define `.leger.yaml` schema
   - [ ] YAML parser
   - [ ] Metadata storage/retrieval
   - [ ] Git source tracking

2. **Enhanced Validation** (`internal/validation/`)
   - [ ] Dependency parsing
   - [ ] Circular dependency detection
   - [ ] Port conflict detection (check running services)
   - [ ] Volume conflict detection
   - [ ] Security warnings (privileged, host network, etc.)

3. **CLI Commands**
   - [ ] `leger validate <n>`
   - [ ] `leger check-conflicts [name]`
   - [ ] Enhanced `leger inspect` with metadata

**Deliverables:**
- Quadlets can declare metadata in `.leger.yaml`
- Comprehensive validation before install
- Conflict detection prevents problems

**Testing:**
```bash
# Create test quadlet with conflicts
leger validate ./test-quadlet
leger check-conflicts --port 8080
leger install ./test-quadlet  # Should fail gracefully
```

---

### Phase 3: Staged Updates (Week 4-5)

**Goal**: Implement safe staged updates with preview

#### Tasks

1. **Staging Manager** (`internal/staging/`)
   - [ ] Staging directory management
   - [ ] Download to staging area
   - [ ] Manifest creation
   - [ ] Diff generation

2. **Diff Implementation** (`internal/staging/diff.go`)
   - [ ] File-by-file diff
   - [ ] Unified diff format
   - [ ] Summary of changes
   - [ ] Highlight important changes

3. **Apply/Discard Logic** (`internal/staging/manager.go`)
   - [ ] Apply staged updates
   - [ ] Discard staging
   - [ ] Automatic backup before apply
   - [ ] Service restart management

4. **CLI Commands**
   - [ ] `leger stage <name|all>`
   - [ ] `leger staged`
   - [ ] `leger diff <n>`
   - [ ] `leger apply <name|all>`
   - [ ] `leger discard <name|all>`

**Deliverables:**
- Stage updates without applying
- Preview changes before applying
- Safe update workflow

**Testing:**
```bash
# Workflow test
leger install https://github.com/me/quadlets/tree/v1.0/myapp
leger stage myapp                    # Downloads v1.1
leger diff myapp                     # Shows changes
leger apply myapp                    # Applies update
```

---

### Phase 4: Backup & Restore (Week 6)

**Goal**: Implement comprehensive backup/restore with volumes

#### Tasks

1. **Backup Manager** (`internal/backup/`)
   - [ ] Backup directory management
   - [ ] Quadlet file backup
   - [ ] Volume backup via `podman volume export`
   - [ ] Manifest creation
   - [ ] Timestamp management

2. **Restore Logic** (`internal/backup/restore.go`)
   - [ ] Service stop
   - [ ] File restore
   - [ ] Volume restore via `podman volume import`
   - [ ] Service restart
   - [ ] Rollback on failure

3. **Retention Management**
   - [ ] Automatic backup before apply
   - [ ] Retention policy
   - [ ] Cleanup old backups

4. **CLI Commands**
   - [ ] `leger backup <name|all>`
   - [ ] `leger backups [name]`
   - [ ] `leger restore <n> [timestamp]`

**Deliverables:**
- Full backup including volumes
- Point-in-time restore
- Automatic backups before updates

**Testing:**
```bash
# Backup/restore test
leger backup myapp
leger list backups myapp
leger restore myapp 20241012-120000

# Update with automatic backup
leger apply myapp  # Creates backup automatically
```

---

### Phase 5: Setec Integration - Client Side (Week 7-8)

**Goal**: Integrate Setec client for secrets management

#### Tasks

1. **Setec Client** (`internal/daemon/setec.go`)
   - [ ] Initialize Setec client
   - [ ] Create secret store
   - [ ] Poll for updates
   - [ ] Handle Tailscale auth

2. **Secret Discovery** (`internal/daemon/discovery.go`)
   - [ ] Parse `.leger.yaml` for secrets
   - [ ] Extract secret requirements
   - [ ] Build secret list

3. **Podman Secrets Integration** (`internal/podman/secrets.go`)
   - [ ] `podman secret create` wrapper
   - [ ] `podman secret rm` wrapper
   - [ ] Secret exists check
   - [ ] Update secrets atomically

4. **Daemon Mode** (`cmd/leger/daemon.go`)
   - [ ] Daemon command
   - [ ] Background service
   - [ ] Signal handling
   - [ ] Health checks

**Deliverables:**
- leger-daemon fetches secrets from Setec
- Syncs to Podman secrets
- Handles secret rotation

**Testing:**
```bash
# Manual testing (requires Setec server)
leger daemon start
leger install <quadlet-with-secrets>
# Verify secrets in container
podman exec myapp env | grep SECRET
```

---

### Phase 6: Daemon Service & Systemd (Week 9)

**Goal**: Package daemon as systemd service

#### Tasks

1. **Systemd Unit Files**
   - [ ] leger-daemon.service (user)
   - [ ] leger-daemon.service (system)
   - [ ] Auto-start on boot
   - [ ] Restart on failure

2. **Daemon Management** (`cmd/leger/daemon.go`)
   - [ ] `leger daemon start`
   - [ ] `leger daemon stop`
   - [ ] `leger daemon status`
   - [ ] `leger daemon restart`

3. **Integration**
   - [ ] Check daemon before install (if secrets needed)
   - [ ] Notify daemon on quadlet install
   - [ ] Daemon discovers new quadlets

**Deliverables:**
- Systemd service for leger-daemon
- Automatic secret synchronization
- CLI commands for daemon management

**Testing:**
```bash
# System integration test
systemctl --user enable --now leger-daemon
systemctl --user status leger-daemon
leger install <quadlet-with-secrets>
journalctl --user -u leger-daemon -f
```

---

### Phase 7: RPM Packaging (Week 10)

**Goal**: Create RPM package for Fedora

#### Tasks

1. **RPM Spec File** (`leger.spec`)
   - [ ] Package metadata
   - [ ] Build instructions
   - [ ] Install scripts
   - [ ] File list
   - [ ] Dependencies

2. **Installation Scripts**
   - [ ] Post-install script
   - [ ] Pre-remove script
   - [ ] User creation (if needed)
   - [ ] Directory creation

3. **Systemd Integration**
   - [ ] Install systemd units
   - [ ] Enable/disable logic
   - [ ] Reload daemon

4. **Build Automation** (`Makefile`)
   - [ ] `make rpm` target
   - [ ] Version from git tags
   - [ ] GoReleaser integration

**Deliverables:**
- Working RPM package
- Installs on Fedora
- Systemd service configured

**Testing:**
```bash
# Build and install
make rpm
sudo dnf install ./dist/leger-*.rpm

# Verify installation
leger version
systemctl --user status leger-daemon
leger install <test-quadlet>
```

---

### Phase 8: Documentation & Polish (Week 11-12)

**Goal**: Complete documentation and polish

#### Tasks

1. **User Documentation**
   - [ ] README.md
   - [ ] INSTALLATION.md
   - [ ] USAGE.md
   - [ ] TROUBLESHOOTING.md
   - [ ] Man pages

2. **Developer Documentation**
   - [ ] ARCHITECTURE.md
   - [ ] CONTRIBUTING.md
   - [ ] API documentation
   - [ ] Code comments

3. **Example Quadlets**
   - [ ] Simple nginx example
   - [ ] Multi-container stack
   - [ ] Secrets integration example
   - [ ] Various .leger.yaml examples

4. **Testing**
   - [ ] Unit tests
   - [ ] Integration tests
   - [ ] End-to-end tests
   - [ ] Test on different Fedora versions

5. **Polish**
   - [ ] Error message improvements
   - [ ] Progress indicators
   - [ ] Colored output
   - [ ] Shell completions

**Deliverables:**
- Complete documentation
- Example quadlets
- Test coverage
- Polished CLI

---

## Technical Stack

### Core Dependencies

```go
// go.mod
module github.com/yourorg/leger

go 1.23

require (
    github.com/spf13/cobra v1.9.1           // CLI framework
    github.com/spf13/viper v1.20.1          // Configuration
    github.com/go-git/go-git/v6 v6.0.0      // Git operations
    github.com/tailscale/setec v1.0.0       // Setec client
    gopkg.in/yaml.v3 v3.0.1                 // YAML parsing
    github.com/google/go-cmp v0.6.0         // Diff generation
)
```

### Build Tools

- **GoReleaser**: Automated releases
- **rpmbuild**: RPM packaging
- **Make**: Build automation
- **golangci-lint**: Code linting

---

## Project Structure (Final)

```
leger/
├── cmd/
│   └── leger/
│       ├── main.go
│       └── daemon.go
│
├── internal/
│   ├── cli/                    # CLI commands
│   │   ├── install.go
│   │   ├── list.go
│   │   ├── remove.go
│   │   ├── inspect.go
│   │   ├── stage.go
│   │   ├── apply.go
│   │   ├── backup.go
│   │   ├── restore.go
│   │   ├── validate.go
│   │   ├── status.go
│   │   └── logs.go
│   │
│   ├── daemon/                 # Daemon mode
│   │   ├── daemon.go
│   │   ├── setec.go
│   │   ├── sync.go
│   │   └── discovery.go
│   │
│   ├── git/                    # Git operations
│   │   ├── clone.go
│   │   ├── parser.go
│   │   └── types.go
│   │
│   ├── podman/                 # Podman integration
│   │   ├── quadlet.go
│   │   ├── secrets.go
│   │   ├── systemd.go
│   │   └── volumes.go
│   │
│   ├── staging/                # Staged updates
│   │   ├── manager.go
│   │   ├── diff.go
│   │   └── manifest.go
│   │
│   ├── backup/                 # Backup/restore
│   │   ├── manager.go
│   │   ├── volumes.go
│   │   ├── restore.go
│   │   └── manifest.go
│   │
│   └── validation/             # Validation
│       ├── syntax.go
│       ├── dependencies.go
│       └── conflicts.go
│
├── pkg/
│   └── types/                  # Shared types
│       ├── quadlet.go
│       ├── metadata.go
│       ├── manifest.go
│       └── config.go
│
├── systemd/                    # Systemd units
│   ├── leger-daemon.service
│   └── leger-daemon@.service
│
├── examples/                   # Example quadlets
│   ├── nginx/
│   ├── webapp-stack/
│   └── with-secrets/
│
├── docs/                       # Documentation
│   ├── README.md
│   ├── INSTALLATION.md
│   ├── USAGE.md
│   ├── ARCHITECTURE.md
│   └── TROUBLESHOOTING.md
│
├── Makefile
├── leger.spec
├── .goreleaser.yaml
├── go.mod
└── go.sum
```

---

## Success Criteria

### Phase 1-2 Success
- [ ] Can install quadlets from Git
- [ ] Uses native Podman commands
- [ ] Basic validation works
- [ ] Service management works

### Phase 3-4 Success
- [ ] Can stage updates
- [ ] Can preview changes
- [ ] Can backup/restore
- [ ] Volumes included in backups

### Phase 5-6 Success
- [ ] Daemon fetches secrets from Setec
- [ ] Secrets sync to Podman
- [ ] Quadlets can use secrets
- [ ] Secret rotation works

### Phase 7-8 Success
- [ ] RPM installs cleanly
- [ ] Systemd service works
- [ ] Documentation complete
- [ ] Examples work

### Final Success
- [ ] Single Go binary
- [ ] Works on Fedora (Atomic & regular)
- [ ] Integrates with Setec
- [ ] Better than pq + BlueBuild combined
- [ ] <5000 lines of Go code

---

## Development Environment

### Required Software

```bash
# Fedora
sudo dnf install golang podman git rpm-build rpmdevtools

# Setup
go version  # 1.23+
podman version  # 5.0+
git version
rpmbuild --version
```

### Development Workflow

```bash
# Clone
git clone https://github.com/yourorg/leger
cd leger

# Build
make build

# Test locally
./bin/leger version
./bin/leger install <test-quadlet>

# Run tests
make test

# Build RPM
make rpm

# Install locally
sudo dnf install ./dist/leger-*.rpm
```

---

## Release Process

1. **Development**
   - Work on feature branch
   - Write tests
   - Update documentation

2. **Testing**
   - Run full test suite
   - Test on Fedora
   - Test with real quadlets

3. **Release**
   - Tag version: `git tag v1.0.0`
   - Push tag: `git push origin v1.0.0`
   - GoReleaser builds binaries + RPM
   - Publish release notes

4. **Distribution**
   - COPR repository for Fedora
   - GitHub releases
   - Documentation update

---

## Timeline Summary

| Phase | Duration | Deliverable |
|-------|----------|-------------|
| 1 | 2 weeks | Core CLI with native Podman |
| 2 | 1 week | Metadata & validation |
| 3 | 2 weeks | Staged updates |
| 4 | 1 week | Backup/restore |
| 5 | 2 weeks | Setec integration |
| 6 | 1 week | Daemon service |
| 7 | 1 week | RPM packaging |
| 8 | 2 weeks | Documentation & polish |
| **Total** | **12 weeks** | **Complete tool** |

---

## Risk Mitigation

### Risk: Podman quadlet commands not stable
**Mitigation**: Feature detection, fallback to manual operations

### Risk: Setec integration complex
**Mitigation**: Phase 5 can be delayed, core tool works without it

### Risk: Volume backup too slow
**Mitigation**: Make backup optional, warn about size

### Risk: RPM packaging issues
**Mitigation**: Test early on multiple Fedora versions

---

## Next Steps

1. **Immediate**: Start Phase 1 - Core CLI
2. **Week 1**: Have basic install/list/remove working
3. **Week 2**: Complete Phase 1, start Phase 2
4. **Weekly**: Review progress, adjust timeline

This roadmap provides a clear path to building leger while maintaining focus on delivering value incrementally!
