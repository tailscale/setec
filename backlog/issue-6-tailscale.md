# Issue #6: feat(cli): implement Tailscale identity verification

## Context

Implement Tailscale integration to detect and verify user identity. This is the foundation for authentication - if a device is already authenticated to Tailscale, leger can use that identity without requiring separate login.

**Architecture**: `/docs/leger-cli-legerd-architecture.md` (Authentication section)  
**Integration Analysis**: `/docs/tailscale-integration-analysis.md`  
**Usage**: `/docs/leger-usage-guide.md` (Example 1)

## Sprint Goal

**CRITICAL PATH** for v0.1.0 sprint. This issue enables Issue #8 (auth commands) to work. The sprint goal is: "User can run `leger auth login` and it detects their existing Tailscale identity."

## Dependencies

- ✅ Issue #5: Cobra CLI structure must be in place

## Tasks

### 1. Add Tailscale Dependencies

```bash
go get tailscale.com/client/tailscale
```

### 2. Create Tailscale Client Package

Create `internal/tailscale/client.go`:

```go
package tailscale

import (
    "context"
    "fmt"
    "os/exec"
    
    "tailscale.com/client/tailscale"
)

// Client wraps Tailscale LocalClient
type Client struct {
    lc *tailscale.LocalClient
}

// NewClient creates a new Tailscale client
func NewClient() *Client {
    return &Client{
        lc: &tailscale.LocalClient{},
    }
}

// IsInstalled checks if Tailscale is installed
func (c *Client) IsInstalled() bool {
    _, err := exec.LookPath("tailscale")
    return err == nil
}

// IsRunning checks if Tailscale daemon is running
func (c *Client) IsRunning(ctx context.Context) bool {
    _, err := c.lc.Status(ctx)
    return err == nil
}

// Identity represents a Tailscale identity
type Identity struct {
    UserID     uint64
    LoginName  string
    Tailnet    string
    DeviceName string
    DeviceIP   string
}

// GetIdentity retrieves the current Tailscale identity
func (c *Client) GetIdentity(ctx context.Context) (*Identity, error) {
    status, err := c.lc.Status(ctx)
    if err != nil {
        return nil, fmt.Errorf("failed to get Tailscale status: %w", err)
    }
    
    if status.Self == nil {
        return nil, fmt.Errorf("not authenticated to Tailscale")
    }
    
    user := status.User[status.Self.UserID]
    
    return &Identity{
        UserID:     uint64(status.Self.UserID),
        LoginName:  user.LoginName,
        Tailnet:    status.CurrentTailnet.Name,
        DeviceName: status.Self.HostName,
        DeviceIP:   status.Self.TailscaleIPs[0].String(),
    }, nil
}

// VerifyIdentity ensures Tailscale is installed, running, and authenticated
func (c *Client) VerifyIdentity(ctx context.Context) (*Identity, error) {
    if !c.IsInstalled() {
        return nil, fmt.Errorf("Tailscale not installed. Install from: https://tailscale.com/download")
    }
    
    if !c.IsRunning(ctx) {
        return nil, fmt.Errorf("Tailscale daemon not running. Run: sudo tailscale up")
    }
    
    return c.GetIdentity(ctx)
}
```

### 3. Create Identity Display Package

Create `internal/cli/identity.go`:

```go
package cli

import (
    "fmt"
    "os"
    
    "github.com/leger-labs/leger/internal/tailscale"
)

// PrintIdentity displays Tailscale identity in user-friendly format
func PrintIdentity(identity *tailscale.Identity) {
    fmt.Println("Tailscale Identity:")
    fmt.Printf("  User:      %s\n", identity.LoginName)
    fmt.Printf("  Tailnet:   %s\n", identity.Tailnet)
    fmt.Printf("  Device:    %s\n", identity.DeviceName)
    fmt.Printf("  IP:        %s\n", identity.DeviceIP)
}

// VerifyTailscaleOrExit checks Tailscale identity and exits on failure
func VerifyTailscaleOrExit(ctx context.Context) *tailscale.Identity {
    client := tailscale.NewClient()
    
    identity, err := client.VerifyIdentity(ctx)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
    
    return identity
}
```

### 4. Update auth.go to Use Tailscale

Modify `cmd/leger/auth.go`:

```go
func authLoginCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "login",
        Short: "Authenticate with Leger Labs",
        Long: `Verify Tailscale identity and authenticate with Leger backend.

This command checks your existing Tailscale authentication and uses it to
authenticate with Leger Labs. No separate login is required.

Requirements:
- Tailscale must be installed
- Tailscale must be running (tailscale up)
- Device must be authenticated to a Tailnet
`,
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            
            fmt.Println("Authenticating with Leger Labs...")
            fmt.Println()
            
            // Verify Tailscale identity
            client := tailscale.NewClient()
            identity, err := client.VerifyIdentity(ctx)
            if err != nil {
                return err
            }
            
            // Display identity
            fmt.Println("✓ Tailscale identity verified")
            fmt.Printf("  User:      %s\n", identity.LoginName)
            fmt.Printf("  Tailnet:   %s\n", identity.Tailnet)
            fmt.Printf("  Device:    %s\n", identity.DeviceName)
            fmt.Println()
            
            // TODO: Authenticate with Leger backend (Issue #8)
            fmt.Println("✓ Authenticated with Leger Labs")
            
            return nil
        },
    }
}

func authStatusCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "status",
        Short: "Check authentication status",
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            
            // Check Tailscale status
            client := tailscale.NewClient()
            
            if !client.IsInstalled() {
                fmt.Println("Tailscale: not installed")
                return fmt.Errorf("Tailscale not installed")
            }
            
            if !client.IsRunning(ctx) {
                fmt.Println("Tailscale: not running")
                return fmt.Errorf("Tailscale daemon not running")
            }
            
            identity, err := client.GetIdentity(ctx)
            if err != nil {
                return err
            }
            
            fmt.Println("Tailscale: authenticated")
            fmt.Printf("  User:      %s\n", identity.LoginName)
            fmt.Printf("  Tailnet:   %s\n", identity.Tailnet)
            fmt.Printf("  Device:    %s\n", identity.DeviceName)
            fmt.Println()
            
            // TODO: Check Leger backend authentication (Issue #8)
            fmt.Println("Leger Labs: not authenticated")
            fmt.Println("Run: leger auth login")
            
            return nil
        },
    }
}
```

### 5. Create Tests

Create `internal/tailscale/client_test.go`:

```go
package tailscale

import (
    "context"
    "testing"
)

func TestClientCreation(t *testing.T) {
    client := NewClient()
    if client == nil {
        t.Fatal("expected non-nil client")
    }
}

func TestIsInstalled(t *testing.T) {
    client := NewClient()
    // This test depends on whether Tailscale is actually installed
    // We just verify it doesn't panic
    _ = client.IsInstalled()
}

// Add more tests as needed
```

### 6. Test Manually

```bash
# Build
make build

# Test without Tailscale
./leger auth status
# Expected: Error about Tailscale not installed/running

# Test with Tailscale (if available)
# Ensure Tailscale is running:
sudo tailscale up

# Check identity
./leger auth status
# Expected: Shows Tailscale identity

# Try login
./leger auth login
# Expected: Shows identity, says "authenticated"
```

### 7. Update Documentation

Add to `docs/DEVELOPMENT.md`:

```markdown
## Tailscale Integration

Leger uses Tailscale for identity verification. To develop with Tailscale:

1. Install Tailscale:
   ```bash
   # Fedora
   sudo dnf install tailscale
   
   # Or download from https://tailscale.com/download
   ```

2. Authenticate:
   ```bash
   sudo tailscale up
   ```

3. Verify:
   ```bash
   tailscale status
   leger auth status
   ```

### Testing Without Tailscale

If Tailscale is not available, the CLI will show appropriate error messages.
```

## Acceptance Criteria

- [ ] Tailscale dependency added
- [ ] `internal/tailscale/client.go` created
- [ ] `internal/cli/identity.go` created
- [ ] `auth.go` updated to use Tailscale
- [ ] Tests created and passing
- [ ] CLI detects Tailscale installation
- [ ] CLI detects Tailscale running status
- [ ] CLI retrieves Tailscale identity
- [ ] `leger auth status` shows Tailscale identity
- [ ] `leger auth login` verifies Tailscale identity
- [ ] Error messages are clear and actionable
- [ ] Documentation updated

## Testing Plan

### Test 1: Tailscale Not Installed
```bash
# On system without Tailscale
leger auth status

# Expected output:
# Tailscale: not installed
# Error: Tailscale not installed. Install from: https://tailscale.com/download
```

### Test 2: Tailscale Installed But Not Running
```bash
# With Tailscale installed but stopped
leger auth status

# Expected output:
# Tailscale: not running
# Error: Tailscale daemon not running. Run: sudo tailscale up
```

### Test 3: Tailscale Running
```bash
# With Tailscale running
leger auth status

# Expected output:
# Tailscale: authenticated
#   User:      alice@example.ts.net
#   Tailnet:   example.ts.net
#   Device:    my-laptop
#
# Leger Labs: not authenticated
# Run: leger auth login
```

### Test 4: Login Command
```bash
leger auth login

# Expected output:
# Authenticating with Leger Labs...
#
# ✓ Tailscale identity verified
#   User:      alice@example.ts.net
#   Tailnet:   example.ts.net
#   Device:    my-laptop
#
# ✓ Authenticated with Leger Labs
```

### Test 5: RPM Installation
```bash
make rpm
sudo dnf install ./leger-*.rpm
leger auth status
```

## Reference Documentation

- **Architecture**: `/docs/leger-cli-legerd-architecture.md`
  - See "Authentication Models" section
  - See "Pure Tailscale Identity" approach
- **Integration**: `/docs/tailscale-integration-analysis.md`
  - Section I.A: Identity & Authentication
  - Section IV: Scenario 2 (Third-Party Integration)
- **Usage**: `/docs/leger-usage-guide.md`
  - Example 1: Install with Tailscale

## Error Handling

Provide clear, actionable error messages:

```
✗ Tailscale not installed

To use Leger, you need Tailscale installed and running.

Install Tailscale:
  https://tailscale.com/download

After installation:
  sudo tailscale up
  leger auth login
```

## Code Quality

- [ ] Clear error messages with actionable steps
- [ ] User-friendly output formatting
- [ ] Proper error handling (no panics)
- [ ] Tests cover main scenarios
- [ ] Docstrings for all public functions
- [ ] Follow Go best practices

## Expected Outcome

After this issue:
- ✅ Tailscale identity detection working
- ✅ `leger auth status` shows Tailscale info
- ✅ `leger auth login` verifies Tailscale identity
- ✅ Clear error messages when Tailscale unavailable
- ✅ Foundation complete for Issue #8 (backend authentication)
- ✅ **Sprint goal achievable**: CLI can detect Tailscale authentication

## Issue Labels

- `type:feat`
- `area:cli`
- `priority:critical`
- `sprint:v0.1.0`
- `dependencies:tailscale`

## Notes

- This issue focuses on LOCAL Tailscale detection only
- Backend authentication comes in Issue #8
- We're using Tailscale's official Go library
- Error messages must guide users to solutions
- Follow conventional commits for all commits

## Security Considerations

- Never store Tailscale credentials
- Only read identity from Tailscale daemon
- Respect Tailscale's authentication state
- No separate credential management needed
