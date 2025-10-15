# leger - Podman Quadlet Manager with Secrets Integration

## Architecture Overview

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
         ↓                              ↓
    ┌────────┐                    ┌──────────┐
    │ Podman │←───────────────────│  Setec   │
    │Quadlet │   Podman Secrets   │ Service  │
    └────────┘                    └──────────┘
         ↓                              ↑
         ↓                              │
    ┌────────────┐                 ┌────────────┐
    │  systemd   │                 │ Tailscale  │
    │  Services  │                 │    Auth    │
    └────────────┘                 └────────────┘
```

## Core Components

### 1. CLI Mode (leger)

Replaces pq with native Podman integration + advanced features:

```go
// Main CLI commands
leger install <repo-url>         // Install from Git
leger list [--installed]         // List quadlets
leger remove <name>              // Remove quadlet
leger inspect <name>             // Inspect configuration

// Staged updates (from BlueBuild)
leger stage <name|all>           // Stage updates
leger staged                     // List staged
leger diff <name>                // Preview changes
leger apply <name|all>           // Apply staged
leger discard <name|all>         // Discard staged

// Backup & Restore (from BlueBuild)
leger backup <name|all>          // Create backup
leger backups <name>             // List backups
leger restore <name> [timestamp] // Restore from backup

// Validation
leger validate <name>            // Validate quadlet
leger check-conflicts <name>     // Check port/volume conflicts

// Service management
leger status <name>              // Service status
leger logs <name>                // View logs
leger start/stop/restart <name>  // Service control
```

### 2. Daemon Mode (leger daemon)

Setec integration for secrets management:

```go
leger daemon start               // Start daemon
leger daemon status              // Check daemon status
leger daemon stop                // Stop daemon

// Daemon manages:
// - Setec client connection (via Tailscale)
// - Secret synchronization
// - Podman secrets injection
// - Secret rotation monitoring
```

## Key Architectural Decisions

### 1. Use Native Podman Commands

Based on our earlier analysis, replace all manual operations:

```go
// OLD (pq-style manual operations)
func install(quadletPath string) error {
    copyDir(quadletPath, installDir)
    systemdDaemonReload()
    // ...
}

// NEW (native Podman quadlet commands)
func install(quadletPath string) error {
    args := []string{"quadlet", "install"}
    if isRootless() {
        args = append(args, "--user")
    }
    args = append(args, quadletPath)
    return exec.Command("podman", args...).Run()
}
```

**Benefits:**
- 70% less code
- Better error handling
- Automatic systemd integration
- URL support (can install from HTTP)

### 2. Staged Updates Pattern (from BlueBuild)

```go
type StagingManager struct {
    stagingDir string  // /var/lib/leger/staged/
    backupDir  string  // /var/lib/leger/backups/
}

func (sm *StagingManager) Stage(name string) error {
    // 1. Download latest from Git
    latest := downloadFromGit(repo)
    
    // 2. Validate
    if err := validateQuadlet(latest); err != nil {
        return err
    }
    
    // 3. Stage in preview area
    return stageFiles(latest, sm.stagingDir)
}

func (sm *StagingManager) Apply(name string) error {
    // 1. Backup current
    if err := sm.Backup(name); err != nil {
        return err
    }
    
    // 2. Install staged version
    stagedPath := filepath.Join(sm.stagingDir, name)
    if err := installWithPodman(stagedPath); err != nil {
        return err
    }
    
    // 3. Restart services
    return restartServices(name)
}
```

### 3. Backup with Volume Support (from BlueBuild)

```go
type BackupManager struct {
    backupDir string
}

func (bm *BackupManager) Backup(name string) error {
    timestamp := time.Now().Format("20060102-150405")
    backupPath := filepath.Join(bm.backupDir, name, timestamp)
    
    // 1. Backup quadlet files
    if err := backupQuadletFiles(name, backupPath); err != nil {
        return err
    }
    
    // 2. Backup volumes
    volumes := getQuadletVolumes(name)
    for _, vol := range volumes {
        if err := backupVolume(vol, backupPath); err != nil {
            return err
        }
    }
    
    // 3. Create manifest
    manifest := BackupManifest{
        Timestamp: timestamp,
        Quadlet:   name,
        Volumes:   volumes,
        Services:  getServices(name),
    }
    return saveManifest(manifest, backupPath)
}

func (bm *BackupManager) Restore(name string, timestamp string) error {
    backupPath := filepath.Join(bm.backupDir, name, timestamp)
    
    // 1. Stop services
    stopServices(name)
    
    // 2. Restore quadlet files
    if err := restoreQuadletFiles(backupPath); err != nil {
        return err
    }
    
    // 3. Restore volumes
    manifest := loadManifest(backupPath)
    for _, vol := range manifest.Volumes {
        if err := restoreVolume(vol, backupPath); err != nil {
            return err
        }
    }
    
    // 4. Restart services
    return startServices(name)
}
```

### 4. Setec Integration for Secrets

```go
type SetecDaemon struct {
    client    *setec.Client
    store     *setec.Store
    syncDir   string
    pollInterval time.Duration
}

func (sd *SetecDaemon) Start() error {
    // 1. Initialize Setec client (uses Tailscale auth automatically)
    sd.client = &setec.Client{
        Server: "https://setec.example.ts.net",
    }
    
    // 2. Discover quadlets that need secrets
    quadlets := discoverQuadletsWithSecrets()
    
    // 3. Create store for all needed secrets
    secretNames := extractSecretNames(quadlets)
    sd.store, err = setec.NewStore(context.Background(), setec.StoreConfig{
        Client:       sd.client,
        Secrets:      secretNames,
        PollInterval: sd.pollInterval,
    })
    if err != nil {
        return err
    }
    
    // 4. Sync secrets to Podman
    return sd.syncSecretsToPodman()
}

func (sd *SetecDaemon) syncSecretsToPodman() error {
    for secretName, secret := range sd.store.Secrets() {
        // Create/update Podman secret
        if err := createPodmanSecret(secretName, secret.Get()); err != nil {
            return err
        }
    }
    
    // Watch for updates
    go sd.watchSecretUpdates()
    return nil
}

func (sd *SetecDaemon) watchSecretUpdates() {
    ticker := time.NewTicker(sd.pollInterval)
    for range ticker.C {
        // Setec store auto-updates, just sync changes
        if err := sd.syncSecretsToPodman(); err != nil {
            log.Errorf("Secret sync failed: %v", err)
        }
    }
}
```

### 5. Secret Injection in Quadlet Files

Quadlet files reference Podman secrets:

```ini
# openwebui.container
[Unit]
Description=Open WebUI
After=network-online.target

[Container]
Image=ghcr.io/open-webui/open-webui:main
ContainerName=openwebui
PublishPort=3000:8080

# Secret from Setec via leger daemon
Secret=openwebui-api-key,type=env,target=OPENAI_API_KEY

[Service]
Restart=always

[Install]
WantedBy=default.target
```

The leger daemon ensures `openwebui-api-key` exists in Podman secrets.

## Project Structure

```
leger/
├── cmd/
│   └── leger/
│       └── main.go                 # CLI entry point
│
├── internal/
│   ├── cli/                        # CLI commands
│   │   ├── install.go              # Install from Git
│   │   ├── list.go                 # List quadlets
│   │   ├── remove.go               # Remove quadlets
│   │   ├── stage.go                # Staged updates
│   │   ├── backup.go               # Backup/restore
│   │   └── validate.go             # Validation
│   │
│   ├── daemon/                     # Daemon mode
│   │   ├── setec.go                # Setec client
│   │   ├── sync.go                 # Secret synchronization
│   │   └── podman_secrets.go      # Podman secrets API
│   │
│   ├── podman/                     # Podman integration
│   │   ├── quadlet.go              # Native quadlet commands
│   │   ├── secrets.go              # Secrets management
│   │   └── systemd.go              # Systemd integration
│   │
│   ├── git/                        # Git operations
│   │   ├── clone.go                # Repository cloning
│   │   └── parser.go               # URL parsing
│   │
│   ├── staging/                    # Staged updates
│   │   ├── manager.go              # Staging manager
│   │   ├── diff.go                 # Diff generation
│   │   └── manifest.go             # Staging metadata
│   │
│   ├── backup/                     # Backup/restore
│   │   ├── manager.go              # Backup manager
│   │   ├── volumes.go              # Volume backup
│   │   └── manifest.go             # Backup metadata
│   │
│   └── validation/                 # Validation
│       ├── syntax.go               # Quadlet syntax
│       ├── dependencies.go         # Dependency analysis
│       └── conflicts.go            # Conflict detection
│
├── pkg/
│   └── types/                      # Shared types
│       ├── quadlet.go
│       ├── manifest.go
│       └── config.go
│
├── go.mod
├── go.sum
├── Makefile
└── leger.spec                      # RPM spec
```

## Implementation Phases

### Phase 1: Core CLI (Replace pq with native Podman)
- [ ] Install from Git URLs
- [ ] List/remove using `podman quadlet` commands
- [ ] Basic validation
- [ ] Service management (status/logs/restart)

### Phase 2: Staged Updates
- [ ] Stage command (download + validate)
- [ ] Diff generation
- [ ] Apply/discard commands
- [ ] Staging directory management

### Phase 3: Backup & Restore
- [ ] Backup quadlet files
- [ ] Volume backup
- [ ] Restore with rollback
- [ ] Backup retention

### Phase 4: Setec Integration
- [ ] Daemon mode
- [ ] Setec client integration
- [ ] Podman secrets synchronization
- [ ] Secret rotation handling

### Phase 5: RPM Packaging
- [ ] RPM spec file
- [ ] Systemd unit for daemon
- [ ] Installation scripts
- [ ] Documentation

## Configuration

### CLI Configuration (~/.config/leger/config.yaml)

```yaml
# Git repository defaults
default-repo: https://github.com/rgolangh/podman-quadlets
branch: main

# Installation
install-dir-user: ~/.config/containers/systemd
install-dir-system: /etc/containers/systemd

# Staging & Backups
staging-dir: /var/lib/leger/staged
backup-dir: /var/lib/leger/backups
backup-retention: 7d

# Daemon
daemon:
  setec-server: https://setec.example.ts.net
  poll-interval: 1h
  secret-prefix: leger/
```

### Quadlet Metadata (in Git repo)

```yaml
# .leger.yaml in quadlet directory
name: openwebui
description: Open WebUI for LLM interactions
version: 1.0.0

# Secrets required from Setec
secrets:
  - name: leger/openwebui/api-key
    podman-secret: openwebui-api-key
    env: OPENAI_API_KEY
  
  - name: leger/openwebui/jwt-secret
    podman-secret: openwebui-jwt
    env: WEBUI_SECRET_KEY

# Dependencies
requires:
  - redis
  - postgresql

# Ports
ports:
  - 3000:8080

# Volumes
volumes:
  - openwebui-data:/app/data
```

## Benefits Over Separate Tools

### vs pq alone:
✅ Staged updates with preview
✅ Backup/restore with volumes
✅ Secrets integration
✅ Native Podman commands
✅ Enhanced validation

### vs BlueBuild module alone:
✅ Works on any system (not just immutable)
✅ Standalone RPM installation
✅ Secrets management
✅ Direct CLI control
✅ No build-time dependency

### vs Manual Setec + Podman:
✅ Integrated workflow
✅ Automatic secret injection
✅ Git-based quadlet management
✅ Backup/restore capability
✅ Conflict detection

## Usage Examples

### Install a quadlet with secrets

```bash
# 1. Start leger daemon (manages secrets)
sudo systemctl start leger-daemon

# 2. Install quadlet from Git
leger install https://github.com/myorg/quadlets/tree/main/openwebui

# Leger will:
# - Clone the repo
# - Validate the quadlet
# - Detect secrets needed (.leger.yaml)
# - Fetch secrets from Setec via daemon
# - Inject as Podman secrets
# - Install via `podman quadlet install`
# - Start services
```

### Safe production update

```bash
# Stage the update
leger stage openwebui

# Preview changes
leger diff openwebui

# Backup before applying
leger backup openwebui

# Apply the update
leger apply openwebui

# If something breaks
leger restore openwebui
```

### Daemon monitors secrets

```bash
# Daemon runs in background
# - Polls Setec for secret updates
# - Syncs to Podman secrets
# - Containers pick up changes on restart

# Check daemon status
leger daemon status

# View secret sync logs
journalctl -u leger-daemon
```

## Comparison Matrix

| Feature | pq | BlueBuild Module | leger |
|---------|------|------------------|--------|
| Git-based install | ✅ | ✅ | ✅ |
| Native Podman | ❌ | ❌ | ✅ |
| Staged updates | ❌ | ✅ | ✅ |
| Backup/restore | ❌ | ✅ | ✅ |
| Volume backup | ❌ | ✅ | ✅ |
| Secrets management | ❌ | Partial | ✅ |
| Conflict detection | ❌ | ✅ | ✅ |
| Dependency analysis | ❌ | ✅ | ✅ |
| Standalone RPM | ✅ | ❌ | ✅ |
| Works everywhere | ✅ | ❌ (immutable only) | ✅ |
| Single binary | ✅ | ❌ | ✅ |

## Next Steps

1. **Prototype Phase 1** - Core CLI with native Podman
2. **Add Staging** - Implement staged updates
3. **Add Backups** - Implement backup/restore with volumes
4. **Integrate Setec** - Add daemon mode
5. **Package RPM** - Create RPM for Fedora
6. **Documentation** - Write comprehensive docs
7. **Testing** - Test on Fedora Atomic + regular

This combines the best of all three approaches into a cohesive tool!
