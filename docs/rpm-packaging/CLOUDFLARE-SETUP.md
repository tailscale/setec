# Cloudflare R2 RPM Repository Setup Guide

This guide explains how to set up pkgs.leger.run as a public RPM repository hosted on Cloudflare R2, with automatic deployment from GitHub Actions.

---

## Architecture Overview

```
GitHub Actions (on tag push)
    ↓
Build RPMs (x86_64, aarch64)
    ↓
Sign with GPG
    ↓
Create Repository Metadata (createrepo_c)
    ↓
Upload to Cloudflare R2
    ↓
Purge Cloudflare Cache
    ↓
Users: dnf install leger
```

---

## Prerequisites

- Cloudflare account
- Domain: leger.run (configured in Cloudflare)
- GitHub repository with Actions enabled

---

## Step 1: Create Cloudflare R2 Bucket

### 1.1 Create R2 Bucket

1. Log in to Cloudflare Dashboard
2. Go to **R2 Object Storage**
3. Click **Create bucket**
4. Settings:
   - **Bucket name**: `leger-packages`
   - **Location**: Automatic (or choose closest to your users)
5. Click **Create bucket**

### 1.2 Enable Public Access

1. Go to your bucket **Settings**
2. Scroll to **Public Access**
3. Click **Connect Domain**
4. Enter: `pkgs.leger.run`
5. Cloudflare will automatically create a CNAME record

Your bucket is now publicly accessible at `https://pkgs.leger.run`

---

## Step 2: Create Cloudflare API Token

### 2.1 Create API Token

1. Go to **Profile** → **API Tokens**
2. Click **Create Token**
3. Use template **Edit Cloudflare Workers** or create custom:
   - **Permissions**:
     - Account → R2 → Edit
     - Zone → Cache Purge → Purge
   - **Account Resources**:
     - Include → Your account
   - **Zone Resources**:
     - Include → leger.run
4. Click **Continue to summary**
5. Click **Create Token**
6. **Copy the token** (you won't see it again!)

### 2.2 Get Account and Zone IDs

**Account ID**:
1. Go to R2 → Overview
2. Copy **Account ID** from the right sidebar

**Zone ID**:
1. Go to **Websites** → **leger.run**
2. Scroll down to **API** section
3. Copy **Zone ID**

---

## Step 3: Configure GitHub Secrets

Add these secrets to your GitHub repository:

1. Go to your repo → **Settings** → **Secrets and variables** → **Actions**
2. Click **New repository secret**
3. Add the following secrets:

### Required Secrets

| Secret Name | Value | Description |
|-------------|-------|-------------|
| `CLOUDFLARE_API_TOKEN` | (from Step 2.1) | API token for R2 and cache |
| `CLOUDFLARE_ACCOUNT_ID` | (from Step 2.2) | Your Cloudflare account ID |
| `CLOUDFLARE_ZONE_ID` | (from Step 2.2) | Zone ID for leger.run |

### Optional Secrets (for signing)

| Secret Name | Value | Description |
|-------------|-------|-------------|
| `GPG_PRIVATE_KEY` | (your GPG private key) | For signing RPMs |
| `GPG_PASSPHRASE` | (your GPG passphrase) | GPG key passphrase |

### Variables

1. Go to **Variables** tab
2. Add:

| Variable Name | Value | Description |
|---------------|-------|-------------|
| `ENABLE_SIGNING` | `true` or `false` | Enable RPM signing |

---

## Step 4: Repository Structure

The GitHub Actions workflow creates this structure in R2:

```
leger-packages (R2 bucket)
├── index.html                    # Repository homepage
├── leger.repo                    # Repo configuration file
├── RPM-GPG-KEY-leger            # Public signing key
└── fedora/
    └── 42/
        ├── x86_64/
        │   ├── leger-1.0.0-1.x86_64.rpm
        │   ├── leger-1.0.1-1.x86_64.rpm
        │   └── repodata/
        │       ├── repomd.xml
        │       ├── primary.xml.gz
        │       ├── filelists.xml.gz
        │       └── other.xml.gz
        └── aarch64/
            ├── leger-1.0.0-1.aarch64.rpm
            └── repodata/
                └── ...
```

---

## Step 5: Test the Workflow

### 5.1 Test Manually

1. Go to **Actions** tab in GitHub
2. Select **Release** workflow
3. Click **Run workflow**
4. Enter version: `v0.1.0-test`
5. Click **Run workflow**

### 5.2 Verify Upload

Check these URLs:

- Repository homepage: https://pkgs.leger.run
- Repo file: https://pkgs.leger.run/leger.repo
- x86_64 metadata: https://pkgs.leger.run/fedora/42/x86_64/repodata/repomd.xml
- aarch64 metadata: https://pkgs.leger.run/fedora/42/aarch64/repodata/repomd.xml

### 5.3 Test Installation

On a Fedora 42+ system:

```bash
# Add repository
sudo dnf config-manager --add-repo https://pkgs.leger.run/leger.repo

# Install package
sudo dnf install leger

# Verify
leger --version
legerd --version
```

---

## Step 6: Create Production Release

### 6.1 Push a Tag

```bash
# Create release tag
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

### 6.2 Monitor Release

1. Go to **Actions** tab
2. Watch the **Release** workflow
3. It will:
   - Build RPMs for x86_64 and aarch64
   - Sign them (if enabled)
   - Create repository metadata
   - Upload to Cloudflare R2
   - Purge cache
   - Create GitHub release

### 6.3 Verify Release

```bash
# Update cache
sudo dnf clean metadata

# Install from repository
sudo dnf install leger

# Check version
leger --version  # Should show v1.0.0
```

---

## Step 7: Domain Configuration (Already Done)

If you need to manually configure DNS:

1. Go to **Cloudflare Dashboard** → **leger.run** → **DNS**
2. Add CNAME record:
   - **Type**: CNAME
   - **Name**: pkgs
   - **Target**: (auto-configured by R2 public access)
   - **Proxy status**: Proxied (orange cloud)

The orange cloud enables:
- CDN caching
- DDoS protection
- SSL/TLS
- Analytics

---

## Cost Estimation

### Cloudflare R2 Pricing (as of 2024)

- **Storage**: $0.015/GB/month
- **Class A operations** (write): $4.50/million
- **Class B operations** (read): $0.36/million
- **Egress**: Free (!)

### Example Costs

For a small project:
- **Storage**: 1 GB RPMs = $0.015/month
- **Uploads**: 100 releases/month = ~$0.01/month
- **Downloads**: 10,000 downloads/month = free!

**Total**: ~$0.03/month (essentially free)

---

## Maintenance

### Updating Packages

Packages are automatically updated on every tag push. The workflow:
1. Preserves old versions
2. Updates repository metadata
3. Purges CDN cache

### Removing Old Versions

To clean up old RPMs:

```bash
# Use rclone with R2
rclone ls cloudflare:leger-packages/fedora/42/x86_64/

# Delete specific version
rclone delete cloudflare:leger-packages/fedora/42/x86_64/leger-0.9.0-1.x86_64.rpm

# Regenerate metadata (done automatically in workflow)
```

### Monitoring

Check R2 usage:
1. Go to **R2** → **leger-packages**
2. View **Metrics** tab
3. Monitor:
   - Storage used
   - Requests
   - Bandwidth

---

## Troubleshooting

### Issue: "Failed to download metadata"

**Solution**: Purge Cloudflare cache

```bash
curl -X POST "https://api.cloudflare.com/client/v4/zones/${ZONE_ID}/purge_cache" \
  -H "Authorization: Bearer ${API_TOKEN}" \
  -H "Content-Type: application/json" \
  --data '{"purge_everything":true}'
```

### Issue: "GPG verification failed"

**Solution**: Ensure public key is accessible

```bash
# Test public key
curl https://pkgs.leger.run/RPM-GPG-KEY-leger

# Import manually
sudo rpm --import https://pkgs.leger.run/RPM-GPG-KEY-leger
```

### Issue: "Repository not found"

**Solution**: Check R2 public access

1. Go to R2 bucket settings
2. Verify **Public Access** is enabled
3. Verify domain `pkgs.leger.run` is connected

### Issue: Workflow fails at upload

**Solution**: Check API token permissions

1. Verify token has R2 Edit permissions
2. Verify token hasn't expired
3. Check account ID is correct

---

## Security Best Practices

### 1. GPG Key Management

- Generate dedicated signing key for packages
- Store private key securely in GitHub Secrets
- Publish public key on website
- Set expiration date (2-3 years)
- Document key rotation procedure

### 2. Access Control

- Limit API token to minimum permissions
- Use separate tokens for different purposes
- Rotate tokens annually
- Monitor token usage in Cloudflare dashboard

### 3. Repository Security

- Enable repo_gpgcheck=1 in .repo file
- Sign all packages
- Use HTTPS only (enforced by Cloudflare)
- Monitor for unauthorized changes

### 4. Backup Strategy

- Keep all RPMs in GitHub releases (backup)
- Export R2 bucket periodically
- Document restore procedure

---

## Advanced Configuration

### Custom Cache Rules

Optimize caching for RPM repository:

1. Go to **Cache** → **Cache Rules**
2. Create rule:
   - **Name**: RPM repository caching
   - **If**: `hostname eq "pkgs.leger.run"`
   - **Then**: 
     - Cache everything
     - Edge TTL: 1 hour
     - Browser TTL: 1 hour

### Analytics

Track package downloads:

1. Go to **Analytics & Logs**
2. View:
   - Requests per second
   - Data transfer
   - Cache hit ratio
3. Filter by path to see which packages are popular

### Rate Limiting

Protect against abuse:

1. Go to **Security** → **WAF**
2. Create rate limiting rule:
   - 100 requests per 10 seconds per IP
   - Exclude known CI systems if needed

---

## Alternative: Cloudflare Pages

For static repository metadata only (no large RPM files):

```bash
# Install Wrangler
npm install -g wrangler

# Login
wrangler login

# Deploy
wrangler pages deploy repo/ --project-name=leger-packages
```

This gives you: `https://leger-packages.pages.dev`

---

## Comparison: R2 vs Alternatives

| Solution | Cost | Speed | Setup | CDN |
|----------|------|-------|-------|-----|
| **Cloudflare R2** | ~Free | Fast | Easy | Yes |
| GitHub Releases | Free | Slow | Easy | No |
| COPR | Free | Medium | Medium | No |
| AWS S3 | $$ | Fast | Medium | Requires CloudFront |
| Self-hosted | $ | Varies | Complex | No |

**Verdict**: Cloudflare R2 is the best choice for leger:
- ✅ Virtually free
- ✅ Global CDN
- ✅ Easy automation
- ✅ Professional infrastructure
- ✅ No bandwidth costs

---

## Resources

- [Cloudflare R2 Documentation](https://developers.cloudflare.com/r2/)
- [Wrangler CLI](https://developers.cloudflare.com/workers/wrangler/)
- [RPM Repository Guide](https://docs.fedoraproject.org/en-US/quick-docs/repositories/)
- [createrepo_c Documentation](https://github.com/rpm-software-management/createrepo_c)

---

## Quick Reference

### Useful Commands

```bash
# Test repository access
curl -I https://pkgs.leger.run/leger.repo

# List packages in repository
dnf repoquery --disablerepo=* --enablerepo=leger

# Force metadata refresh
sudo dnf clean metadata && sudo dnf makecache

# Check package signature
rpm -K leger-*.rpm

# Import GPG key
sudo rpm --import https://pkgs.leger.run/RPM-GPG-KEY-leger
```

### URLs

- Repository homepage: https://pkgs.leger.run
- Repository file: https://pkgs.leger.run/leger.repo
- Public GPG key: https://pkgs.leger.run/RPM-GPG-KEY-leger
- x86_64 packages: https://pkgs.leger.run/fedora/42/x86_64/
- aarch64 packages: https://pkgs.leger.run/fedora/42/aarch64/
