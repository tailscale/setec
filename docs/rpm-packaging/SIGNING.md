# Package Signing Guide for leger

This document explains how to set up and use package signing for leger RPMs, inspired by Tailscale's signing infrastructure.

## Overview

Package signing ensures users can verify that RPMs are authentic and haven't been tampered with. leger supports two signing approaches:

1. **GPG Signing** (Recommended for initial releases) - Standard RPM signing
2. **Ed25519 distsign** (Future) - Tailscale's advanced two-tier signing system

---

## Approach 1: GPG Signing (Start Here)

### 1.1 Generate GPG Key (One-time Setup)

```bash
# Generate a new GPG key for package signing
gpg --full-generate-key

# Choose:
#  - Key type: (1) RSA and RSA
#  - Key size: 4096 bits
#  - Expiration: 0 (does not expire) or set an expiration date
#  - Real name: leger Package Signing
#  - Email: packages@yourdomain.com
#  - Comment: (optional)
```

### 1.2 Export Public Key

```bash
# List your keys to find the key ID
gpg --list-keys

# Export the public key
gpg --export --armor "packages@yourdomain.com" > RPM-GPG-KEY-leger

# Publish this file so users can import it
# Options:
#  - GitHub repository (in docs/ or root)
#  - Your website
#  - Public keyserver: gpg --send-keys <KEY_ID>
```

### 1.3 Sign RPMs Locally

```bash
# Sign a single RPM
rpmsign --addsign --key-id=<YOUR_KEY_ID> leger-*.rpm

# Or use the Makefile target
make sign GPG_KEY=packages@yourdomain.com

# Verify the signature
rpm --checksig leger-*.rpm
# Should show: leger-1.0.0-1.amd64.rpm: digests signatures OK
```

### 1.4 Set Up CI Signing (GitHub Actions)

**Step 1: Export Private Key**

```bash
# Export private key (KEEP THIS SECURE!)
gpg --export-secret-keys --armor "packages@yourdomain.com" > private-key.asc

# Important: This file contains your private signing key
# Never commit it to git!
# Store it securely and delete after uploading to GitHub secrets
```

**Step 2: Add to GitHub Secrets**

1. Go to your repository on GitHub
2. Navigate to Settings → Secrets and variables → Actions
3. Click "New repository secret"
4. Add two secrets:
   - Name: `GPG_PRIVATE_KEY`, Value: (paste contents of private-key.asc)
   - Name: `GPG_PASSPHRASE`, Value: (your GPG key passphrase)

**Step 3: Securely Delete Private Key File**

```bash
# Overwrite and delete the file
shred -u private-key.asc

# Or on macOS
rm -P private-key.asc
```

**Step 4: Update GitHub Actions Workflow**

Add this job to `.github/workflows/release.yml`:

```yaml
sign:
  name: Sign RPM Packages
  needs: build
  runs-on: ubuntu-latest
  steps:
    - name: Download artifacts
      uses: actions/download-artifact@v4
      with:
        path: artifacts
    
    - name: Import GPG key
      run: |
        echo "${{ secrets.GPG_PRIVATE_KEY }}" | gpg --batch --import
    
    - name: Sign RPMs
      env:
        GPG_PASSPHRASE: ${{ secrets.GPG_PASSPHRASE }}
      run: |
        # Create gpg-agent config for non-interactive signing
        echo "allow-loopback-pinentry" > ~/.gnupg/gpg-agent.conf
        gpg-connect-agent reloadagent /bye
        
        # Sign all RPMs
        for rpm in artifacts/rpm-*/*.rpm; do
          echo "Signing $rpm"
          echo "$GPG_PASSPHRASE" | \
            rpmsign --addsign \
            --define="_gpg_name packages@yourdomain.com" \
            --define="__gpg_sign_cmd %{__gpg} gpg --batch --pinentry-mode loopback --passphrase-fd 3 --no-verbose --no-armor --no-secmem-warning -u '%{_gpg_name}' -sbo %{__signature_filename} %{__plaintext_filename}" \
            "$rpm" 3<&0
        done
    
    - name: Upload signed RPMs
      uses: actions/upload-artifact@v4
      with:
        name: signed-rpms
        path: artifacts/rpm-*/*.rpm
```

### 1.5 User Verification

**For Users: How to Verify Signed Packages**

```bash
# Import the public key
curl -O https://github.com/yourname/leger/raw/main/RPM-GPG-KEY-leger
sudo rpm --import RPM-GPG-KEY-leger

# Verify the signature
rpm --checksig leger-*.rpm

# Should show: leger-1.0.0-1.amd64.rpm: digests signatures OK
```

Add this to your installation documentation.

---

## Approach 2: Ed25519 distsign (Advanced - Future)

Tailscale's two-tier signing system is more sophisticated and enables key rotation without recompiling clients. Adopt this approach when:

- You have multiple releases
- You want to rotate signing keys without updating clients
- You need very high security

### 2.1 Architecture

```
Root Keys (offline, embedded in binary)
    ↓ signs
Signing Keys (online, dynamically fetched)
    ↓ signs
Packages (leger-*.rpm)
```

### 2.2 Implementation Steps

**Step 1: Copy distsign Package**

```bash
# Copy from Tailscale repository
cp -r tailscale/clientupdate/distsign ./internal/distsign

# Update import paths
find ./internal/distsign -type f -name "*.go" \
  -exec sed -i 's|tailscale.com/|github.com/yourname/leger/internal/|g' {} \;
```

**Step 2: Generate Keys**

```bash
# Generate root key (KEEP OFFLINE!)
go run ./cmd/distsign generate-root

# Output:
# Private: root-private.pem (NEVER COMMIT!)
# Public:  root-public.pem (embed in binary)

# Generate signing key
go run ./cmd/distsign generate-signing

# Output:
# Private: signing-private.pem (store in CI secrets)
# Public:  signing-public.pem (publish to pkgs server)
```

**Step 3: Sign Signing Key with Root**

```bash
# This proves signing key is authorized
go run ./cmd/distsign sign-signing-key \
  --root root-private.pem \
  signing-public.pem > signing-public.pem.sig
```

**Step 4: Embed Root Public Key**

Create `internal/distsign/roots/leger-root.pem`:
```pem
-----BEGIN ROOT PUBLIC KEY-----
<base64 encoded public key>
-----END ROOT PUBLIC KEY-----
```

**Step 5: Sign Packages**

```bash
# Sign RPM
go run ./cmd/distsign sign-package \
  --key signing-private.pem \
  leger-1.0.0-1.amd64.rpm > leger-1.0.0-1.amd64.rpm.sig
```

**Step 6: Verify in Client**

```go
import "github.com/yourname/leger/internal/distsign"

client, err := distsign.NewClient(logf, "https://releases.yourdomain.com")
err = client.Download(ctx, "leger-1.0.0-1.amd64.rpm", "/tmp/leger.rpm")
// Automatically verifies signature
```

### 2.3 Key Rotation

**Rotating Signing Keys** (no client update needed):
```bash
# Generate new signing key
go run ./cmd/distsign generate-signing

# Sign with root
go run ./cmd/distsign sign-signing-key \
  --root root-private.pem \
  new-signing-public.pem > new-signing-public.pem.sig

# Publish both old and new keys together
# After grace period, remove old key
```

**Rotating Root Keys** (requires client recompile):
```bash
# Generate new root
# Embed in code
# Release new client version
# Old clients can still verify with old root
# Eventually deprecate old root
```

---

## Comparison: GPG vs distsign

| Feature | GPG Signing | distsign |
|---------|-------------|----------|
| **Setup Complexity** | Simple | Complex |
| **Key Rotation** | Manual, disruptive | Automatic, seamless |
| **Crypto** | RSA 4096 | Ed25519 |
| **Verification** | rpm --checksig | Custom client code |
| **Industry Standard** | Yes (RPM standard) | No (Tailscale only) |
| **Client Updates** | Not needed | Needed for root rotation |
| **When to Use** | Initial releases | Production at scale |

---

## Security Best Practices

### Key Management

1. **Root keys** (distsign only):
   - Generate offline (air-gapped machine)
   - Store in hardware security module (HSM) or password manager
   - Never store in CI/CD
   - Use multiple root keys in different physical locations

2. **Signing keys**:
   - Store in GitHub secrets or vault
   - Rotate annually
   - Monitor for unauthorized use
   - Revoke immediately if compromised

3. **GPG keys**:
   - Use strong passphrase
   - Set expiration date (e.g., 2 years)
   - Back up to secure location
   - Publish revocation certificate

### Operational Security

1. **Access Control**:
   - Limit who can access signing keys
   - Use least privilege principle
   - Audit access logs

2. **Automation**:
   - Sign in CI/CD, not locally
   - Never commit private keys to git
   - Use ephemeral environments for signing

3. **Monitoring**:
   - Alert on signing failures
   - Log all signing operations
   - Review signatures periodically

### Incident Response

If a signing key is compromised:

1. **Immediate**:
   - Revoke the compromised key
   - Generate new signing key
   - Re-sign all packages
   - Notify users

2. **Communication**:
   - Publish security advisory
   - Provide key revocation info
   - Update documentation

3. **Recovery**:
   - Investigate how compromise occurred
   - Improve security controls
   - Consider security audit

---

## Recommended Timeline

### Phase 1: MVP (Week 1)
- ✅ Generate GPG key
- ✅ Sign packages locally
- ✅ Test verification

### Phase 2: Automation (Week 2-3)
- ✅ Set up GitHub Actions signing
- ✅ Publish public key
- ✅ Document verification for users

### Phase 3: Polish (Month 2)
- ✅ Automate key rotation
- ✅ Set up monitoring
- ✅ Security audit

### Phase 4: Advanced (Quarter 2)
- 🔲 Evaluate distsign adoption
- 🔲 Implement if needed
- 🔲 Migrate users

---

## Resources

- [RPM Packaging Guide - Signing](https://rpm-packaging-guide.github.io/#signing-packages)
- [Fedora Package Signing](https://docs.fedoraproject.org/en-US/package-maintainers/Package_Signing/)
- [Tailscale distsign Source](https://github.com/tailscale/tailscale/tree/main/clientupdate/distsign)
- [Ed25519 Signing](https://ed25519.cr.yp.to/)

---

## Quick Reference

```bash
# Generate GPG key
gpg --full-generate-key

# Export public key
gpg --export --armor "email@example.com" > RPM-GPG-KEY-leger

# Sign RPM
rpmsign --addsign --key-id=<KEY_ID> leger-*.rpm

# Verify signature
rpm --checksig leger-*.rpm

# Import public key (user)
sudo rpm --import RPM-GPG-KEY-leger
```
