# Issue #7: feat(cli): implement legerd HTTP client

## Context

Implement HTTP client for leger CLI to communicate with legerd daemon. This enables the CLI to retrieve secrets from the local legerd instance.

**Architecture**: `/docs/leger-cli-legerd-architecture.md` (Communication section)  
**Usage**: `/docs/leger-usage-guide.md` (Secrets examples)

## Sprint Status

**⚠️ DEFERRED TO v0.2.0**

This issue is NOT part of the v0.1.0 sprint. For v0.1.0, we're focusing solely on authentication. Secrets management comes in v0.2.0.

## Dependencies

- ✅ Issue #1: legerd daemon exists (setec fork)
- ⏳ v0.2.0: legerd needs to be running and accessible

## Tasks

### 1. Create Daemon Client Package

Create `internal/daemon/client.go`:

```go
package daemon

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
)

// Client communicates with legerd daemon
type Client struct {
    baseURL    string
    httpClient *http.Client
}

// NewClient creates a new legerd client
func NewClient(baseURL string) *Client {
    if baseURL == "" {
        baseURL = "http://localhost:8080"
    }
    
    return &Client{
        baseURL: baseURL,
        httpClient: &http.Client{
            Timeout: 10 * time.Second,
        },
    }
}

// Health checks if legerd is running
func (c *Client) Health(ctx context.Context) error {
    req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", nil)
    if err != nil {
        return err
    }
    
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("legerd not reachable: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("legerd health check failed: %d", resp.StatusCode)
    }
    
    return nil
}

// Secret represents a secret from legerd
type Secret struct {
    Name  string `json:"name"`
    Value string `json:"value"`
}

// GetSecret retrieves a secret from legerd
func (c *Client) GetSecret(ctx context.Context, name string) (string, error) {
    url := fmt.Sprintf("%s/api/get?name=%s", c.baseURL, name)
    
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return "", err
    }
    
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return "", fmt.Errorf("failed to get secret: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode == http.StatusNotFound {
        return "", fmt.Errorf("secret not found: %s", name)
    }
    
    if resp.StatusCode != http.StatusOK {
        return "", fmt.Errorf("legerd error: %d", resp.StatusCode)
    }
    
    var secret Secret
    if err := json.NewDecoder(resp.Body).Decode(&secret); err != nil {
        return "", fmt.Errorf("failed to decode response: %w", err)
    }
    
    return secret.Value, nil
}

// PutSecret stores a secret in legerd
func (c *Client) PutSecret(ctx context.Context, name, value string) error {
    secret := Secret{
        Name:  name,
        Value: value,
    }
    
    body, err := json.Marshal(secret)
    if err != nil {
        return err
    }
    
    req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/put", bytes.NewReader(body))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return fmt.Errorf("failed to put secret: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("legerd error: %d", resp.StatusCode)
    }
    
    return nil
}

// ListSecrets returns all secret names
func (c *Client) ListSecrets(ctx context.Context) ([]string, error) {
    req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/list", nil)
    if err != nil {
        return nil, err
    }
    
    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to list secrets: %w", err)
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("legerd error: %d", resp.StatusCode)
    }
    
    var secrets []string
    if err := json.NewDecoder(resp.Body).Decode(&secrets); err != nil {
        return nil, fmt.Errorf("failed to decode response: %w", err)
    }
    
    return secrets, nil
}
```

### 2. Create Status Command Integration

Update `cmd/leger/status.go`:

```go
func statusCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "status",
        Short: "Show overall system status",
        Long:  "Display status of Tailscale, Leger auth, legerd daemon, and deployed services",
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            
            fmt.Println("=== Leger System Status ===")
            fmt.Println()
            
            // Check Tailscale (from Issue #6)
            // ... existing code ...
            
            // Check legerd daemon
            fmt.Println("=== legerd Daemon ===")
            fmt.Println()
            
            daemonClient := daemon.NewClient("")
            if err := daemonClient.Health(ctx); err != nil {
                fmt.Println("Status: NOT RUNNING")
                fmt.Printf("  Error: %v\n", err)
                fmt.Println()
                fmt.Println("Start legerd:")
                fmt.Println("  systemctl --user start legerd.service")
            } else {
                fmt.Println("Status: RUNNING")
                fmt.Printf("  URL: %s\n", "http://localhost:8080")
                fmt.Println()
            }
            
            return nil
        },
    }
}
```

### 3. Create Secrets Commands (Stub)

Update `cmd/leger/secrets.go`:

```go
func secretsCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "secrets",
        Short: "Secrets management",
        Long:  "Manage secrets via legerd daemon",
    }
    
    cmd.AddCommand(secretsListCmd())
    cmd.AddCommand(secretsSyncCmd())
    
    return cmd
}

func secretsListCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "list",
        Short: "List available secrets",
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            
            client := daemon.NewClient("")
            
            // Check if legerd is running
            if err := client.Health(ctx); err != nil {
                return fmt.Errorf("legerd not running: %w\nStart with: systemctl --user start legerd.service", err)
            }
            
            secrets, err := client.ListSecrets(ctx)
            if err != nil {
                return err
            }
            
            if len(secrets) == 0 {
                fmt.Println("No secrets found")
                return nil
            }
            
            fmt.Println("Available secrets:")
            for _, secret := range secrets {
                fmt.Printf("  - %s\n", secret)
            }
            
            return nil
        },
    }
}

func secretsSyncCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "sync",
        Short: "Sync secrets from backend",
        Long:  "Fetch secrets from Leger backend and store in legerd",
        RunE: func(cmd *cobra.Command, args []string) error {
            // TODO: Implement in v0.2.0
            return fmt.Errorf("not yet implemented - coming in v0.2.0")
        },
    }
}
```

### 4. Create Tests

Create `internal/daemon/client_test.go`:

```go
package daemon

import (
    "context"
    "testing"
)

func TestClientCreation(t *testing.T) {
    client := NewClient("")
    if client == nil {
        t.Fatal("expected non-nil client")
    }
    
    if client.baseURL != "http://localhost:8080" {
        t.Errorf("expected default baseURL, got %s", client.baseURL)
    }
}

func TestClientWithCustomURL(t *testing.T) {
    client := NewClient("http://custom:9000")
    if client.baseURL != "http://custom:9000" {
        t.Errorf("expected custom baseURL, got %s", client.baseURL)
    }
}

// Note: Integration tests require legerd to be running
// Add those in a separate test suite
```

### 5. Add Configuration Support

Update `config/leger.yaml`:

```yaml
# Existing config...

# Daemon connection
daemon:
  url: "http://localhost:8080"
  timeout: 10s
  
# Or use Tailscale URL for remote access
# daemon:
#   url: "http://legerd.example.ts.net:8080"
#   timeout: 10s
```

## Acceptance Criteria

- [ ] `internal/daemon/client.go` created
- [ ] HTTP client can communicate with legerd
- [ ] Health check works
- [ ] GetSecret works
- [ ] PutSecret works
- [ ] ListSecrets works
- [ ] Status command shows legerd status
- [ ] Secrets list command works (when legerd running)
- [ ] Tests pass
- [ ] Clear error messages when legerd not running
- [ ] Configuration support for daemon URL

## Testing Plan

### Test 1: Without legerd Running
```bash
leger status
# Expected:
# legerd Daemon: NOT RUNNING
# Start with: systemctl --user start legerd.service

leger secrets list
# Expected: Error: legerd not running
```

### Test 2: With legerd Running
```bash
# Start legerd
systemctl --user start legerd.service

# Check status
leger status
# Expected:
# legerd Daemon: RUNNING

# List secrets
leger secrets list
# Expected: (empty or list of secrets)
```

### Test 3: Health Check
```bash
# With legerd running
curl http://localhost:8080/health
# Expected: 200 OK

# CLI should detect this
leger status
# Expected: Shows legerd as RUNNING
```

## Reference Documentation

- **Architecture**: `/docs/leger-cli-legerd-architecture.md`
  - Communication: leger ↔ legerd section
  - Secret Retrieval methods
- **Usage**: `/docs/leger-usage-guide.md`
  - Example 5: Secret Rotation

## Notes

- This issue is DEFERRED to v0.2.0
- Focus on authentication first (v0.1.0)
- legerd HTTP API is already defined (setec fork)
- This client just wraps that API
- Follow conventional commits for all commits

## Integration with Issue #1 (legerd)

legerd (Issue #1) already provides HTTP API:
- `GET /health` - Health check
- `GET /api/get?name=<name>` - Get secret
- `POST /api/put` - Store secret
- `GET /api/list` - List secrets

This issue creates the client-side wrapper for that API.

## Expected Outcome

After this issue:
- ✅ leger CLI can talk to legerd daemon
- ✅ Secrets commands functional (list, get)
- ✅ Foundation for secrets management (v0.2.0)
- ✅ Status command shows legerd health

## Issue Labels

- `type:feat`
- `area:cli`
- `area:daemon`
- `priority:medium`
- `milestone:v0.2.0` (NOT v0.1.0)

## Future Work (v0.2.0)

- Implement secrets sync from backend
- Implement secrets push to legerd
- Podman secrets integration
- Secret rotation monitoring
