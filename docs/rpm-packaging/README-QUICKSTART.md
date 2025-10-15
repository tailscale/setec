# leger RPM Packaging - Complete Setup Guide

This guide provides everything you need to implement production-ready RPM packaging for leger (CLI + legerd daemon), based on Tailscale's proven approach, with Cloudflare R2 deployment for pkgs.leger.run.

## 📦 What's Included

### Dual Binary Support
- **`leger`** - Interactive CLI for managing quadlets
- **`legerd`** - Background daemon for continuous sync (like tailscaled)

### Core Files
- **`Makefile`** - Build orchestration with version stamping
- **`nfpm-dual.yaml`** - Package configuration for dual binaries
- **`version/version.go`** - Version embedding package

### RPM Scripts (Tailscale Pattern)
- **`release/rpm/postinst.sh`** - Post-install: Creates dirs, systemd preset
- **`release/rpm/prerm.sh`** - Pre-removal: Stops only on uninstall
- **`release/rpm/postrm.sh`** - Post-removal: Restarts on upgrade

### Systemd Services
- **`systemd/legerd.service`** - User-scope unit
- **`systemd/legerd@.service`** - System-scope unit  
- **`systemd/legerd.default`** - Environment file

### Configuration
- **`config/leger.yaml`** - Full configuration with all options

### CI/CD
- **`.github/workflows/release-cloudflare.yml`** - Complete workflow with R2 deployment

### Documentation
- **`docs/RPM-PACKAGING.md`** - Step-by-step implementation
- **`docs/SIGNING.md`** - Package signing guide
- **`docs/CLOUDFLARE-SETUP.md`** - R2 repository setup
- **`RPM-PACKAGING-ANALYSIS.md`** - Tailscale deep dive

## 🚀 Quick Start (15 Minutes)

### 1. Prerequisites

```bash
# Install nfpm
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

# Verify
nfpm --version

# Install createrepo_c (for repository metadata)
sudo dnf install createrepo_c  # Fedora
```

### 2. Project Structure

Ensure your project has this structure:

```
leger/
├── cmd/
│   ├── leger/         # CLI binary
│   │   └── main.go
│   └── legerd/        # Daemon binary
│       └── main.go
├── go.mod
└── ...
```

### 3. Copy Files

```bash
# From outputs directory to your leger project root
cp outputs/Makefile .
cp outputs/nfpm-dual.yaml nfpm.yaml
cp -r outputs/version .
cp -r outputs/release .
cp -r outputs/systemd .
cp -r outputs/config .
cp -r outputs/.github .
cp -r outputs/docs .

# Make scripts executable
chmod +x release/rpm/*.sh
```

### 4. Update Module Path

```bash
# Replace with your actual module path
MODULE="github.com/YOURUSERNAME/leger"

# Update all files
sed -i "s|github.com/yourname/leger|${MODULE}|g" Makefile
sed -i "s|github.com/yourname/leger|${MODULE}|g" .github/workflows/*.yml
```

### 5. Implement Version Package

Add to your code (cmd/leger/main.go and cmd/legerd/main.go):

```go
import "github.com/YOURUSERNAME/leger/version"

func main() {
    if *showVersion {
        fmt.Println(version.Long())
        os.Exit(0)
    }
    // ... your code
}
```

### 6. Test Local Build

```bash
# Build both binaries
make build

# Check versions
./leger --version
./legerd --version

# Build RPM
make rpm

# Output: leger-0.1.0-1.x86_64.rpm
```

### 7. Test Installation

```bash
# Install
sudo dnf install ./leger-*.rpm

# Verify binaries
which leger legerd
leger --version
legerd --version

# Check systemd units
systemctl list-unit-files | grep legerd

# Start service
systemctl --user enable --now legerd.service
systemctl --user status legerd.service

# Uninstall for now
sudo dnf remove leger
```

## 🌐 Cloudflare R2 Setup (10 Minutes)

### Step 1: Create R2 Bucket

1. Log in to [Cloudflare Dashboard](https://dash.cloudflare.com)
2. Go to **R2 Object Storage**
3. Click **Create bucket**
4. Name: `leger-packages`
5. Click **Create bucket**

### Step 2: Enable Public Access

1. Go to bucket **Settings**
2. Click **Connect Domain** under Public Access
3. Enter: `pkgs.leger.run`
4. Cloudflare creates the CNAME automatically

### Step 3: Get API Credentials

**API Token**:
1. Profile → **API Tokens**
2. **Create Token**
3. Permissions:
   - Account → R2 → Edit
   - Zone → Cache Purge → Purge
4. Account Resources: Your account
5. Zone Resources: leger.run
6. **Create Token** → Copy it!

**IDs**:
- **Account ID**: R2 → Overview → Right sidebar
- **Zone ID**: Websites → leger.run → API section

### Step 4: Add GitHub Secrets

Go to your repo → Settings → Secrets and variables → Actions

Add these secrets:
- `CLOUDFLARE_API_TOKEN` - From Step 3
- `CLOUDFLARE_ACCOUNT_ID` - From Step 3
- `CLOUDFLARE_ZONE_ID` - From Step 3

Optional (for signing):
- `GPG_PRIVATE_KEY` - Your GPG private key
- `GPG_PASSPHRASE` - GPG key passphrase

Add variable:
- `ENABLE_SIGNING` = `true` or `false`

### Step 5: Test Workflow

```bash
# Commit all changes
git add .
git commit -m "Add RPM packaging with Cloudflare deployment"
git push

# Test workflow manually
# GitHub → Actions → Release → Run workflow → v0.1.0-test
```

### Step 6: Verify Deployment

Check these URLs:
- https://pkgs.leger.run (repository homepage)
- https://pkgs.leger.run/leger.repo (repo file)
- https://pkgs.leger.run/fedora/42/x86_64/repodata/repomd.xml (metadata)

### Step 7: Test Installation from Repository

On Fedora 42+:

```bash
# Add repository
sudo dnf config-manager --add-repo https://pkgs.leger.run/leger.repo

# Install
sudo dnf install leger

# Verify
leger --version
legerd --version
```

## 📦 First Production Release

### Create Release Tag

```bash
# Create and push tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

### Monitor Release

1. GitHub → Actions → Watch "Release" workflow
2. It will:
   - Build leger + legerd for x86_64 and aarch64
   - Create RPMs
   - Sign them (if enabled)
   - Generate repository metadata
   - Upload to Cloudflare R2
   - Purge CDN cache
   - Create GitHub release

### Verify Release

```bash
# On Fedora 42+ machine
sudo dnf clean metadata
sudo dnf install leger

# Check version
leger --version  # Should show v1.0.0
legerd --version

# Start daemon
systemctl --user enable --now legerd.service
systemctl --user status legerd.service
```

## 🔐 Package Signing (Optional but Recommended)

### Generate GPG Key

```bash
# Generate key
gpg --full-generate-key
# Choose: RSA and RSA, 4096 bits, 2 years expiration
# Real name: leger Package Signing
# Email: packages@leger.run

# Export public key
gpg --export --armor "packages@leger.run" > RPM-GPG-KEY-leger

# Commit public key
git add RPM-GPG-KEY-leger
git commit -m "Add RPM signing public key"
git push
```

### Add to GitHub Secrets

```bash
# Export private key (KEEP SECURE!)
gpg --export-secret-keys --armor "packages@leger.run" > private-key.asc

# Add to GitHub Secrets:
# - GPG_PRIVATE_KEY: contents of private-key.asc
# - GPG_PASSPHRASE: your GPG passphrase

# Securely delete
shred -u private-key.asc
```

### Enable Signing

```bash
# Set variable in GitHub
# Repo → Settings → Variables → ENABLE_SIGNING = true
```

See **[docs/SIGNING.md](docs/SIGNING.md)** for complete guide.

## 📚 Architecture

### Build Flow

```
Git Tag (v1.0.0)
    ↓
GitHub Actions Triggered
    ↓
Build leger + legerd binaries
├── leger-amd64
├── legerd-amd64
├── leger-arm64
└── legerd-arm64
    ↓
Create RPMs with nfpm
├── leger-1.0.0-1.x86_64.rpm
└── leger-1.0.0-1.aarch64.rpm
    ↓
Sign with GPG (optional)
    ↓
Create Repository Metadata
└── repodata/ (createrepo_c)
    ↓
Upload to Cloudflare R2
└── pkgs.leger.run/fedora/42/{x86_64,aarch64}/
    ↓
Purge CDN Cache
    ↓
Create GitHub Release
    ↓
Users Install
└── dnf install leger
```

### Package Contents

After installation:

```
/usr/bin/leger             # CLI tool
/usr/bin/legerd            # Daemon
/etc/leger/config.yaml     # Configuration
/etc/default/legerd        # Environment file
/var/lib/leger/            # State directory
/run/leger/                # Runtime directory (socket)
/usr/lib/systemd/user/legerd.service     # User service
/usr/lib/systemd/system/legerd.service   # System service
```

### Daemon Operation

```
legerd starts
    ↓
Reads /etc/leger/config.yaml
    ↓
Creates socket at /run/leger/legerd.sock
    ↓
Connects to Podman
    ↓
Polls Git repository
    ↓
Downloads and applies Quadlets
    ↓
Connects to Setec for secrets
    ↓
Monitors and auto-updates
```

## 🎯 Tailscale Patterns We Adopted

### 1. Dual Binary Pattern
- **CLI** (leger) for user interaction
- **Daemon** (legerd) for continuous operation
- Both in same package

### 2. Systemd Integration
- Type=notify for daemon awareness
- RuntimeDirectory, StateDirectory management
- Security hardening (NoNewPrivileges, PrivateTmp)
- Environment file for configuration

### 3. RPM Scriptlet Pattern
- **postinst**: Use systemd preset, don't auto-enable
- **prerm**: Only stop on uninstall ($1 == 0)
- **postrm**: Restart on upgrade ($1 >= 1)
- Seamless upgrades (service stays running)

### 4. Version Stamping
- Git tags as source of truth
- Embedded via ldflags at build time
- Multiple formats (short, long, commit, date)

### 5. Package Building
- nfpm library (cross-platform)
- No rpmbuild dependency
- CI-friendly
- Programmatic control

## ⚡ Key Differences from Tailscale

| Feature | Tailscale | leger |
|---------|-----------|-------|
| **Distribution** | pkgs.tailscale.com | Cloudflare R2 (pkgs.leger.run) |
| **Target** | Multi-OS (10+ platforms) | Fedora 42+ only |
| **Binaries** | tailscale + tailscaled | leger + legerd |
| **Signing** | Two-tier Ed25519 | GPG (standard) |
| **Complexity** | Enterprise scale | Focused and streamlined |
| **Cost** | Self-hosted infrastructure | ~$0.03/month (Cloudflare) |

## ✅ Success Checklist

- [ ] Binaries build with version info
- [ ] RPM creates successfully
- [ ] Local install works
- [ ] Systemd units present and working
- [ ] Upgrade preserves running service
- [ ] Uninstall stops service
- [ ] Cloudflare R2 bucket created
- [ ] GitHub secrets configured
- [ ] Workflow runs successfully
- [ ] Repository accessible at pkgs.leger.run
- [ ] Can install: `dnf install leger`
- [ ] Packages signed (optional)
- [ ] Documentation updated

## 🔥 Common Issues

### "nfpm: command not found"
```bash
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Version shows "development"
```bash
git tag v0.1.0
make build
./leger --version
```

### Cloudflare upload fails
- Check API token permissions
- Verify Account ID and Zone ID are correct
- Ensure token hasn't expired

### Repository metadata not found
```bash
# Purge Cloudflare cache
curl -X POST "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/purge_cache" \
  -H "Authorization: Bearer ${API_TOKEN}" \
  --data '{"purge_everything":true}'
```

### Service fails to start
```bash
# Check logs
journalctl --user -u legerd.service -n 50

# Check config
cat /etc/leger/config.yaml

# Check socket
ls -l /run/leger/legerd.sock
```

## 📖 Additional Documentation

- **[docs/RPM-PACKAGING.md](docs/RPM-PACKAGING.md)** - Complete implementation guide
- **[docs/SIGNING.md](docs/SIGNING.md)** - Package signing setup
- **[docs/CLOUDFLARE-SETUP.md](docs/CLOUDFLARE-SETUP.md)** - Detailed R2 setup
- **[RPM-PACKAGING-ANALYSIS.md](RPM-PACKAGING-ANALYSIS.md)** - Tailscale analysis

## 🎉 You're Done!

You now have:
✅ Production-ready RPM packaging  
✅ Dual binary support (CLI + daemon)  
✅ Cloudflare CDN distribution  
✅ Automated CI/CD pipeline  
✅ Professional systemd integration  
✅ Seamless upgrades  
✅ Optional package signing  
✅ Cost-effective infrastructure (~$0.03/month)  

**Happy shipping! 🚀**

---

*Based on Tailscale's production-tested RPM packaging approach, adapted for leger with modern Cloudflare infrastructure.*
