# leger - Complete Usage Guide

## Installation

```bash
# Install RPM
sudo dnf install leger

# Start the secrets daemon
sudo systemctl enable --now leger-daemon

# Verify installation
leger version
```

## Configuration

### ~/.config/leger/config.yaml

```yaml
# Git defaults
default-repo: https://github.com/rgolangh/podman-quadlets
branch: main

# Directories
staging-dir: /var/lib/leger/staged
backup-dir: /var/lib/leger/backups
backup-retention: 7d

# Daemon configuration
daemon:
  setec-server: https://setec.example.ts.net
  poll-interval: 1h
  secret-prefix: leger/
```

## Example 1: Install Open WebUI with Secrets

### Quadlet Repository Structure

```
myorg/quadlets/
└── openwebui/
    ├── .leger.yaml          # Metadata
    ├── openwebui.container  # Main container
    └── openwebui.volume     # Data volume
```

### .leger.yaml

```yaml
name: openwebui
description: Open WebUI for LLM interactions
version: 1.0.0
scope: user

# Secrets from Setec
secrets:
  - name: leger/openwebui/api-key
    podman-secret: openwebui-api-key
    env: OPENAI_API_KEY
  
  - name: leger/openwebui/jwt-secret
    podman-secret: openwebui-jwt
    env: WEBUI_SECRET_KEY

ports:
  - 3000:8080

volumes:
  - openwebui-data:/app/data
```

### openwebui.container

```ini
[Unit]
Description=Open WebUI
After=network-online.target

[Container]
Image=ghcr.io/open-webui/open-webui:main
ContainerName=openwebui
PublishPort=3000:8080
Volume=openwebui.volume:/app/data

# Secrets injected by leger-daemon
Secret=openwebui-api-key,type=env,target=OPENAI_API_KEY
Secret=openwebui-jwt,type=env,target=WEBUI_SECRET_KEY

[Service]
Restart=always

[Install]
WantedBy=default.target
```

### Installation Steps

```bash
# 1. Ensure daemon is running
systemctl --user status leger-daemon
# ● leger-daemon.service - Leger Secrets Daemon
#    Loaded: loaded
#    Active: active (running)

# 2. Install the quadlet
leger install https://github.com/myorg/quadlets/tree/main/openwebui

# Output:
# Cloning https://github.com/myorg/quadlets...
# Validating quadlet...
# Checking for conflicts...
# 📋 Secrets required:
#   - leger/openwebui/api-key
#   - leger/openwebui/jwt-secret
# ✓ Secrets prepared
# Installing openwebui...
# Starting services...
# ✓ Started openwebui.service
#
# ✅ Successfully installed openwebui

# 3. Verify it's running
leger status openwebui

# Output:
# openwebui.service: active (running)
# Ports: 3000:8080
# Secrets: 2 injected
# Uptime: 2 minutes

# 4. Check logs
leger logs openwebui

# 5. Access the UI
firefox http://localhost:3000
```

### What Happened Behind the Scenes

1. **leger install** detected secrets in `.leger.yaml`
2. **leger-daemon** fetched secrets from Setec:
   ```
   GET https://setec.example.ts.net/api/get
   {"Name": "leger/openwebui/api-key"}
   ```
3. **leger-daemon** created Podman secrets:
   ```bash
   podman secret create --user openwebui-api-key -
   podman secret create --user openwebui-jwt -
   ```
4. **leger** installed using native Podman:
   ```bash
   podman quadlet install --user /tmp/openwebui
   ```
5. **systemd** started the service, injecting secrets as environment variables

## Example 2: Staged Update Workflow

### Scenario: OpenWebUI has a security update

```bash
# 1. Stage the update (downloads but doesn't apply)
leger stage openwebui

# Output:
# Fetching latest version from https://github.com/myorg/quadlets...
# Validating...
# ✓ Staged openwebui (1.0.0 → 1.1.0)

# 2. See what's staged
leger staged

# Output:
# NAME       OLD VERSION  NEW VERSION  STAGED AT
# openwebui  1.0.0        1.1.0        2024-10-12 14:30:00

# 3. Preview the changes
leger diff openwebui

# Output:
# Diff for openwebui (1.0.0 → 1.1.0)
# 
# Modified files:
#   openwebui.container
# 
# --- a/openwebui.container
# +++ b/openwebui.container
# @@ -5,7 +5,7 @@
#  [Container]
# -Image=ghcr.io/open-webui/open-webui:v0.1.0
# +Image=ghcr.io/open-webui/open-webui:v0.2.0
#  ContainerName=openwebui
# +Environment=NEW_FEATURE=enabled
#  PublishPort=3000:8080
# 
# New secrets:
#   + leger/openwebui/analytics-key (optional)
# 
# Changes summary:
#   - Image updated to v0.2.0
#   - New environment variable added
#   - Optional analytics secret available

# 4. Backup current version (safety net)
leger backup openwebui

# Output:
# Creating backup of openwebui...
# ✓ Backed up quadlet files
# ✓ Backed up volume: openwebui-data (2.3 GB)
# ✓ Backup saved: 20241012-143500

# 5. Apply the staged update
leger apply openwebui

# Output:
# Applying staged update for openwebui...
# Creating automatic backup...
# ✓ Backup created: 20241012-144000
# Stopping openwebui.service...
# Installing new version...
# ✓ Secrets prepared (including new analytics-key)
# Starting openwebui.service...
# ✓ Applied openwebui (1.0.0 → 1.1.0)

# 6. Verify it works
leger status openwebui

# 7. If something is broken, rollback
# leger restore openwebui 20241012-143500
```

## Example 3: Disaster Recovery

### Scenario: Update broke the application

```bash
# 1. Check what backups exist
leger backups openwebui

# Output:
# Backups for openwebui:
# 
# ID                    VERSION  SIZE    CREATED
# 20241012-144000      1.0.0    2.3 GB  5 minutes ago  (auto-backup)
# 20241012-143500      1.0.0    2.3 GB  10 minutes ago (manual backup)
# 20241010-090000      0.9.0    2.1 GB  2 days ago
# 20241008-120000      0.9.0    2.1 GB  4 days ago

# 2. Restore to the last known good version
leger restore openwebui 20241012-143500

# Output:
# Restoring openwebui from backup 20241012-143500...
# Stopping services...
# ✓ Stopped openwebui.service
# Restoring quadlet files...
# ✓ Restored .container, .volume files
# Restoring volumes...
# ✓ Restored openwebui-data (2.3 GB)
# Installing...
# ✓ Installed via podman quadlet
# Starting services...
# ✓ Started openwebui.service
# 
# ✅ Restored openwebui to version 1.0.0

# 3. Verify it's working
leger status openwebui
leger logs openwebui --lines 50
```

## Example 4: Multi-Container Application

### Scenario: Install a complete stack (web app + database + cache)

### Repository Structure

```
myorg/quadlets/
└── webapp-stack/
    ├── .leger.yaml
    ├── webapp-network.network
    ├── webapp.container
    ├── postgres.container
    ├── redis.container
    ├── postgres-data.volume
    └── redis-data.volume
```

### .leger.yaml

```yaml
name: webapp-stack
description: Complete web application stack
version: 2.0.0
scope: user

secrets:
  - name: leger/webapp/db-password
    podman-secret: webapp-db-password
    env: DATABASE_PASSWORD
  
  - name: leger/webapp/api-key
    podman-secret: webapp-api-key
    env: API_KEY

requires:
  - postgres
  - redis

ports:
  - 8080:8080  # webapp
  - 5432:5432  # postgres (for development)
  - 6379:6379  # redis (for development)

volumes:
  - postgres-data:/var/lib/postgresql/data
  - redis-data:/data
```

### Installation

```bash
# Install the entire stack
leger install https://github.com/myorg/quadlets/tree/main/webapp-stack

# Output:
# Cloning https://github.com/myorg/quadlets...
# Validating quadlet...
# Checking for conflicts...
# 📋 Secrets required:
#   - leger/webapp/db-password
#   - leger/webapp/api-key
# ✓ Secrets prepared
# Installing webapp-stack...
# Starting services...
# ✓ Started webapp-network-network.service
# ✓ Started postgres.service
# ✓ Started redis.service
# ✓ Started webapp.service
#
# ✅ Successfully installed webapp-stack

# Check all services
leger status webapp-stack

# Output:
# webapp-stack services:
#   webapp-network-network.service: active
#   postgres.service: active (running)
#   redis.service: active (running)
#   webapp.service: active (running)
# 
# Ports:
#   8080:8080 (webapp)
#   5432:5432 (postgres)
#   6379:6379 (redis)
# 
# Volumes:
#   postgres-data: 500 MB
#   redis-data: 100 MB
# 
# Secrets: 2 injected
```

## Example 5: Secret Rotation

### Scenario: Rotate API key due to compromise

```bash
# 1. Update secret in Setec
setec -s https://setec.example.ts.net put leger/openwebui/api-key
# Enter secret: ****
# Secret saved as "leger/openwebui/api-key", version 8

# 2. Activate new version
setec -s https://setec.example.ts.net activate leger/openwebui/api-key 8

# 3. leger-daemon detects the change (within poll interval)
# Monitors logs:
journalctl --user -u leger-daemon -f

# Output:
# leger-daemon: Secret updated: leger/openwebui/api-key (v7 → v8)
# leger-daemon: Syncing to Podman secret: openwebui-api-key
# leger-daemon: ✓ Secret synced successfully

# 4. Restart the container to pick up new secret
leger restart openwebui

# Output:
# Restarting openwebui...
# ✓ Stopped openwebui.service
# ✓ Started openwebui.service

# 5. Verify new secret is in use
leger logs openwebui | grep "API"

# Output:
# [INFO] Using API key: sk-...new-key...
```

## Example 6: Conflict Detection

### Scenario: Prevent installing service with port conflict

```bash
# Try to install service that uses port 3000
leger install https://github.com/someone/quadlets/tree/main/another-app

# Output:
# Cloning https://github.com/someone/quadlets...
# Validating quadlet...
# Checking for conflicts...
# ⚠️  Conflicts detected:
#   - Port 3000 already in use by openwebui.service
#   - Volume another-app-data conflicts with existing volume
# 
# ERROR: conflicts must be resolved before install

# Check what's using port 3000
leger check-conflicts --port 3000

# Output:
# Port 3000 usage:
#   openwebui.service: 3000:8080

# Solution: Change port in quadlet or stop conflicting service
```

## Example 7: Development Workflow

### Scenario: Test quadlet locally before committing

```bash
# 1. Create local quadlet
mkdir -p ~/quadlets/myapp
cd ~/quadlets/myapp

# 2. Create files
cat > .leger.yaml <<EOF
name: myapp
version: 0.1.0-dev
scope: user
ports:
  - 9000:8080
EOF

cat > myapp.container <<EOF
[Unit]
Description=My App Development

[Container]
Image=myapp:latest
PublishPort=9000:8080
EOF

# 3. Validate locally
leger validate ~/quadlets/myapp

# Output:
# Validating ~/quadlets/myapp...
# ✓ Syntax valid
# ✓ No dependency issues
# ✓ No conflicts detected
# ✓ Validation passed

# 4. Install from local path
leger install ~/quadlets/myapp

# 5. Test it
curl http://localhost:9000

# 6. Make changes, re-validate, re-install
# ... iterate ...

# 7. Push to Git when ready
cd ~/quadlets/myapp
git init
git add .
git commit -m "Initial commit"
git remote add origin https://github.com/me/quadlets
git push -u origin main

# 8. Now install from Git on production
leger install https://github.com/me/quadlets/tree/main/myapp --scope=system
```

## Example 8: Monitoring and Maintenance

### Regular Maintenance Tasks

```bash
# Check all quadlets status
leger list

# Output:
# NAME           SCOPE   STATUS   VERSION  SERVICES
# openwebui      user    running  1.1.0    1
# webapp-stack   user    running  2.0.0    4
# monitoring     system  running  3.5.0    2

# Check for available updates
leger stage all

# Output:
# Checking for updates...
# ✓ openwebui: no updates
# ✓ webapp-stack: update available (2.0.0 → 2.1.0)
# ✓ monitoring: no updates

# Review and apply updates
leger diff webapp-stack
leger apply webapp-stack

# Create backups
leger backup all

# Output:
# Backing up all quadlets...
# ✓ openwebui: 20241012-150000
# ✓ webapp-stack: 20241012-150030
# ✓ monitoring: 20241012-150100

# Clean up old backups (older than 7 days)
find /var/lib/leger/backups -type d -mtime +7 -exec rm -rf {} \;
```

## Example 9: System-Wide Services

### Scenario: Install monitoring stack for all users

```bash
# Install as system service (requires root)
sudo leger install https://github.com/org/quadlets/tree/main/monitoring \
  --scope=system

# Output:
# Cloning https://github.com/org/quadlets...
# Validating quadlet...
# Installing monitoring (system scope)...
# Starting services...
# ✓ Started prometheus.service
# ✓ Started grafana.service
#
# ✅ Successfully installed monitoring

# Check status (requires root)
sudo leger status monitoring

# All users can access
firefox http://localhost:3001  # Grafana
```

## Daemon Management

```bash
# Check daemon status
systemctl --user status leger-daemon

# View daemon logs
journalctl --user -u leger-daemon -f

# Stop daemon (stops secret synchronization)
systemctl --user stop leger-daemon

# Start daemon
systemctl --user start leger-daemon

# Restart daemon (re-syncs all secrets)
systemctl --user restart leger-daemon
```

## Troubleshooting

### Problem: Service won't start

```bash
# Check detailed status
leger status myapp

# View logs
leger logs myapp --lines 100

# Validate configuration
leger validate myapp

# Check for conflicts
leger check-conflicts myapp
```

### Problem: Secrets not working

```bash
# Check daemon is running
systemctl --user status leger-daemon

# Check daemon logs
journalctl --user -u leger-daemon

# Verify secret exists in Podman
podman secret ls --user | grep myapp

# Test Setec connection
setec -s https://setec.example.ts.net list
```

### Problem: Update failed

```bash
# Check what's staged
leger staged

# Discard bad update
leger discard myapp

# Restore from backup
leger backups myapp
leger restore myapp <backup-id>
```

## Best Practices

1. **Always stage updates first**
   ```bash
   leger stage all
   leger diff <name>
   leger apply <name>
   ```

2. **Backup before major changes**
   ```bash
   leger backup all
   ```

3. **Use user scope for personal services**
   ```yaml
   scope: user  # in .leger.yaml
   ```

4. **Use system scope for shared services**
   ```yaml
   scope: system  # requires root
   ```

5. **Keep backups for at least a week**
   ```yaml
   backup-retention: 7d  # in config.yaml
   ```

6. **Monitor daemon logs regularly**
   ```bash
   journalctl --user -u leger-daemon -f
   ```

7. **Validate before committing**
   ```bash
   leger validate ./myquadlet
   ```

8. **Use descriptive metadata**
   ```yaml
   # Good .leger.yaml
   name: myapp
   description: Clear description of what this does
   version: 1.0.0
   ```

## Integration with Other Tools

### With systemd

```bash
# Use systemd commands directly
systemctl --user status openwebui
systemctl --user restart openwebui
journalctl --user -u openwebui
```

### With Podman

```bash
# Inspect containers
podman ps --user
podman inspect systemd-openwebui

# Check secrets
podman secret ls --user
```

### With Tailscale

```bash
# Setec uses Tailscale auth automatically
# Just ensure you're logged in
tailscale status
```

This comprehensive guide shows how **leger** provides a complete solution for managing Podman Quadlets with integrated secrets management, all in a single tool!
