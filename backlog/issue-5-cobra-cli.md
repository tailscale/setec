# Issue #5: feat(cli): implement Cobra CLI structure

## Context

Replace placeholder `cmd/leger/main.go` with full Cobra CLI structure. This establishes the foundation for all CLI commands and provides user-facing interface.

**Architecture Reference**: `/docs/leger-architecture.md`  
**Usage Guide**: `/docs/leger-usage-guide.md`  
**CLI Design**: `/docs/leger-cli-legerd-architecture.md`

## Sprint Goal

Part of v0.1.0 sprint. This issue creates the CLI skeleton that will support Tailscale authentication in Issue #8. The CLI must be installable via RPM and provide version information.

## Dependencies

- ✅ Issue #3: RPM packaging (leger binary must be installable)

## Tasks

### 1. Add Cobra Dependency

```bash
go get -u github.com/spf13/cobra@latest
```

### 2. Rewrite cmd/leger/main.go

Replace placeholder with complete Cobra structure:

```go
package main

import (
    "fmt"
    "os"

    "github.com/leger-labs/leger/version"
    "github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
    Use:   "leger",
    Short: "Podman Quadlet Manager with Secrets",
    Long: `Leger manages Podman Quadlets from Git repositories with integrated
secrets management via legerd.`,
    Version: version.String(),
}

func init() {
    rootCmd.SetVersionTemplate(version.Long() + "\n")
    
    // Global flags
    rootCmd.PersistentFlags().StringP("config", "c", "", "config file (default: /etc/leger/config.yaml)")
    rootCmd.PersistentFlags().BoolP("verbose", "v", false, "verbose output")
    
    // Command groups
    rootCmd.AddCommand(authCmd())
    rootCmd.AddCommand(configCmd())
    rootCmd.AddCommand(deployCmd())
    rootCmd.AddCommand(secretsCmd())
    rootCmd.AddCommand(statusCmd())
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

### 3. Create Command Stub Files

Create these files in `cmd/leger/`:

- [ ] `auth.go` - Authentication commands
- [ ] `config.go` - Configuration management
- [ ] `deploy.go` - Deployment commands
- [ ] `secrets.go` - Secrets management
- [ ] `status.go` - Status checking

Each file should have this structure:

```go
// cmd/leger/auth.go
package main

import (
    "fmt"
    "github.com/spf13/cobra"
)

func authCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "auth",
        Short: "Authentication commands",
        Long:  "Manage Leger authentication and Tailscale identity verification",
    }
    
    cmd.AddCommand(authLoginCmd())
    cmd.AddCommand(authStatusCmd())
    cmd.AddCommand(authLogoutCmd())
    
    return cmd
}

func authLoginCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "login",
        Short: "Authenticate with Leger Labs",
        Long:  "Verify Tailscale identity and authenticate with Leger backend",
        RunE: func(cmd *cobra.Command, args []string) error {
            return fmt.Errorf("not yet implemented")
        },
    }
}

func authStatusCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "status",
        Short: "Check authentication status",
        RunE: func(cmd *cobra.Command, args []string) error {
            return fmt.Errorf("not yet implemented")
        },
    }
}

func authLogoutCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "logout",
        Short: "Log out of Leger Labs",
        RunE: func(cmd *cobra.Command, args []string) error {
            return fmt.Errorf("not yet implemented")
        },
    }
}
```

### 4. Implement Basic Command Structure

For each command group, create stub commands:

**auth.go**:
- `leger auth login` - Authenticate with Tailscale identity
- `leger auth status` - Check authentication status
- `leger auth logout` - Clear authentication

**config.go**:
- `leger config pull` - Pull configuration from backend
- `leger config show` - Display current configuration

**deploy.go**:
- `leger deploy init` - Initialize deployment
- `leger deploy update` - Update deployed services

**secrets.go**:
- `leger secrets sync` - Sync secrets from backend
- `leger secrets list` - List available secrets

**status.go**:
- `leger status` - Show overall system status

### 5. Wire Up Version Information

Ensure version package integration:

```go
// Root command should use version package
rootCmd.Version = version.String()
rootCmd.SetVersionTemplate(version.Long() + "\n")
```

### 6. Test CLI Help System

```bash
# Build
make build

# Test help
./leger --help
./leger auth --help
./leger auth login --help

# Test version
./leger --version
./leger version  # Should also work
```

### 7. Test Installation Flow

```bash
# Build RPM
make rpm

# Install
sudo dnf install ./leger-*.rpm

# Test installed binary
leger --help
leger --version
leger auth login  # Should show "not yet implemented"
```

## Command Structure Reference

Based on `/docs/leger-architecture.md`:

```
leger
├── auth
│   ├── login       # Verify Tailscale identity
│   ├── status      # Check authentication
│   └── logout      # Clear authentication
├── config
│   ├── pull        # Pull config from backend
│   └── show        # Display current config
├── deploy
│   ├── init        # Initialize deployment
│   └── update      # Update services
├── secrets
│   ├── sync        # Sync from backend
│   └── list        # List secrets
├── status          # Overall status
└── version         # Show version (also --version)
```

## Acceptance Criteria

- [ ] Cobra dependency added
- [ ] `cmd/leger/main.go` rewritten with Cobra structure
- [ ] Command stub files created:
  - [ ] `auth.go`
  - [ ] `config.go`
  - [ ] `deploy.go`
  - [ ] `secrets.go`
  - [ ] `status.go`
- [ ] All commands return "not yet implemented" cleanly
- [ ] Help system works:
  - [ ] `leger --help` shows all commands
  - [ ] `leger auth --help` shows auth subcommands
  - [ ] Each subcommand has proper help text
- [ ] Version information works:
  - [ ] `leger --version` shows version
  - [ ] `leger version` shows detailed version info
  - [ ] Version comes from `version/version.go`
- [ ] Global flags work:
  - [ ] `--config` flag accepted
  - [ ] `--verbose` flag accepted
- [ ] No crashes or panics
- [ ] Installs correctly via RPM
- [ ] Tests pass: `go test ./cmd/leger/...`

## Testing Plan

### Test 1: Help System
```bash
leger --help
# Expected:
# - Shows all command groups
# - Shows global flags
# - Clean formatting

leger auth --help
# Expected:
# - Shows auth subcommands
# - Shows command descriptions

leger auth login --help
# Expected:
# - Shows usage
# - Shows description
```

### Test 2: Version Display
```bash
leger --version
# Expected: v0.1.0-dev (or current version)

leger version
# Expected: Detailed version with commit, build date
```

### Test 3: Not Implemented Commands
```bash
leger auth login
# Expected: Error: not yet implemented
# Exit code: 1

leger config pull
# Expected: Error: not yet implemented
# Exit code: 1
```

### Test 4: Global Flags
```bash
leger --verbose auth login
# Expected: Accepts flag, shows "not implemented"

leger --config /custom/config.yaml status
# Expected: Accepts flag, shows "not implemented"
```

### Test 5: Installation
```bash
make rpm
sudo dnf install ./leger-*.rpm
which leger
leger --version
```

## Reference Documentation

- **Architecture**: `/docs/leger-architecture.md` (Section: CLI Mode)
- **Usage**: `/docs/leger-usage-guide.md` (All examples)
- **CLI Design**: `/docs/leger-cli-legerd-architecture.md`

## Code Quality

- [ ] All functions have docstrings
- [ ] Error messages are user-friendly
- [ ] Help text is concise and clear
- [ ] Consistent command naming (lowercase, hyphenated)
- [ ] Follow Go best practices

## Expected Outcome

After this issue:
- ✅ Full Cobra CLI structure in place
- ✅ All command groups defined
- ✅ Help system functional
- ✅ Version information correct
- ✅ Foundation ready for Issue #6 (Tailscale integration)
- ✅ Foundation ready for Issue #8 (auth implementation)

## Issue Labels

- `type:feat`
- `area:cli`
- `priority:critical`
- `sprint:v0.1.0`

## Notes

- Keep command stubs simple (return "not implemented")
- Focus on structure, not implementation
- Implementation comes in Issues #6, #7, #8
- All help text should be user-friendly
- Follow conventional commits for all commits
