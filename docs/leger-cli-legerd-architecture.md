# Leger Architecture: CLI + Daemon (Correct Model)

## The Tailscale Pattern

```
tailscale (CLI)          legerd (daemon)
    │                         │
    ├─ status                 ├─ manages network
    ├─ up                     ├─ handles auth
    ├─ down                   ├─ maintains connections
    └─ [commands]             └─ [always running]

       ↓  (talks to daemon via socket/API)

SAME MODEL:

leger (CLI)              legerd (daemon)
    │                         │
    ├─ auth login            ├─ SETEC server (IS setec)
    ├─ deploy init           ├─ stores/serves secrets
    ├─ config pull           ├─ HTTP API on :8080
    ├─ secrets sync          ├─ Tailscale-authenticated
    └─ [commands]            └─ [always running]
```

## Binary Structure

### What Gets Installed (RPM Package)

```
/usr/bin/
├── leger                    # CLI tool (talks to legerd)
└── legerd                   # Daemon (IS setec, rebranded)

/usr/lib/systemd/system/
└── legerd.service          # Systemd unit for daemon

/var/lib/legerd/            # Daemon state directory
├── secrets.db              # Encrypted secrets
└── keys/
    └── main.key

/var/lib/leger/             # CLI state directory
├── config/
├── backups/
└── staged/
```

## legerd IS setec

`legerd` is not "embedding" setec - it **IS** setec, forked and adapted:

```go
// cmd/legerd/main.go
package main

import (
    "github.com/leger-labs/leger/internal/setec/server"
)

func main() {
    // This IS the setec server
    // Just rebranded as "legerd"
    
    cfg := &server.Config{
        StorePath:  "/var/lib/legerd",
        ListenAddr: ":8080",
        TailscaleOnly: true,
    }
    
    srv, err := server.New(cfg)
    if err != nil {
        log.Fatal(err)
    }
    
    // Run server (blocks)
    if err := srv.Run(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

**Why fork setec instead of using it directly?**
1. Can customize for Leger use case
2. Add Leger-specific features
3. Control versioning/updates
4. Rebrand for clarity (`legerd` vs generic `setec`)

## Communication: leger ↔ legerd

### Via HTTP API (like tailscale uses socket)

```go
// internal/daemon/client.go
package daemon

type Client struct {
    baseURL string  // http://localhost:8080 or http://secrets.tailnet.ts.net:8080
}

func NewClient() *Client {
    return &Client{
        baseURL: "http://localhost:8080",
    }
}

func (c *Client) StoreSecret(name, value string) error {
    resp, err := http.Post(
        c.baseURL + "/api/put",
        "application/json",
        strings.NewReader(fmt.Sprintf(`{"name":"%s","value":"%s"}`, name, value)),
    )
    return err
}

func (c *Client) GetSecret(name string) (string, error) {
    resp, err := http.Get(c.baseURL + "/api/get?name=" + name)
    // ...
}
```

### leger CLI Uses Daemon Client

```bash
$ leger secrets sync

# What happens:
# 1. leger CLI talks to Cloudflare Workers
# 2. Fetches encrypted secrets
# 3. Decrypts locally
# 4. Talks to legerd (via HTTP)
# 5. legerd stores in /var/lib/legerd/secrets.db
```

```go
// cmd/leger/secrets.go
func syncSecrets(cmd *cobra.Command, args []string) error {
    // 1. Fetch from Cloudflare
    secrets, err := cloudflareClient.FetchSecrets()
    
    // 2. Connect to local legerd
    daemonClient := daemon.NewClient()
    
    // 3. Store each secret
    for name, value := range secrets {
        err := daemonClient.StoreSecret(
            fmt.Sprintf("leger/%s/%s", userID, name),
            value,
        )
    }
    
    return nil
}
```

## Secret Retrieval: Most Efficient Methods

### Option 1: Podman Secrets + legerd Backend (BEST)

**Concept:** Podman has native secrets support. We make legerd a Podman secrets driver.

```ini
# openwebui.container
[Container]
Image=ghcr.io/open-webui/open-webui:latest

# Use Podman secrets (backed by legerd)
Secret=openai_api_key,type=env,target=OPENAI_API_KEY
Secret=anthropic_api_key,type=env,target=ANTHROPIC_API_KEY
```

**How it works:**

```bash
# When systemd starts the container:
1. Podman sees Secret=openai_api_key
2. Queries Podman secrets driver
3. Driver calls: http://localhost:8080/api/get?name=leger/alice/openai_api_key
4. legerd returns decrypted secret
5. Podman injects as environment variable
```

**Implementation:**

```go
// internal/podman/secrets_driver.go
// This runs as part of leger CLI, not legerd
// Podman calls this when it needs secrets

package podman

import (
    "github.com/containers/podman/v4/pkg/secrets"
)

type LegerdSecretsDriver struct {
    daemonClient *daemon.Client
}

func (d *LegerdSecretsDriver) Lookup(name string) ([]byte, error) {
    // Podman calls this
    // We call legerd
    return d.daemonClient.GetSecret(name)
}

func (d *LegerdSecretsDriver) Store(name string, data []byte) error {
    return d.daemonClient.StoreSecret(name, string(data))
}

func (d *LegerdSecretsDriver) Delete(name string) error {
    return d.daemonClient.DeleteSecret(name)
}

func (d *LegerdSecretsDriver) List() ([]string, error) {
    return d.daemonClient.ListSecrets()
}
```

**Advantages:**
- ✅ Native Podman integration
- ✅ No shell scripts needed
- ✅ Secrets in memory only (not on disk)
- ✅ Clean quadlet files
- ✅ Automatic cleanup when container stops

**Disadvantages:**
- ⚠️ Requires Podman secrets driver plugin system
- ⚠️ More complex integration

---

### Option 2: systemd EnvironmentFile + ExecStartPre (GOOD)

**Concept:** Generate environment file at service start time.

```ini
# openwebui.container
[Container]
Image=ghcr.io/open-webui/open-webui:latest
EnvironmentFile=/run/user/%U/openwebui.env

[Service]
# Fetch secrets before container starts
ExecStartPre=/usr/bin/leger secrets fetch-for-service openwebui
ExecStopPost=/bin/rm -f /run/user/%U/openwebui.env
```

**How it works:**

```bash
# When systemd starts service:
systemd runs: leger secrets fetch-for-service openwebui
    ↓
leger CLI queries legerd HTTP API
    ↓
Writes /run/user/1000/openwebui.env:
    OPENAI_API_KEY=sk-...
    ANTHROPIC_API_KEY=sk-ant-...
    ↓
Podman reads EnvironmentFile
    ↓
Container starts with environment variables
    ↓
On stop, file deleted
```

**Implementation:**

```go
// cmd/leger/secrets.go
func fetchForService(cmd *cobra.Command, args []string) error {
    serviceName := args[0]  // "openwebui"
    
    // Get required secrets for this service
    requiredSecrets := config.GetRequiredSecrets(serviceName)
    
    // Fetch from legerd
    envFile := fmt.Sprintf("/run/user/%d/%s.env", os.Getuid(), serviceName)
    f, err := os.Create(envFile)
    defer f.Close()
    
    for _, secretName := range requiredSecrets {
        value, err := daemonClient.GetSecret(
            fmt.Sprintf("leger/%s/%s", userID, secretName),
        )
        if err != nil {
            return err
        }
        
        fmt.Fprintf(f, "%s=%s\n", strings.ToUpper(secretName), value)
    }
    
    // Secure permissions
    os.Chmod(envFile, 0600)
    
    return nil
}
```

**Advantages:**
- ✅ Simple to implement
- ✅ Works with standard Podman
- ✅ Secrets only exist during container runtime
- ✅ Automatic cleanup

**Disadvantages:**
- ⚠️ Secrets briefly on disk (in /run, which is tmpfs)
- ⚠️ Extra leger CLI invocation at each start

---

### Option 3: Init Container Pattern (OKAY)

**Concept:** Small init script in container fetches secrets.

```ini
# openwebui.container
[Container]
Image=ghcr.io/open-webui/open-webui:latest

# Override entrypoint with init script
Entrypoint=/init.sh

Volume=init-script:/init.sh:Z,ro
```

**init.sh (provided by leger):**

```bash
#!/bin/sh
# /usr/share/leger/init.sh

# Fetch secrets from legerd
export OPENAI_API_KEY=$(curl -sf http://secrets.tailnet.ts.net:8080/api/get?name=leger/$LEGER_USER/openai_api_key)
export ANTHROPIC_API_KEY=$(curl -sf http://secrets.tailnet.ts.net:8080/api/get?name=leger/$LEGER_USER/anthropic_api_key)

# Run actual application
exec /app/start.sh
```

**Advantages:**
- ✅ Secrets never on disk
- ✅ No ExecStartPre needed

**Disadvantages:**
- ⚠️ Requires curl in container
- ⚠️ Modifies container entrypoint
- ⚠️ More complex quadlet files

---

### Option 4: Direct HTTP in Quadlet (SIMPLE but UGLY)

What I showed before - inline curl in ExecStartPre:

```ini
[Service]
ExecStartPre=/bin/sh -c 'echo OPENAI_API_KEY=$(curl -sf http://localhost:8080/api/get?name=leger/alice/openai_api_key) > /run/openwebui.env'
```

**Advantages:**
- ✅ No extra CLI calls
- ✅ Simple to understand

**Disadvantages:**
- ⚠️ Ugly quadlet files
- ⚠️ Hardcoded secret names
- ⚠️ Complex shell escaping

---

## Recommendation: Hybrid Approach

### For v1.0: Option 2 (EnvironmentFile + leger CLI)

**Why:**
- Simple to implement
- Works with standard Podman
- Clean quadlet files
- Secrets managed by leger CLI

**Quadlet looks like:**

```ini
# ~/.config/containers/systemd/openwebui/openwebui.container
[Unit]
Description=Open WebUI
After=network-online.target legerd.service
Requires=legerd.service

[Container]
ContainerName=open-webui
Image=ghcr.io/open-webui/open-webui:latest
EnvironmentFile=/run/user/%U/openwebui.env
Pod=openwebui

[Service]
Restart=always

# Fetch secrets before start
ExecStartPre=/usr/bin/leger secrets fetch openwebui

# Cleanup on stop
ExecStopPost=/bin/rm -f /run/user/%U/openwebui.env

[Install]
WantedBy=default.target
```

**Backend template renders:**

```typescript
// Backend knows which secrets each service needs
export function renderOpenWebUI(config: OpenWebUIConfig): QuadletFiles {
    return {
        'openwebui.container': `
[Container]
EnvironmentFile=/run/user/%U/openwebui.env

[Service]
ExecStartPre=/usr/bin/leger secrets fetch openwebui
ExecStopPost=/bin/rm -f /run/user/%U/openwebui.env
        `
    };
}
```

**leger CLI knows mapping:**

```go
// internal/config/secrets.go
var serviceSecrets = map[string][]string{
    "openwebui": {
        "openai_api_key",
        "anthropic_api_key",
        "openwebui_secret_key",
    },
    "litellm": {
        "openai_api_key",
        "anthropic_api_key",
        "groq_api_key",
    },
}

func GetRequiredSecrets(serviceName string) []string {
    return serviceSecrets[serviceName]
}
```

### For v2.0: Option 1 (Podman Secrets Driver)

Once we have more time, implement proper Podman secrets integration:

```ini
# Future: Clean quadlet
[Container]
Secret=openai_api_key,type=env,target=OPENAI_API_KEY
Secret=anthropic_api_key,type=env,target=ANTHROPIC_API_KEY
```

No ExecStartPre needed, Podman handles everything.

## Complete Flow Example

### 1. legerd Running (Always On)

```bash
$ systemctl status legerd
● legerd.service - Leger Secrets Daemon
     Active: active (running)
   Main PID: 1234 (legerd)

$ curl http://localhost:8080/health
{"status":"ok","secrets_count":5}
```

### 2. User Syncs Secrets

```bash
$ leger secrets sync

Syncing secrets to local daemon...

Fetching from Cloudflare...
  ✓ openai_api_key
  ✓ anthropic_api_key

Connecting to legerd...
  POST http://localhost:8080/api/put
  ✓ leger/alice/openai_api_key stored
  ✓ leger/alice/anthropic_api_key stored

Secrets synced successfully.
```

**What happened:**

```go
// leger CLI
secrets := cloudflareClient.FetchSecrets()

daemonClient := daemon.NewClient()
for name, value := range secrets {
    daemonClient.StoreSecret(
        fmt.Sprintf("leger/%s/%s", userID, name),
        value,
    )
}
```

### 3. User Starts Service

```bash
$ systemctl --user start openwebui

# Systemd executes:
# 1. ExecStartPre=/usr/bin/leger secrets fetch openwebui
# 2. Starts container with EnvironmentFile
```

**What leger secrets fetch does:**

```go
func fetchForService(serviceName string) error {
    // 1. Get list of required secrets
    required := config.GetRequiredSecrets(serviceName)
    
    // 2. Query legerd for each
    envFile := fmt.Sprintf("/run/user/%d/%s.env", os.Getuid(), serviceName)
    f, _ := os.Create(envFile)
    
    for _, secret := range required {
        value, _ := daemonClient.GetSecret(
            fmt.Sprintf("leger/%s/%s", userID, secret),
        )
        fmt.Fprintf(f, "%s=%s\n", 
            envVarName(secret), 
            value,
        )
    }
    
    os.Chmod(envFile, 0600)
    return nil
}
```

**Result:**

```bash
# /run/user/1000/openwebui.env created:
OPENAI_API_KEY=sk-proj-abc...
ANTHROPIC_API_KEY=sk-ant-def...
OPENWEBUI_SECRET_KEY=generated-key

# Container starts, reads this file
# File deleted when container stops
```

### 4. Container Uses Secrets

```bash
# Inside container
$ echo $OPENAI_API_KEY
sk-proj-abc...

# Container makes API calls with these secrets
# Secrets never written to container's filesystem
# Only exist in container's memory
```

## Performance Characteristics

### Startup Time

```
Container start triggered
    ↓ (< 1ms)
systemd runs ExecStartPre
    ↓ (5-10ms)
leger CLI starts
    ↓ (1-2ms)
HTTP call to legerd (localhost)
    ↓ (< 1ms)
legerd returns secrets
    ↓ (1-2ms)
Write to /run (tmpfs, in-memory)
    ↓ (< 1ms)
leger CLI exits
    ↓
Podman reads EnvironmentFile
    ↓
Container starts with secrets

Total overhead: ~10-15ms
```

**Very fast because:**
- ✅ legerd is localhost (no network latency)
- ✅ /run is tmpfs (in-memory filesystem)
- ✅ Single HTTP request per service
- ✅ No disk I/O except tmpfs

### Memory Usage

```
legerd daemon:    ~20MB (running setec server)
leger CLI:        ~15MB (only during command execution)
Secrets in /run:  < 1KB per service
```

### Security

```
Secrets flow:
1. Encrypted in Cloudflare KV
2. Decrypted by leger CLI (in memory)
3. Sent to legerd over localhost HTTP
4. Stored encrypted in /var/lib/legerd/secrets.db
5. At container start: retrieved from legerd
6. Written to /run (tmpfs, memory-backed)
7. Read by container
8. Deleted when container stops

Secrets NEVER on persistent disk except encrypted in legerd's database.
```

## Summary

**Correct Mental Model:**
- `leger` = CLI tool (like `tailscale`)
- `legerd` = Daemon that IS setec (like `tailscaled`)
- Quadlets in `~/.config/containers/systemd/`
- Secrets fetched at container start via leger CLI → legerd HTTP API
- Secrets in /run (tmpfs) only during container runtime

**Most Efficient Method (v1.0):**
- ExecStartPre calls: `leger secrets fetch <service>`
- leger queries legerd (localhost HTTP)
- Writes /run/user/UID/service.env
- Container uses EnvironmentFile
- Cleanup on stop

**Total overhead: ~10-15ms per container start**

---

Excellent questions! Let me break down authentication patterns and then give you the **minimal viable approach** for a solo dev.

## How Other CLIs Handle Auth

### GitHub CLI (`gh`)

**Method: Device Flow**
```bash
$ gh auth login

? What account do you want to log into? GitHub.com
? You're already logged into github.com. Do you want to re-authenticate? Yes
? How would you like to authenticate? Login with a web browser

! First copy your one-time code: ABCD-1234
- Press Enter to open github.com in your browser...
✓ Authentication complete. Press Enter to continue...
```

**What happens:**
1. CLI calls GitHub API: `POST /login/device/code`
2. Gets back: device_code + user_code (ABCD-1234)
3. Shows user code, waits
4. User enters code at github.com/login/device
5. CLI polls GitHub API every 5s
6. GitHub returns OAuth token when authorized
7. Token stored in `~/.config/gh/hosts.yml`

**Token file:**
```yaml
github.com:
    user: yourusername
    oauth_token: gho_xxxxxxxxxxxx
    git_protocol: https
```

### Tailscale CLI

**Method: Uses Tailscale Identity (No Separate Auth!)**

```bash
$ tailscale status
# Shows you're authenticated or not

$ tailscale up
# If not authenticated, opens browser to login.tailscale.com
# Uses Google/Microsoft/GitHub OAuth
# Auth handled by tailscaled daemon
```

**Key difference:** No separate token storage! The `tailscaled` daemon manages authentication. CLI talks to daemon via socket.

```bash
# tailscale CLI doesn't store tokens
# It queries the daemon:
$ tailscale status --json
{
  "Self": {
    "UserID": 123456,
    "LoginName": "alice@example.ts.net",
    ...
  }
}
```

## Recommended for Leger: **Pure Tailscale Identity**

Since Tailscale is a **hard dependency**, you can avoid building separate auth entirely!

### The Minimal Approach (No Device Flow Needed!)

```bash
$ leger auth login

Authenticating with Leger Labs...

Checking Tailscale identity...
✓ Authenticated as: alice@example.ts.net
✓ Tailnet: example.ts.net
✓ Device: blueprint-desktop

Registering with Leger cloud...
✓ Account created/linked

Ready to use Leger!
```

**What happens:**

```go
// cmd/leger/auth.go
func login(cmd *cobra.Command, args []string) error {
    // 1. Get local Tailscale identity (no browser needed!)
    tsClient := &tailscale.LocalClient{}
    status, err := tsClient.Status(context.Background())
    
    identity := &Identity{
        UserID:   status.Self.UserID,
        LoginName: status.User[status.Self.UserID].LoginName,
        Tailnet:  status.CurrentTailnet.Name,
        DeviceName: status.Self.HostName,
    }
    
    // 2. Call Leger API with Tailscale identity
    resp, err := http.Post(
        "https://api.leger.run/auth/tailscale",
        "application/json",
        // Send identity info
    )
    
    // 3. Leger backend validates against Tailscale API
    // 4. Returns short-lived token
    
    // 5. Store token locally
    token := parseResponse(resp)
    saveToken(token)
    
    return nil
}
```

**Backend validation (Cloudflare Workers):**

```typescript
// workers/auth.ts
export async function validateTailscaleIdentity(identity: TailscaleIdentity) {
    // Verify this device really is on the claimed tailnet
    // Option 1: User proves ownership via Tailscale OAuth in Web UI first
    // Option 2: Use Tailscale API to verify device exists
    
    // For v1, require Web UI linkage first
    const account = await KV.get(`tailscale:${identity.tailnet}:${identity.userID}`);
    if (!account) {
        return { error: "Account not linked. Visit app.leger.run first" };
    }
    
    // Generate token
    const token = await generateJWT({
        tailscale_user: identity.userID,
        tailnet: identity.tailnet,
        device: identity.deviceName
    });
    
    return { token };
}
```

### Two-Step Process (First Time Only)

**Step 1: Link in Web UI**
```
User visits app.leger.run
  ↓
"Sign in with Tailscale" button
  ↓
Tailscale OAuth flow
  ↓
Leger knows: This person owns this tailnet
  ↓
Store: tailscale:<tailnet>:<user-id> → linked
```

**Step 2: CLI Uses That Linkage**
```
leger auth login
  ↓
Get local Tailscale identity
  ↓
Call Leger API with identity
  ↓
Leger checks: "Is this tailnet+user linked?"
  ↓
Yes → Issue token
  ↓
Store token locally
```

## Where Tokens Are Stored

### Minimal Approach

```
~/.config/leger/auth.json
{
    "access_token": "eyJhbGciOiJIUzI1NiIs...",
    "expires_at": 1697040000,
    "tailscale_user": "alice@example.ts.net",
    "tailnet": "example.ts.net"
}
```

**Or even simpler - no token at all:**
```go
// Every API call, just prove Tailscale identity
func (c *CloudflareClient) makeRequest(endpoint string) {
    identity := getTailscaleIdentity()
    
    req.Header.Set("X-Tailscale-User", identity.UserID)
    req.Header.Set("X-Tailscale-Tailnet", identity.Tailnet)
    
    // Cloudflare validates with Tailscale
}
```

But this requires Tailscale API key in Cloudflare - might be too much.

## Web UI Device Management

**Minimal v1.0 (Single Device):**

```typescript
// No device management UI needed!
// Just show:
interface UserAccount {
    tailscale_user: string;     // "alice@example.ts.net"
    tailnet: string;            // "example.ts.net"
    current_config_version: number;
    last_seen: string;
}

// Display in Web UI:
Connected Device: blueprint-desktop
Last Sync: 2 minutes ago
Current Config: v3
```

**Future v2.0 (Multi-Device):**

```typescript
interface Device {
    name: string;               // "blueprint-desktop"
    hostname: string;           // "blueprint"
    tailscale_ip: string;       // "100.101.102.103"
    last_seen: string;
    config_version: number;
    status: "online" | "offline";
}

// Web UI shows:
Your Devices:
• blueprint-desktop (v3) - online - 2m ago
• blueprint-laptop (v2) - offline - 3d ago
```

## Recommended: Minimal Auth for Solo Dev

### What You Need:

**Web UI (app.leger.run):**
1. "Sign in with Tailscale" button
2. Tailscale OAuth integration
3. Store: `tailscale_users:<user-id> → account_data`

**CLI (leger):**
1. Read local Tailscale identity (no auth needed!)
2. Call API with identity
3. Cloudflare validates you own that tailnet
4. Store minimal token (or no token, just rely on Tailscale identity)

**No Need For:**
- ❌ Device flow (gh-style codes)
- ❌ Separate password system
- ❌ Email/password auth
- ❌ Device management UI (v1)
- ❌ Token refresh logic (Tailscale identity is the auth)

### Implementation Checklist

```typescript
// Cloudflare Workers - Minimal Auth

// 1. Web UI OAuth (one-time per user)
router.get('/auth/tailscale', (req, env) => {
    // Redirect to Tailscale OAuth
    return Response.redirect(
        `https://login.tailscale.com/authorize?client_id=${CLIENT_ID}&redirect_uri=${REDIRECT}`
    );
});

router.get('/auth/callback', async (req, env) => {
    // Handle OAuth callback
    const code = url.searchParams.get('code');
    const userInfo = await exchangeCodeForUser(code);
    
    // Store linkage
    await env.KV.put(
        `users:${userInfo.tailscale_id}`,
        JSON.stringify({
            tailnet: userInfo.tailnet,
            email: userInfo.email,
            created_at: Date.now()
        })
    );
    
    // Set session cookie
    return new Response('Authenticated!', {
        headers: {
            'Set-Cookie': `session=${makeSessionToken(userInfo)}; HttpOnly; Secure`
        }
    });
});

// 2. CLI Auth Endpoint
router.post('/auth/cli', async (req, env) => {
    const { tailscale_user, tailnet } = await req.json();
    
    // Check if this user/tailnet is linked
    const user = await env.KV.get(`users:${tailscale_user}`);
    if (!user) {
        return jsonError('Not linked. Visit app.leger.run first', 403);
    }
    
    // Generate token (or just return success)
    const token = await generateJWT({ tailscale_user, tailnet });
    
    return json({ token, expires_in: 2592000 }); // 30 days
});
```

```go
// leger CLI - Minimal Auth

func (a *Auth) Login() error {
    // 1. Get Tailscale identity
    tsClient := &tailscale.LocalClient{}
    status, _ := tsClient.Status(context.Background())
    
    identity := status.User[status.Self.UserID].LoginName
    tailnet := status.CurrentTailnet.Name
    
    // 2. Call Leger API
    resp, err := http.Post(
        "https://api.leger.run/auth/cli",
        "application/json",
        strings.NewReader(fmt.Sprintf(`{"tailscale_user":"%s","tailnet":"%s"}`, identity, tailnet)),
    )
    
    if resp.StatusCode == 403 {
        return fmt.Errorf("Account not linked. Please visit https://app.leger.run and sign in with Tailscale first")
    }
    
    // 3. Store token
    var result struct {
        Token string `json:"token"`
    }
    json.NewDecoder(resp.Body).Decode(&result)
    
    // 4. Save to ~/.config/leger/auth.json
    auth := Auth{
        Token: result.Token,
        TailscaleUser: identity,
        Tailnet: tailnet,
    }
    saveAuth(auth)
    
    return nil
}
```

## Your Project List - Scoping Advice

Looking at your list as a solo dev:

### Phase 1: Core (3 months)
1. ✅ **leger CLI** (Go binary)
   - Essential commands: auth, config, deploy, secrets
   - ~2000 lines of Go
   
2. ✅ **RPM packaging**
   - Create .spec file
   - Set up GitHub Actions for builds
   - Host on Cloudflare R2 or GitHub releases
   - ~1 week

3. ✅ **Cloudflare backend**
   - Template engine (TypeScript)
   - API endpoints (auth, config, secrets)
   - Workers KV setup
   - ~1 month

4. ✅ **legerd daemon** (setec fork)
   - Fork setec
   - Minimal customization
   - Systemd service
   - ~2 weeks

### Phase 2: Web UI (2 months)
5. ⚠️ **Web UI for config** (React)
   - **Scope carefully!** This can balloon
   - Start with 2-3 services only
   - OpenWebUI + LiteLLM + basic infrastructure
   - JSON schema → React forms
   - Use existing form libraries (react-hook-form, zod)

### Phase 3+: Advanced (Later)
6. 🔮 **GitHub-based template rendering**
   - **Skip this for v1!** Backend templates work fine
   - Can add later if needed
   
7. 🔮 **Public quadlet appstore**
   - **v2 feature** - focus on your own configs first
   - Let community contribute via PRs

8. ⚠️ **Documentation**
   - **Essential but do incrementally**
   - README first
   - Add docs as you build
   - Don't block development

### Minimal v1.0 Scope

**Must Have:**
- leger + legerd binaries (RPM)
- Cloudflare backend with 3-5 service templates
- Basic web UI (config management for those services)
- Tailscale OAuth in web UI
- CLI auth using Tailscale identity
- Deploy workflow works end-to-end

**Can Wait:**
- Advanced web UI features
- Many service templates
- Public appstore
- GitHub-based rendering
- Device management
- Monitoring/telemetry
- Advanced docs

**Time Estimate:** 4-5 months solo dev

### Authentication: Final Recommendation

**Use pure Tailscale identity:**
1. Web UI: Tailscale OAuth (one button)
2. CLI: Read local Tailscale identity (no browser needed)
3. Backend: Validate tailnet ownership
4. No device codes, no separate passwords
5. Store minimal token or just rely on Tailscale

**Files created:**
- `~/.config/leger/auth.json` (just the token, if needed)
- Web UI can show: "Connected as: alice@example.ts.net"

**This gives you:**
- ✅ Secure (Tailscale's auth)
- ✅ Simple (one OAuth integration)
- ✅ Fast (no device flow)
- ✅ Minimal code (~200 lines)
- ✅ No device management UI needed


---

# Leger Labs Architecture: Cloudflare + Local CLI

## Online Components (Cloudflare)

### **app.leger.run (Cloudflare Pages)**
```
Web UI SPA (React/Vue/Svelte)
├─ Tailscale OAuth integration
├─ Configuration management interface
└─ Secrets input forms
```

**Purpose**: Replace your existing chezmoi dotfiles configuration with a Vercel-admin-quality settings page.

### **api.leger.run (Cloudflare Workers)**
```
Authentication
├─ /auth/tailscale → OAuth flow
├─ /auth/cli → Device authentication flow
└─ Session management

Configuration API
├─ /config/latest → Get current configuration
├─ /config/versions → List all versions
├─ /config/save → Save new configuration
└─ /secrets/store → Store secrets (temporary)

Template Rendering
├─ /templates/render → Render quadlets from config
└─ /templates/publish → Publish to static URL
```

### **Cloudflare Workers KV**
```
User Data
├─ users:{tailscale-id} → user metadata
└─ sessions:{token} → session data

Configuration Storage
├─ configs:{user-id}:latest → current config version
├─ configs:{user-id}:v1 → version 1 config
├─ configs:{user-id}:v2 → version 2 config
└─ configs:{user-id}:vN → version N config

Rendered Templates
├─ rendered:{user-id}:latest → URL to latest rendered quadlets
├─ rendered:{user-id}:v1 → URL to v1 rendered quadlets
└─ manifests:{user-id}:latest → file list + checksums
```

### **Static File Storage (R2 or KV Assets)**
```
/{user-id}/latest/
├─ openwebui.container
├─ openwebui.volume
├─ litellm.container
├─ litellm.yaml
├─ postgres.container
└─ manifest.json

/{user-id}/v1/
├─ openwebui.container
├─ ...
└─ manifest.json
```

## Local Components (Blueprint Linux)

### **Leger CLI**
```
Authentication
├─ Tailscale identity-based
└─ Token stored in ~/.config/leger/auth.json

Template Pulling (pq-style)
├─ Fetches manifest from Cloudflare
├─ Downloads rendered quadlets
├─ Validates checksums
└─ Stages files locally

Secret Fetching
├─ Uses setec client CLI
└─ Injects via `podman secret`

Quadlet Management
├─ Copies to ~/.config/containers/systemd/
├─ systemctl --user daemon-reload
└─ systemctl --user restart <services>
```

### **Podman Quadlets**
```
Deployed Services
├─ openwebui.container → with secrets injected
├─ litellm.container → with secrets injected
├─ postgres.container
└─ All accessible via Tailscale MagicDNS
```

---

## The Complete Flow

### **1. User Configures Online (Web UI)**

```
User visits: https://app.leger.run
  ↓
Signs in with Tailscale OAuth
  ↓
Tailscale redirects to:
  https://login.tailscale.com/authorize?
    client_id=leger-labs
    redirect_uri=https://app.leger.run/auth/callback
  ↓
Worker verifies Tailscale identity:
  {
    "tailscale_id": "u123456",
    "email": "alice@example.ts.net",
    "tailnet": "example.ts.net"
  }
  ↓
Session created in Workers KV:
  sessions:leg_abc123 → {user_id, tailscale_id, expires_at}
  ↓
User lands in dashboard
```

**Configuration Interface:**

```
Dashboard shows:
┌─────────────────────────────────────────┐
│ Leger Labs - AI Stack Configuration    │
├─────────────────────────────────────────┤
│                                         │
│ 🤖 LLM Providers                        │
│   ☑ OpenAI (requires API key)          │
│   ☑ Anthropic (requires API key)       │
│   ☐ Google Gemini                      │
│   ☐ Local inference only               │
│                                         │
│ ✨ Features                             │
│   ☑ Web search (SearXNG)               │
│   ☑ Speech-to-text (Whisper)           │
│   ☑ Code execution (Jupyter)           │
│                                         │
│ 🔐 Secrets                              │
│   OpenAI API Key: [Configure]          │
│   Anthropic API Key: [Configure]       │
│                                         │
│ [Save Configuration]                    │
└─────────────────────────────────────────┘
```

User clicks "Configure" on Anthropic API Key:
```
Modal appears:
┌─────────────────────────────────────────┐
│ Configure: Anthropic API Key            │
│                                         │
│ Value: [sk-ant-...                ]    │
│                                         │
│ This will be:                           │
│ • Stored in your device's setec         │
│ • Accessible only via Tailscale         │
│ • Used by OpenWebUI and LiteLLM         │
│                                         │
│ [Cancel] [Save]                         │
└─────────────────────────────────────────┘
```

User clicks "Save Configuration":
```
POST https://api.leger.run/config/save
{
  "services": {
    "openwebui": {
      "enabled": true,
      "features": {"rag": true, "web_search": true}
    },
    "litellm": {
      "enabled": true,
      "models": {
        "claude-sonnet-4-5": {"provider": "anthropic"}
      }
    }
  },
  "secrets_metadata": {
    "anthropic_api_key": {
      "value": "sk-ant-...",  // Encrypted before storage
      "required_by": ["openwebui", "litellm"]
    }
  }
}
  ↓
Worker processes:
  1. Validates configuration
  2. Increments version: v1 → v2
  3. Stores in KV: configs:alice:v2 → {config JSON}
  4. Triggers template rendering
```

### **2. Template Rendering (Server-Side)**

```
Worker receives new configuration
  ↓
Loads quadlet templates:
  templates/openwebui.container.tmpl
  templates/litellm.container.tmpl
  templates/postgres.container.tmpl
  ...
  ↓
Hydrates templates with configuration:
  
  # openwebui.container.tmpl
  [Container]
  Image=ghcr.io/open-webui/open-webui:main
  {{#if features.rag}}
  Environment=RAG_EMBEDDING_ENGINE=openai
  {{/if}}
  {{#if features.web_search}}
  Environment=ENABLE_RAG_WEB_SEARCH=True
  {{/if}}
  Secret=anthropic_api_key,type=env,target=ANTHROPIC_API_KEY
  
  ↓ Renders to:
  
  [Container]
  Image=ghcr.io/open-webui/open-webui:main
  Environment=RAG_EMBEDDING_ENGINE=openai
  Environment=ENABLE_RAG_WEB_SEARCH=True
  Secret=anthropic_api_key,type=env,target=ANTHROPIC_API_KEY
  ↓
Uploads rendered quadlets to R2:
  /alice/v2/openwebui.container
  /alice/v2/litellm.container
  /alice/v2/postgres.container
  /alice/v2/manifest.json
  ↓
Updates KV:
  rendered:alice:latest → "https://static.leger.run/alice/v2/"
  ↓
Web UI shows:
  "✓ Configuration v2 saved"
  "Run 'leger update' to deploy"
```

**Manifest Structure:**
```json
{
  "version": 2,
  "created_at": "2024-10-15T12:00:00Z",
  "files": [
    {
      "path": "openwebui.container",
      "checksum": "sha256:abc123...",
      "size": 1024
    },
    {
      "path": "litellm.container",
      "checksum": "sha256:def456...",
      "size": 2048
    }
  ],
  "secrets_required": [
    "anthropic_api_key",
    "openai_api_key"
  ]
}
```

### **3. CLI Pulls Configuration (pq-style)**

```bash
$ leger update

[1/7] Authenticating...
      Using Tailscale identity: alice@example.ts.net
      
[2/7] Checking for updates...
      Current version: v1
      Latest version: v2
      
[3/7] Fetching manifest...
      GET https://api.leger.run/config/latest
      ↓ Returns: {
          rendered_url: "https://static.leger.run/alice/v2/",
          version: 2,
          manifest: {manifest JSON}
        }
      
[4/7] Pulling quadlets... (pq-style)
      GET https://static.leger.run/alice/v2/manifest.json
      ↓
      Downloading files:
        openwebui.container [====] 1.0 KB
        litellm.container   [====] 2.0 KB
        postgres.container  [====] 1.5 KB
      ↓
      Verifying checksums:
        ✓ openwebui.container (sha256:abc123...)
        ✓ litellm.container (sha256:def456...)
        ✓ postgres.container (sha256:ghi789...)
      ↓
      Staged in: /tmp/leger-update-v2/
```

**The pq Functionality:**

Similar to git pull, `leger update` implements:
- Fetch manifest (list of files + checksums)
- Download only changed files
- Verify integrity with checksums
- Atomic staging (all-or-nothing)
- Rollback on failure

```bash
[5/7] Fetching secrets...
      Reading manifest.secrets_required:
        - anthropic_api_key
        - openai_api_key
      ↓
      Using setec client:
        $ setec -s http://secrets.example.ts.net get \
            leger/alice/anthropic_api_key
        ↓ Returns: sk-ant-...
        
        $ setec -s http://secrets.example.ts.net get \
            leger/alice/openai_api_key
        ↓ Returns: sk-...
      ↓
      Creating podman secrets:
        $ podman secret create anthropic_api_key -
        $ podman secret create openai_api_key -
      ↓
      ✓ Secrets ready for injection
```

### **4. Secret Injection Flow**

```bash
[6/7] Deploying quadlets...
      
      Copying to systemd directory:
        cp /tmp/leger-update-v2/*.{container,volume} \
           ~/.config/containers/systemd/
      
      Podman secrets already created (step 5)
      
      Quadlets reference secrets via:
        Secret=anthropic_api_key,type=env,target=ANTHROPIC_API_KEY
      
      When systemd starts container, podman:
        1. Reads secret from podman secret store
        2. Injects as environment variable
        3. Container sees: ANTHROPIC_API_KEY=sk-ant-...
      
      Reloading systemd:
        systemctl --user daemon-reload
      
      Starting services:
        systemctl --user restart openwebui
        systemctl --user restart litellm
        systemctl --user restart postgres
```

**Secret Flow Detail:**
```
1. setec client retrieves secret
   ↓ sk-ant-...
   
2. podman secret create injects into podman's secret store
   ↓ Stored in: ~/.local/share/containers/storage/secrets/
   
3. Quadlet references: Secret=anthropic_api_key,type=env,target=...
   ↓
   
4. systemd starts container, podman mounts secret
   ↓
   
5. Container runtime has: ANTHROPIC_API_KEY=sk-ant-...
```

### **5. Runtime Access**

```bash
[7/7] Verifying deployment...
      
      Checking service status:
        ✓ openwebui.service (active)
        ✓ litellm.service (active)
        ✓ postgres.service (active)
      
      Testing endpoints:
        ✓ https://ai.example.ts.net (200 OK)
        ✓ http://litellm.example.ts.net:4000 (200 OK)
      
      ✓ Deployment successful (v1 → v2)
      
      Your AI stack is ready:
        • Chat: https://ai.example.ts.net
        • LiteLLM: http://litellm.example.ts.net:4000
```

---

## Architecture Decisions

### **Why Cloudflare for Account Management?**

1. **Perfect for Configuration Management**
   - Workers KV stores user configurations
   - Fast global distribution
   - Simple key-value access pattern
   - No database to manage

2. **Template Rendering at the Edge**
   - Workers render quadlets server-side
   - User never sees template syntax
   - Validation happens before storage
   - Hydrated templates uploaded to R2

3. **Tailscale OAuth Integration**
   - Natural fit: Tailscale identity = Leger identity
   - No password management needed
   - User's tailnet is the authentication domain
   - MagicDNS already configured

4. **Static Asset Delivery**
   - R2 stores rendered quadlets
   - Global CDN delivery
   - CLI pulls like git (fast, cached)
   - Versioned URLs

### **Why Template Hydration Server-Side?**

**User Never Sees:**
```handlebars
{{#if features.rag}}
Environment=RAG_EMBEDDING_ENGINE={{providers.rag_embedding}}
{{/if}}
```

**User Sees:**
- Web UI checkboxes and dropdowns
- Configuration saved
- `leger update` downloads ready-to-use quadlets

**Benefits:**
- Zero template knowledge required
- Complex logic hidden from users
- Validation before rendering
- Guaranteed valid quadlet syntax

### **Why pq-Style Pulling?**

From Leger spec: "pull-based configuration deployment"

**Git-like workflow:**
```bash
$ leger update
Similar to: git pull origin main

1. Fetch manifest (what's new?)
2. Download only changed files
3. Verify integrity
4. Stage locally
5. Apply atomically
```

**User Control:**
- Explicit command to update
- Shows diff before applying
- Can review changes
- Rollback on failure

### **Why Combine setec client + podman secret?**

**Two-stage secret handling:**

1. **setec client**: Retrieve from secure store
   - Centralized secret management
   - Audit logging
   - Tailscale-authenticated access

2. **podman secret**: Inject into containers
   - Native podman integration
   - Secrets never in quadlet files
   - Memory-only in container
   - systemd integration

**Flow:**
```
setec (retrieval) → podman secret (injection) → container (runtime)
```

---

## Cloudflare Workers KV Schema

### **Users**
```javascript
Key: users:u123456
Value: {
  tailscale_id: "u123456",
  email: "alice@example.ts.net",
  tailnet: "example.ts.net",
  created_at: "2024-10-15T12:00:00Z",
  current_version: 2
}
```

### **Configurations**
```javascript
Key: configs:alice:v2
Value: {
  version: 2,
  created_at: "2024-10-15T12:00:00Z",
  services: {
    openwebui: {enabled: true, features: {...}},
    litellm: {enabled: true, models: {...}}
  },
  secrets_metadata: {
    anthropic_api_key: {required_by: ["openwebui", "litellm"]}
  }
}
```

### **Rendered Templates**
```javascript
Key: rendered:alice:latest
Value: {
  url: "https://static.leger.run/alice/v2/",
  version: 2,
  manifest: {
    files: [
      {path: "openwebui.container", checksum: "sha256:..."}
    ],
    secrets_required: ["anthropic_api_key"]
  }
}
```

---

## Authentication Models

### **Web UI Authentication**

```
Tailscale OAuth
  ↓
Worker verifies with Tailscale API
  ↓
Creates session in KV
  ↓
Returns httpOnly cookie
  ↓
Subsequent requests authenticated via cookie
```

### **CLI Authentication**

```
User runs: leger auth login
  ↓
CLI generates device code
  ↓
User visits: https://app.leger.run/cli/auth
  ↓
Enters code, authorizes via Tailscale
  ↓
CLI polls for confirmation
  ↓
Receives token tied to Tailscale identity
  ↓
Token stored: ~/.config/leger/auth.json
```

### **CLI to Cloudflare**

```
GET https://api.leger.run/config/latest
Headers:
  Authorization: Bearer leg_abc123
  X-Tailscale-User: alice@example.ts.net
  
Worker verifies:
  1. Token valid in KV
  2. Tailscale identity matches
  3. Token not expired
```

---

## Why This Architecture Works

1. **Cloudflare handles configuration** (public data)
   - User preferences
   - Service selections
   - Rendered quadlets
   - Fast, global, cached

2. **setec handles secrets** (private data)
   - API keys
   - Tokens
   - Local-only or Tailscale-only access

3. **Tailscale is the trust boundary**
   - Identity provider
   - Network layer
   - MagicDNS for service discovery

4. **podman native integration**
   - Quadlets are systemd units
   - Secrets are podman secrets
   - No custom orchestration needed

5. **User control preserved**
   - Pull-based updates
   - Explicit commands
   - Diff before apply
   - Rollback on failure

This replaces your chezmoi dotfiles with a web UI while maintaining the same end result: configured quadlets with secrets properly injected, all accessible via Tailscale.


