# Contributing to Leger

Thank you for your interest in contributing to Leger! This guide explains our development workflow and conventions.

## 🚀 Quick Start

1. **Fork and clone** the repository
2. **Create a branch** following our naming convention
3. **Make your changes** with conventional commits
4. **Open a PR** with a descriptive title
5. **Address review feedback**
6. **Celebrate** when merged! 🎉

## 📋 Branch Naming Convention

We use issue-based branch naming to maintain traceability:

```bash
<type>/<issue-number>-<short-description>

Examples:
feat/42-hardware-detection
fix/89-tailscale-auth
docs/103-architecture-guide
chore/55-update-deps
```

**Why?** This automatically links your work to the issue and helps with project tracking.

## 📝 Commit Message Format

We follow [Conventional Commits](https://www.conventionalcommits.org/) to automate versioning and changelog generation.

### Format

```
<type>(<scope>): <subject>

[optional body]

[optional footer]
```

### Types

| Type       | Description                            | Version Bump |
| ---------- | -------------------------------------- | ------------ |
| `feat`     | New feature                            | MINOR        |
| `fix`      | Bug fix                                | PATCH        |
| `docs`     | Documentation only                     | none         |
| `chore`    | Maintenance, dependencies              | none         |
| `test`     | Adding or updating tests               | none         |
| `refactor` | Code change that neither fixes nor adds | none         |
| `ci`       | CI/CD changes                          | none         |
| `perf`     | Performance improvements               | PATCH        |
| `revert`   | Revert a previous commit               | varies       |

**Breaking changes:** Add `!` after type or add `BREAKING CHANGE:` in footer to trigger a MAJOR version bump.

### Scopes

Scopes indicate which part of the codebase changed:

- `cli` - Leger CLI (`cmd/leger`)
- `daemon` - Legerd daemon (`cmd/legerd`)
- `internal` - Internal packages
- `docs` - Documentation
- `ci` - CI/CD
- `rpm` - RPM packaging
- `systemd` - Systemd integration
- `config` - Configuration management

### Examples

#### Simple feature
```bash
git commit -m "feat(cli): add secrets fetch command"
```

#### Bug fix with scope
```bash
git commit -m "fix(daemon): resolve race condition in state sync"
```

#### Breaking change
```bash
git commit -m "feat(cli)!: change config file format to YAML

BREAKING CHANGE: Config files are now in YAML format instead of JSON.
Users must migrate their config files using 'leger config migrate'.
"
```

#### Documentation update
```bash
git commit -m "docs: add installation guide for Fedora"
```

#### Dependency update
```bash
git commit -m "chore(deps): bump tailscale to v1.56.0"
```

## 🔄 Release Workflow

Our releases are **fully automated** using [release-please](https://github.com/googleapis/release-please):

### How It Works

1. **You commit** with conventional format
2. **Release Please bot** opens/updates a "Release PR"
3. **Maintainer reviews** the Release PR (checks version bump, changelog)
4. **Release PR is merged** → GitHub Release is created automatically
5. **Binaries are built** and attached to the release

### Version Bumping

- `feat:` commits → bump **MINOR** version (0.1.0 → 0.2.0)
- `fix:` commits → bump **PATCH** version (0.1.0 → 0.1.1)
- `feat!:` or `BREAKING CHANGE:` → bump **MAJOR** version (0.1.0 → 1.0.0)
- Other types (`docs:`, `chore:`, etc.) → no version bump, but appear in changelog

### Changelog

The CHANGELOG.md file is **automatically maintained** by release-please:
- Organized by commit type
- Links to PRs and commits
- Includes contributor credits

**Never edit CHANGELOG.md manually** - it's regenerated on each release.

## 🏷️ Pull Request Guidelines

### PR Title Format

PR titles **must** follow conventional commit format:

```
<type>(<scope>): <description>

✅ Good:
feat(cli): add hardware detection
fix(daemon): resolve authentication timeout
docs: update README with new examples

❌ Bad:
Add feature
Fixed bug
Update docs
```

**Why?** The PR title becomes the commit message when squash-merged and drives our release automation.

### PR Description Template

When opening a PR, include:

```markdown
## Description
Brief description of what this PR does.

## Related Issue
Closes #42

## Changes
- Added X
- Fixed Y
- Updated Z

## Testing
- [ ] Tests pass locally
- [ ] Added/updated tests for changes
- [ ] Manual testing completed

## Checklist
- [ ] PR title follows conventional commit format
- [ ] Code follows project style guidelines
- [ ] Documentation updated (if needed)
- [ ] CHANGELOG.md NOT modified (automated)
```

## 🧪 Testing

Before submitting a PR:

```bash
# Run all tests
make test

# Run linter
make lint

# Build both binaries
make build

# Verify builds
./leger --version
./legerd version
```

## 📚 Development Setup

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/leger.git
cd leger

# Add upstream remote
git remote add upstream https://github.com/leger-labs/leger.git

# Install dependencies
go mod download

# Build binaries
make build

# Run tests
make test
```

## 🎯 Milestones and Planning

Issues are organized by milestone:

- **0.1.x** - MVP: Local deployment foundation
- **0.2.x** - Public repo setup
- **0.3.x** - Community readiness
- **1.0.0** - Production release

Before starting work:
1. Check if an issue exists
2. If not, create one describing the problem/feature
3. Get confirmation it's wanted (comment or assign)
4. Create your branch from the issue number

## 🤝 Code Review Process

1. **All PRs require review** before merging
2. **CI must pass** (lint, test, build)
3. **Conventional commit title** must be valid
4. **Maintainer squash-merges** using PR title as commit message

## 📞 Getting Help

- **Questions?** Open an issue with `question` label
- **Bug report?** Use `type:fix` label and bug report template
- **Feature request?** Use `type:feat` label and feature request template

## 🙏 Attribution

By contributing, you agree that your contributions will be licensed under the same license as the project:
- New Leger components: Apache License 2.0
- Legerd (setec fork): BSD-3-Clause

Your contribution will be noted in release notes and CHANGELOG.md automatically.

---

## Example Workflow

Here's a complete example:

```bash
# 1. Create issue (or find existing)
# Issue #42: "Add hardware detection to CLI"

# 2. Create branch
git checkout -b feat/42-hardware-detection

# 3. Make changes
# ... code, code, code ...

# 4. Commit with conventional format
git add .
git commit -m "feat(cli): add hardware detection

Detects Framework laptop model and AMD GPU.
Enables hardware-specific optimizations.

Closes #42"

# 5. Push branch
git push origin feat/42-hardware-detection

# 6. Open PR with title:
# "feat(cli): add hardware detection"

# 7. Address review feedback
# ... make changes, push more commits ...

# 8. Maintainer merges PR
# → Release Please bot updates Release PR
# → On next release, your change is in CHANGELOG
# → GitHub Release includes your contribution
```

Thank you for contributing to Leger! 🚀
