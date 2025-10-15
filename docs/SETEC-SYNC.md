# Syncing Upstream Setec Changes

legerd is a minimal fork of [tailscale/setec](https://github.com/tailscale/setec).
This document describes how to merge upstream changes.

## Quarterly Sync Workflow

### 1. Check for Updates

```bash
git fetch upstream
git log HEAD..upstream/main --oneline
```

### 2. Review Changes

```bash
git diff HEAD..upstream/main

# Focus on these directories:
# - cmd/legerd/ (was cmd/setec/)
# - client/
# - server/
# - db/
# - acl/
# - audit/
```

### 3. Merge Upstream

```bash
git checkout main
git merge upstream/main

# Conflicts will likely appear in:
# - cmd/legerd/legerd.go (our renames)
# - README.md (our content)
# - go.mod (if they update dependencies)
```

### 4. Resolve Conflicts

**cmd/legerd/legerd.go:**
- Keep our renames (legerd-dev, legerd-dev.state)
- Accept their functional changes
- Preserve version embedding hook

**README.md:**
- Keep our Leger-focused content
- Note upstream changes in separate upstream-README.md if needed

**go.mod:**
- Accept their dependency updates
- Test thoroughly

### 5. Commit

```bash
git add .
git commit -m "chore(daemon): sync setec upstream to vX.Y.Z

Merged changes from tailscale/setec@<commit-hash>

Changes:
- [list key changes]

Conflicts resolved:
- cmd/legerd/legerd.go: preserved legerd naming
- README.md: kept Leger documentation

All tests passing.
"
```

## When to Merge

✅ **Always merge:**
- Security fixes
- Bug fixes
- Cryptographic library updates
- Database improvements

⚠️ **Review carefully:**
- API changes
- New features
- Breaking changes
- Dependency updates

❌ **Skip or defer:**
- Major refactoring (wait for stable release)
- Features unrelated to Leger's use case
- Changes that conflict with Leger architecture

## Upstream Monitoring

**Subscribe to:**
- https://github.com/tailscale/setec/releases
- https://github.com/tailscale/setec/security/advisories

**Check quarterly:** First week of Jan, Apr, Jul, Oct
