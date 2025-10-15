# Guide for Claude Code

This document provides essential context for Claude Code when working on Leger issues.

## Project Overview

**Leger** is a Podman Quadlet manager with integrated secrets management. It combines:
- CLI tool (`leger`) for managing quadlets
- Daemon (`legerd`) - a fork of Tailscale's setec for secrets
- Tailscale identity for authentication
- RPM packaging for Fedora distribution

---

## Issue Tracking

All issues are specified in detail in the `/backlog/` directory:

- **Issue #3**: `/backlog/ISSUE-3.md` - RPM packaging with nfpm
- **Issue #4**: `/backlog/ISSUE-4.md` - CI workflow for releases
- **Issue #5**: `/backlog/ISSUE-5.md` - Cobra CLI structure
- **Issue #6**: `/backlog/ISSUE-6.md` - Tailscale integration
- **Issue #7**: `/backlog/ISSUE-7.md` - legerd HTTP client (v0.2.0)
- **Issue #8**: `/backlog/ISSUE-8.md` - Auth commands implementation

**Always read the issue file first** before starting implementation.

---

## Documentation

Comprehensive documentation is available in `/docs/`:

### Architecture & Design
- `/docs/leger-architecture.md` - Complete system architecture
- `/docs/leger-executive-summary.md` - Vision and design decisions
- `/docs/leger-cli-legerd-architecture.md` - Detailed component breakdown
- `/docs/leger-usage-guide.md` - User workflows and examples

### RPM Packaging
- `/docs/rpm-packaging/README-QUICKSTART.md` - 15-minute setup guide
- `/docs/rpm-packaging/RPM-PACKAGING.md` - Complete implementation guide
- `/docs/rpm-packaging/CLOUDFLARE-SETUP.md` - R2 repository setup
- `/docs/rpm-packaging/SIGNING.md` - Package signing guide
- `/docs/rpm-packaging/RPM-PACKAGING-ANALYSIS.md` - Tailscale analysis

### Integration
- `/docs/tailscale-integration-analysis.md` - Tailscale dependencies and scenarios

### Reference Implementations
- `/docs/rpm-packaging/Makefile` - Build system reference
- `/docs/rpm-packaging/nfpm-dual.yaml` - Package configuration
- `/docs/rpm-packaging/release/rpm/*.sh` - RPM scriptlets
- `/docs/rpm-packaging/.github/workflows/*.yml` - CI workflow examples

**Always reference these docs** when implementing features.

---

## Contributing Guidelines

All contribution guidelines and standards are in `.github/`:

### Required Reading
- `.github/CONTRIBUTING.md` - Complete contribution guide
- `.github/ISSUE_TEMPLATE/` - Issue templates
- `.github/PULL_REQUEST_TEMPLATE.md` - PR template
- `.github/labels.yml` - Label definitions

### Commit Standards
**CRITICAL**: All commits must follow Conventional Commits format:

```
type(scope): description

[optional body]

[optional footer]
```

**Valid types**: `feat`, `fix`, `docs`, `chore`, `ci`, `test`, `refactor`, `perf`

**Common scopes**: `cli`, `daemon`, `rpm`, `infra`, `ci`, `docs`

**Examples**:
```
feat(cli): implement Cobra CLI structure
fix(daemon): correct health check endpoint
docs: update installation guide
chore(rpm): add nfpm configuration
ci: add RPM build workflow
```

This is enforced by the Semantic PR workflow and required for release-please.

---

## Known Limitations

### GitHub Workflows

⚠️ **IMPORTANT**: Claude Code CANNOT write to `.github/workflows/` directory due to security restrictions.

**When implementing Issue #4 or any CI workflow**:

1. Create workflow files in a temporary location (e.g., `/tmp/workflows/`)
2. **Clearly list in the PR description** which files could not be created
3. Provide the complete file contents in the PR body
4. I will manually copy them to `.github/workflows/`

**Example PR description**:
```markdown
## Files Not Created (Manual Step Required)

⚠️ Due to GitHub security restrictions, the following files need manual creation:

### `.github/workflows/release.yml`
```yaml
[paste complete file contents here]
```

Please copy this file to `.github/workflows/release.yml` after PR approval.
```

---

## Project Structure

```
leger/
├── cmd/
│   ├── leger/          # CLI binary
│   │   ├── main.go
│   │   ├── auth.go
│   │   ├── config.go
│   │   ├── deploy.go
│   │   ├── secrets.go
│   │   └── status.go
│   └── legerd/         # Daemon (setec fork)
│       └── main.go
├── internal/           # Leger-specific internal packages
│   ├── auth/          # Authentication storage
│   ├── cli/           # CLI helpers
│   ├── config/        # Configuration management
│   ├── daemon/        # legerd HTTP client
│   ├── tailscale/     # Tailscale integration
│   └── version/       # Version information
├── version/
│   └── version.go     # Version stamping (ldflags)
├── config/
│   └── leger.yaml     # Default configuration
├── systemd/           # Systemd units
│   ├── legerd.service         # User scope
│   └── legerd@.service        # System scope
├── release/
│   └── rpm/           # RPM scriptlets
│       ├── postinst.sh
│       ├── prerm.sh
│       └── postrm.sh
├── docs/              # Documentation (see above)
├── backlog/           # Issue specifications
│   └── ISSUE-*.md
├── .github/           # GitHub configuration
│   ├── workflows/     # CI/CD (⚠️ cannot write here)
│   └── labels.yml     # Label definitions
├── Makefile           # Build orchestration
├── nfpm.yaml          # Package configuration
└── go.mod
```

---

## Version Stamping

Version information is embedded at build time via ldflags:

```go
// version/version.go
var (
    Version   = "development"  // Set via ldflags
    Commit    = "unknown"      // Set via ldflags
    BuildDate = "unknown"      // Set via ldflags
)
```

Used in Makefile:
```makefile
LDFLAGS := -ldflags "\
    -X github.com/leger-labs/leger/version.Version=$(VERSION) \
    -X github.com/leger-labs/leger/version.Commit=$(COMMIT) \
    -X github.com/leger-labs/leger/version.BuildDate=$(BUILD_DATE)"
```

**Always use this pattern** when displaying version information.

---

## Testing Requirements

### Unit Tests
- Place tests next to the code: `foo.go` → `foo_test.go`
- Test files in `internal/` packages
- Use table-driven tests where appropriate
- Mock external dependencies (Tailscale, HTTP clients)

### Integration Tests
- RPM installation: `make rpm && sudo dnf install ./leger-*.rpm`
- Binary functionality: `leger --version`, `leger auth login`
- Systemd integration: `systemctl --user status legerd.service`

### Manual Testing Checklist
Always include in PR description:
```markdown
## Testing

- [ ] Unit tests pass: `go test ./...`
- [ ] Build succeeds: `make build`
- [ ] RPM creates: `make rpm`
- [ ] Version correct: `./leger --version`
- [ ] Help works: `./leger --help`
- [ ] [Specific feature tests]
```

---

## Dependencies

### Core Dependencies
```go
// Cobra for CLI
github.com/spf13/cobra

// Tailscale for identity
tailscale.com/client/tailscale

// Setec for secrets (already in legerd)
github.com/tailscale/setec
```

### Build Tools
- **nfpm** - Package builder: `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest`
- **golangci-lint** - Linter (optional but recommended)

### Runtime Requirements (User)
- **Tailscale** - Identity provider (must be installed and running)
- **Podman** - Container runtime (for future features)

---

## Code Quality Standards

### Go Best Practices
- Follow effective Go guidelines
- Use `gofmt` for formatting
- Add docstrings to exported functions
- Handle errors explicitly (no ignored errors)
- Avoid global state

### Error Messages
Must be user-friendly and actionable:

❌ **Bad**:
```
Error: failed to connect
```

✅ **Good**:
```
Error: Could not connect to legerd daemon

legerd is not running. Start it with:
  systemctl --user start legerd.service

Or check logs:
  journalctl --user -u legerd.service -f
```

### Help Text
All commands must have:
- Clear one-line `Short` description
- Detailed `Long` description with examples
- Usage examples where appropriate

```go
&cobra.Command{
    Use:   "login",
    Short: "Authenticate with Leger Labs",
    Long: `Verify Tailscale identity and authenticate with Leger.

This command checks your existing Tailscale authentication and uses it
to authenticate with Leger Labs. No separate login is required.

Requirements:
- Tailscale must be installed
- Tailscale must be running (tailscale up)
- Device must be authenticated to a Tailnet
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        // Implementation
    },
}
```

---

## Security Considerations

### Authentication
- Never store credentials in code
- Never log sensitive information
- Use Tailscale identity as source of truth
- Store auth state with restrictive permissions (0600)

### File Permissions
```go
// Auth file
os.WriteFile(path, data, 0600)  // User only

// Config directories
os.MkdirAll(dir, 0700)  // User only

// Public files
os.WriteFile(path, data, 0644)  // World readable
```

---

## Release Process

### Versioning
- Uses Semantic Versioning (MAJOR.MINOR.PATCH)
- Managed by release-please based on conventional commits
- Git tags trigger releases

### Release Workflow
1. Merge PR to `main` (with conventional commit)
2. release-please creates/updates Release PR
3. Merge Release PR → triggers GitHub Actions
4. Actions build RPMs (amd64 + arm64)
5. GitHub Release created with RPM attachments

---

## Common Patterns

### Check Tailscale Status
```go
client := tailscale.NewClient()
identity, err := client.VerifyIdentity(ctx)
if err != nil {
    return fmt.Errorf("Tailscale not available: %w", err)
}
```

### Load Configuration
```go
cfg, err := config.Load()
if err != nil {
    return fmt.Errorf("failed to load config: %w", err)
}
```

### Check Authentication
```go
if !auth.IsAuthenticated() {
    return fmt.Errorf("not authenticated. Run: leger auth login")
}
```

---

## Issue-Specific Notes

### Issue #3 (RPM Packaging)
- Reference `/docs/rpm-packaging/RPM-PACKAGING.md` extensively
- Use nfpm (not rpmbuild)
- Test on Fedora system
- Scriptlets must handle user + system scope

### Issue #4 (CI Workflow)
- ⚠️ **Cannot write to `.github/workflows/`**
- Provide complete workflow content in PR
- Test with `workflow_dispatch` first
- Include both amd64 and arm64 builds

### Issue #5 (Cobra CLI)
- Focus on structure, not implementation
- All commands return "not implemented"
- Wire up version from `version/version.go`
- Comprehensive help text

### Issue #6 (Tailscale)
- Use official Tailscale Go library
- Clear error messages when not installed
- Don't assume Tailscale is available
- Test without Tailscale first

### Issue #8 (Auth Commands)
- Store auth in `~/.config/leger/auth.json`
- File permissions: 0600
- Complete login/status/logout flow
- Validate against current Tailscale identity

---

## Getting Help

If you encounter issues or need clarification:

1. **Check documentation first**: `/docs/` has extensive guides
2. **Read issue file**: `/backlog/ISSUE-#.md` for detailed requirements
3. **Check examples**: Reference implementation files in `/docs/rpm-packaging/`
4. **Ask in PR**: Include specific questions in PR description

---

## Summary Checklist

When implementing an issue:

- [ ] Read `/backlog/ISSUE-#.md` thoroughly
- [ ] Reference relevant docs in `/docs/`
- [ ] Follow conventional commits
- [ ] Add tests for new functionality
- [ ] Update documentation if needed
- [ ] Include testing checklist in PR
- [ ] Note any files that couldn't be created (workflows)
- [ ] Ensure error messages are user-friendly
- [ ] Add docstrings to exported functions
- [ ] Follow Go best practices

---

**Good luck! You're building something great. 🚀**
