# task list

- user authentication done through tailscale locally

- version release flow for new versions with conventinal commits, semantic versioning and release-please

- fork tailscale/setec server as legerd; maintain straight conversion script for keeping up to date with upstream through gh action or similar

- podman quadlet management with `pq` readaptation + podman secrets injection module

- go packaged rpm file build and uploaded to pkg on leger.run domain; with gpg key like tailscale

---

### quadlet secrets
```
    - re: secrets `echo "sk-..." | podman secret create openai_api_key -`
# Reference in quadlet
[Container]
Secret=openai_api_key,type=env,target=OPENAI_API_KEY
```


### quadlet backup and staging
see in existing blueprint quadlet module, functionality to be exported to go
```
~/.local/share/bluebuild-quadlets/
├── active/                    # Currently deployed
│   └── llm-stack/
│       ├── litellm.container
│       ├── openwebui.container
│       └── metadata.json
│
├── staged/                    # Downloaded but not applied
│   └── llm-stack-v1.3.0/
│       ├── litellm.container
│       └── new-service.container
│
├── backups/                   # Rollback points
│   └── llm-stack/
│       ├── 2025-01-15-pre-v1.3.0/
│       │   ├── quadlets/
│       │   ├── volumes.tar.gz
│       │   └── metadata.json
│       └── 2025-01-10-manual/
│
└── config.yaml                # User overrides
```

## old prototype "status" flag
```
### **Infrastructure**

**Section: Network**
```
┌─────────────────────────────────────┐
│ Network Configuration               │
├─────────────────────────────────────┤
│ Name:     [llm                    ] │
│ Subnet:   [10.89.0.0/24           ] │
│ Gateway:  [10.89.0.1              ] │
└─────────────────────────────────────┘
```

**Section: Service Registry**
```
┌────────────────────────────────────────────────────┐
│ Service                 Port    Published   Status │
├────────────────────────────────────────────────────┤
│ □ LiteLLM              4000    4000        ●      │
│ └─ □ PostgreSQL        5432    -           ●      │
│ └─ □ Redis             6379    -           ●      │
│                                                     │
│ □ OpenWebUI            8080    3000        ●      │
│ └─ □ PostgreSQL        5432    -           ●      │
│ └─ □ Redis             6379    -           ●      │
│ └─ Requires: LiteLLM                               │
│                                                     │
│ □ Jupyter              8888    8889        -      │
│ └─ Requires: LiteLLM                               │
│ └─ Enabled by: OpenWebUI → Code Execution         │
└────────────────────────────────────────────────────┘


---

reference implementation: 
in /etc/yum.system.d/tailscale-repo
```
[tailscale-stable]
name=Tailscale stable
baseurl=https://pkgs.tailscale.com/stable/fedora/$basearch
enabled=1
type=rpm
repo_gpgcheck=0
gpgcheck=0
gpgkey=https://pkgs.tailscale.com/stable/fedora/repo.gpg
```
want the exact same up to the gpg signing

Build RPM spec that installs:

/usr/bin/leger
/usr/bin/legerd
/usr/lib/systemd/system/legerd.service


Replace [tailscale-stable] with [leger-stable]

name=Leger stable
baseurl=https://pkgs.leger.run/fedora/$basearch
enabled=1
gpgcheck=0


Add .github automation:

release-please.yml

semantic-pr.yml

ci.yml

Outcome

leger run builds, installs, and starts.

Cloudflare backend stub + local secrets store verified.
---
🔐 Security Model

Secrets lifecycle:

Created via Web UI → encrypted (Cloudflare KV)

Pulled via CLI (leger secrets sync)

Stored locally in encrypted SQLite (/var/lib/legerd/secrets.db)

Decrypted only at runtime into tmpfs (/run/user/<UID>/*.env)

Deleted on container stop
→ No persistent plaintext anywhere

Access control:

legerd only accessible via Tailscale (or localhost)

CLI authenticated via JWT/device flow from Cloudflare

Containers access secrets indirectly (no network tokens)

🧰 Developer Workflow

Install RPM → sets up CLI + daemon + systemd unit

Authenticate (leger auth login)

Deploy (leger deploy init) → pulls config, secrets, and writes Quadlets

Run containers → secrets fetched at startup

Update or rotate secrets → simple leger config pull or leger secrets sync

⚙️ Implementation Strategy (Go-side)

leger CLI

Commands: auth, config, deploy, secrets, services

Uses internal HTTP client for both Cloudflare API and legerd local API

Handles JSON config/state under /var/lib/leger/ and ~/.local/share/leger/

legerd Daemon

Fork of setec with Leger-specific API and file layout

REST API on localhost:8080

SQLite backend with local encryption key

Systemd-managed service

Both components share internal packages (internal/cloudflare, internal/daemon, internal/secrets).

📦 Packaging

You’ll deliver an RPM that installs:

/usr/bin/leger

/usr/bin/legerd

/usr/lib/systemd/system/legerd.service

/var/lib/leger[d]/ directories for persistent state

And the post-install script enables and starts legerd.

🏗️ Overall Summary

Leger =

“A personal, Tailscale-secured, Cloudflare-backed Podman deployment manager with first-class secret handling.”

It’s like HashiCorp Vault + Kubernetes + Tailscale + Fly.io, distilled into one local-first tool designed for fast, secure AI service deployment.

---

add to readme.md for leger account configuration: This project requires a tailscale account with the following:
A Tailscale network (tailnet) with magicDNS and HTTPS enabled - This is mandatory as tsidp relies on Tailscale's secure DNS and certificate infrastructure
