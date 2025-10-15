# Issue #8: feat(cli): implement auth commands with local storage

## Context

Implement complete authentication workflow: login, status, logout. For v0.1.0, authentication is based solely on Tailscale identity stored locally. Backend authentication will be added in future versions.

**Sprint Goal**: This issue completes the v0.1.0 sprint. After this, users can run `leger auth login` and it will detect their existing Tailscale identity without requiring separate authentication.

**Architecture**: `/docs/leger-cli-legerd-architecture.md` (Authentication section)  
**Integration**: `/docs/tailscale-integration-analysis.md` (Scenario 2)

## Dependencies

- ✅ Issue #6: Tailscale identity verification must be working

## Tasks

### 1. Create Auth Storage Package

Create `internal/auth/storage.go`:

```go
package auth

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"
)

// Auth represents stored authentication state
type Auth struct {
    TailscaleUser string    `json:"tailscale_user"`
    Tailnet       string    `json:"tailnet"`
    DeviceName    string    `json:"device_name"`
    DeviceIP      string    `json:"device_ip"`
    AuthenticatedAt time.Time `json:"authenticated_at"`
}

// AuthFile returns the path to the auth file
func AuthFile() (string, error) {
    home, err := os.UserHomeDir()
    if err != nil {
        return "", fmt.Errorf("failed to get home directory: %w", err)
    }
    
    configDir := filepath.Join(home, ".config", "leger")
    if err := os.MkdirAll(configDir, 0700); err != nil {
        return "", fmt.Errorf("failed to create config directory: %w", err)
    }
    
    return filepath.Join(configDir, "auth.json"), nil
}

// Save saves authentication state to disk
func (a *Auth) Save() error {
    path, err := AuthFile()
    if err != nil {
        return err
    }
    
    data, err := json.MarshalIndent(a, "", "  ")
    if err != nil {
        return fmt.Errorf("failed to marshal auth: %w", err)
    }
    
    if err := os.WriteFile(path, data, 0600); err != nil {
        return fmt.Errorf("failed to write auth file: %w", err)
    }
    
    return nil
}

// Load loads authentication state from disk
func Load() (*Auth, error) {
    path, err := AuthFile()
    if err != nil {
        return nil, err
    }
    
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil // Not authenticated
        }
        return nil, fmt.Errorf("failed to read auth file: %w", err)
    }
    
    var auth Auth
    if err := json.Unmarshal(data, &auth); err != nil {
        return nil, fmt.Errorf("failed to parse auth file: %w", err)
    }
    
    return &auth, nil
}

// Clear removes authentication state
func Clear() error {
    path, err := AuthFile()
    if err != nil {
        return err
    }
    
    if err := os.Remove(path); err != nil {
        if os.IsNotExist(err) {
            return nil // Already cleared
        }
        return fmt.Errorf("failed to remove auth file: %w", err)
    }
    
    return nil
}

// IsAuthenticated checks if user is authenticated
func IsAuthenticated() bool {
    auth, err := Load()
    return err == nil && auth != nil
}
```

### 2. Update auth.go Commands

Modify `cmd/leger/auth.go`:

```go
func authLoginCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "login",
        Short: "Authenticate with Leger Labs",
        Long: `Verify Tailscale identity and authenticate with Leger.

This command checks your existing Tailscale authentication and uses it to
authenticate with Leger. No separate login is required.

Requirements:
- Tailscale must be installed
- Tailscale must be running (tailscale up)
- Device must be authenticated to a Tailnet

Your Tailscale identity will be stored locally in:
  ~/.config/leger/auth.json
`,
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            
            fmt.Println("Authenticating with Leger...")
            fmt.Println()
            
            // Check if already authenticated
            if auth.IsAuthenticated() {
                currentAuth, _ := auth.Load()
                fmt.Println("✓ Already authenticated")
                fmt.Printf("  User:      %s\n", currentAuth.TailscaleUser)
                fmt.Printf("  Tailnet:   %s\n", currentAuth.Tailnet)
                fmt.Printf("  Device:    %s\n", currentAuth.DeviceName)
                fmt.Println()
                fmt.Println("Run 'leger auth logout' to clear authentication")
                return nil
            }
            
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
            fmt.Printf("  IP:        %s\n", identity.DeviceIP)
            fmt.Println()
            
            // Save authentication
            authState := &auth.Auth{
                TailscaleUser:   identity.LoginName,
                Tailnet:         identity.Tailnet,
                DeviceName:      identity.DeviceName,
                DeviceIP:        identity.DeviceIP,
                AuthenticatedAt: time.Now(),
            }
            
            if err := authState.Save(); err != nil {
                return fmt.Errorf("failed to save authentication: %w", err)
            }
            
            authFile, _ := auth.AuthFile()
            fmt.Println("✓ Authentication saved")
            fmt.Printf("  Location:  %s\n", authFile)
            fmt.Println()
            fmt.Println("You're now authenticated with Leger!")
            
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
            
            fmt.Println("=== Tailscale Status ===")
            fmt.Println()
            
            if !client.IsInstalled() {
                fmt.Println("Status: NOT INSTALLED")
                fmt.Println()
                fmt.Println("Install Tailscale from: https://tailscale.com/download")
                return fmt.Errorf("Tailscale not installed")
            }
            
            if !client.IsRunning(ctx) {
                fmt.Println("Status: NOT RUNNING")
                fmt.Println()
                fmt.Println("Start Tailscale: sudo tailscale up")
                return fmt.Errorf("Tailscale daemon not running")
            }
            
            identity, err := client.GetIdentity(ctx)
            if err != nil {
                fmt.Println("Status: ERROR")
                return err
            }
            
            fmt.Println("Status: AUTHENTICATED")
            fmt.Printf("  User:      %s\n", identity.LoginName)
            fmt.Printf("  Tailnet:   %s\n", identity.Tailnet)
            fmt.Printf("  Device:    %s\n", identity.DeviceName)
            fmt.Printf("  IP:        %s\n", identity.DeviceIP)
            fmt.Println()
            
            // Check Leger authentication
            fmt.Println("=== Leger Authentication ===")
            fmt.Println()
            
            authState, err := auth.Load()
            if err != nil {
                fmt.Println("Status: ERROR")
                return fmt.Errorf("failed to load auth: %w", err)
            }
            
            if authState == nil {
                fmt.Println("Status: NOT AUTHENTICATED")
                fmt.Println()
                fmt.Println("Run: leger auth login")
                return nil
            }
            
            fmt.Println("Status: AUTHENTICATED")
            fmt.Printf("  User:      %s\n", authState.TailscaleUser)
            fmt.Printf("  Tailnet:   %s\n", authState.Tailnet)
            fmt.Printf("  Device:    %s\n", authState.DeviceName)
            fmt.Printf("  Authenticated: %s\n", authState.AuthenticatedAt.Format("2006-01-02 15:04:05"))
            fmt.Println()
            
            // Verify identity matches
            if authState.TailscaleUser != identity.LoginName {
                fmt.Println("⚠ Warning: Tailscale identity has changed")
                fmt.Println("  Run 'leger auth login' to re-authenticate")
            }
            
            return nil
        },
    }
}

func authLogoutCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "logout",
        Short: "Log out of Leger",
        Long:  "Clear local authentication state. Tailscale authentication is not affected.",
        RunE: func(cmd *cobra.Command, args []string) error {
            if !auth.IsAuthenticated() {
                fmt.Println("Not authenticated")
                return nil
            }
            
            currentAuth, _ := auth.Load()
            
            if err := auth.Clear(); err != nil {
                return fmt.Errorf("failed to clear authentication: %w", err)
            }
            
            fmt.Println("✓ Logged out successfully")
            fmt.Printf("  User:      %s\n", currentAuth.TailscaleUser)
            fmt.Println()
            fmt.Println("Tailscale authentication is still active.")
            fmt.Println("Run 'leger auth login' to authenticate again.")
            
            return nil
        },
    }
}
```

### 3. Create Tests

Create `internal/auth/storage_test.go`:

```go
package auth

import (
    "os"
    "testing"
    "time"
)

func TestAuthSaveLoad(t *testing.T) {
    // Create test auth
    testAuth := &Auth{
        TailscaleUser:   "alice@example.ts.net",
        Tailnet:         "example.ts.net",
        DeviceName:      "test-device",
        DeviceIP:        "100.64.0.1",
        AuthenticatedAt: time.Now(),
    }
    
    // Save
    if err := testAuth.Save(); err != nil {
        t.Fatalf("failed to save auth: %v", err)
    }
    
    // Load
    loaded, err := Load()
    if err != nil {
        t.Fatalf("failed to load auth: %v", err)
    }
    
    if loaded == nil {
        t.Fatal("expected non-nil auth")
    }
    
    if loaded.TailscaleUser != testAuth.TailscaleUser {
        t.Errorf("expected %s, got %s", testAuth.TailscaleUser, loaded.TailscaleUser)
    }
    
    // Cleanup
    Clear()
}

func TestClear(t *testing.T) {
    // Create test auth
    testAuth := &Auth{
        TailscaleUser: "alice@example.ts.net",
    }
    testAuth.Save()
    
    // Clear
    if err := Clear(); err != nil {
        t.Fatalf("failed to clear: %v", err)
    }
    
    // Verify cleared
    loaded, _ := Load()
    if loaded != nil {
        t.Fatal("expected nil after clear")
    }
}
```

### 4. Update Documentation

Add to `README.md`:

```markdown
## Quick Start

### Authentication

Leger uses your existing Tailscale authentication - no separate login required!

1. **Install Tailscale** (if not already installed):
   ```bash
   # Fedora
   sudo dnf install tailscale
   
   # Start and authenticate
   sudo tailscale up
   ```

2. **Authenticate with Leger**:
   ```bash
   leger auth login
   ```

3. **Check status**:
   ```bash
   leger auth status
   ```

4. **Log out** (clears local auth only):
   ```bash
   leger auth logout
   ```

### How It Works

- Leger detects your Tailscale identity
- No separate password or API key needed
- Authentication stored locally in `~/.config/leger/auth.json`
- Your Tailscale authentication is never modified
```

## Acceptance Criteria

- [ ] `internal/auth/storage.go` created
- [ ] Auth state stored in `~/.config/leger/auth.json`
- [ ] `leger auth login` works:
  - [ ] Detects Tailscale identity
  - [ ] Saves authentication locally
  - [ ] Shows success message
  - [ ] Shows already authenticated if run again
- [ ] `leger auth status` works:
  - [ ] Shows Tailscale status
  - [ ] Shows Leger authentication status
  - [ ] Shows warning if identity changed
  - [ ] Clear error messages when not authenticated
- [ ] `leger auth logout` works:
  - [ ] Clears local authentication
  - [ ] Shows what was cleared
  - [ ] Notes Tailscale still authenticated
- [ ] Tests pass
- [ ] Documentation updated
- [ ] File permissions secure (0600 for auth.json)

## Testing Plan

### Test 1: Full Authentication Flow
```bash
# Start fresh
leger auth logout  # Clear any existing auth

# Check status (not authenticated)
leger auth status
# Expected:
# Tailscale: AUTHENTICATED
# Leger: NOT AUTHENTICATED

# Login
leger auth login
# Expected:
# ✓ Tailscale identity verified
# ✓ Authentication saved
# You're now authenticated with Leger!

# Check status (authenticated)
leger auth status
# Expected:
# Tailscale: AUTHENTICATED
# Leger: AUTHENTICATED

# Try login again (already authenticated)
leger auth login
# Expected:
# ✓ Already authenticated
# Run 'leger auth logout' to clear authentication
```

### Test 2: Logout Flow
```bash
# Login first
leger auth login

# Logout
leger auth logout
# Expected:
# ✓ Logged out successfully
# Tailscale authentication is still active

# Check status
leger auth status
# Expected:
# Leger: NOT AUTHENTICATED
```

### Test 3: Auth File Location
```bash
# Login
leger auth login

# Verify file created
ls -la ~/.config/leger/auth.json
# Expected: File exists with 0600 permissions

# Check contents (should be JSON)
cat ~/.config/leger/auth.json
# Expected: Valid JSON with identity info
```

### Test 4: Tailscale Identity Change
```bash
# Login with one identity
leger auth login

# Switch Tailscale account (simulate)
# Run status check
leger auth status
# Expected: Warning about identity mismatch
```

### Test 5: Error Cases
```bash
# Without Tailscale installed
leger auth login
# Expected: Clear error about installation

# With Tailscale not running
leger auth login
# Expected: Clear error with instructions
```

## Reference Documentation

- **Architecture**: `/docs/leger-cli-legerd-architecture.md`
  - "Recommended: Minimal Auth for Solo Dev" section
- **Integration**: `/docs/tailscale-integration-analysis.md`
  - Section IV: Scenario 2 (Third-Party Integration)
- **Usage**: `/docs/leger-usage-guide.md`
  - Example workflows

## Security Considerations

- [ ] Auth file has restrictive permissions (0600)
- [ ] Auth file in user's home directory only
- [ ] No sensitive data in auth file (just identity info)
- [ ] Clear error messages don't expose sensitive info
- [ ] Logout clears local state completely

## User Experience

- [ ] Clear, friendly messages
- [ ] Actionable error messages with solutions
- [ ] Consistent formatting
- [ ] Progress indicators where appropriate
- [ ] No technical jargon in user-facing messages

## Expected Outcome

After this issue:
- ✅ **SPRINT GOAL COMPLETE**: User can authenticate via Tailscale
- ✅ `leger auth login` detects existing Tailscale authentication
- ✅ No duplicate authentication required
- ✅ Authentication state persists across CLI runs
- ✅ Clear status checking
- ✅ Simple logout
- ✅ **v0.1.0 READY FOR RELEASE**

## Issue Labels

- `type:feat`
- `area:cli`
- `priority:critical`
- `sprint:v0.1.0`
- `milestone:v0.1.0`

## Notes

- This is a **local-only** authentication for v0.1.0
- Backend authentication will be added in future version
- Tailscale is the sole authentication mechanism
- Auth state is just cached identity, not credentials
- Follow conventional commits for all commits

## Future Enhancements (Not in v0.1.0)

- Backend API authentication
- Token refresh
- Multi-device support
- Web UI login flow
- OAuth integration
