# Leger Labs - Tailscale Integration Analysis

**Version:** 1.0  
**Date:** October 2024  
**Purpose:** Define Tailscale dependencies and engagement scenarios

---

## I. Current Tailscale Usage Inventory

### A. Identity & Authentication

**1. User Identity Verification**
- **What:** Linking Leger accounts to Tailscale identities
- **How:** CLI uses Tailscale whoami to get user identity
- **Coupling Level:** CRITICAL
- **Alternatives:** OAuth flow independent of Tailscale
- **Impact if unavailable:** Cannot verify user identity for secrets/config access

**2. Device Authentication**
- **What:** Verifying device belongs to authenticated user
- **How:** Device must be on user's tailnet and authenticated
- **Coupling Level:** CRITICAL
- **Alternatives:** Device-specific API keys/tokens
- **Impact if unavailable:** Cannot securely associate device with user account

**3. Setec Authentication**
- **What:** Accessing user secrets from Setec server
- **How:** Setec uses Tailscale identity to authorize secret access
- **Coupling Level:** CRITICAL (if using Tailscale-hosted Setec)
- **Alternatives:** Self-hosted Setec with different auth
- **Impact if unavailable:** Cannot retrieve user secrets

### B. Networking & Service Discovery

**4. MagicDNS for Service Access**
- **What:** Accessing services via human-readable URLs
- **How:** 
  - `https://ai.<tailnet>.ts.net` → OpenWebUI
  - `https://jupyter.<tailnet>.ts.net` → Jupyter
  - `https://search.<tailnet>.ts.net` → SearXNG
- **Coupling Level:** HIGH (user-facing)
- **Alternatives:** 
  - Raw Tailscale IPs (less user-friendly)
  - Custom DNS via leger.run subdomain forwarding
- **Impact if unavailable:** Users must use IP addresses

**5. Tailscale Subnet Router (Optional)**
- **What:** Making services accessible to tailnet devices
- **How:** Device advertises routes for service subnets
- **Coupling Level:** MEDIUM (optional feature)
- **Alternatives:** Services only accessible on local device
- **Impact if unavailable:** Cannot access services from other tailnet devices

**6. Tailscale HTTPS Certificates**
- **What:** TLS certificates for *.ts.net domains
- **How:** Tailscale automatically provisions certs for MagicDNS names
- **Coupling Level:** HIGH (security)
- **Alternatives:** Self-signed certs (browser warnings) or custom CA
- **Impact if unavailable:** HTTPS warnings or manual cert management

### C. Security & Encryption

**7. WireGuard Encrypted Transport**
- **What:** All Leger ↔ Device communication encrypted
- **How:** Traffic flows over Tailscale VPN
- **Coupling Level:** CRITICAL
- **Alternatives:** TLS over public internet
- **Impact if unavailable:** Must implement separate encryption layer

**8. Zero-Trust Network Model**
- **What:** Services not exposed to public internet
- **How:** Services only accessible via tailnet
- **Coupling Level:** HIGH (security model)
- **Alternatives:** Firewall rules + VPN
- **Impact if unavailable:** Must implement separate access control

### D. Backend Communication

**9. Leger Backend ↔ Device Communication**
- **What:** CLI fetches configs from Leger backend
- **How:** Backend accessible via Tailscale (e.g., api.leger.ts.net)
- **Coupling Level:** CRITICAL
- **Alternatives:** Public HTTPS endpoint with different auth
- **Impact if unavailable:** Must expose backend publicly

**10. Setec Server Access**
- **What:** CLI retrieves secrets from Setec
- **How:** Setec accessible via Tailscale
- **Coupling Level:** CRITICAL (if using Tailscale-hosted)
- **Alternatives:** Self-hosted Setec on different network
- **Impact if unavailable:** Must deploy separate secrets infrastructure

### E. Configuration Management

**11. Tailscale Hostname Detection**
- **What:** Determining device's Tailscale hostname
- **How:** CLI runs `tailscale status` to get hostname
- **Coupling Level:** MEDIUM
- **Alternatives:** User manually configures hostname
- **Impact if unavailable:** Cannot auto-configure Caddy routes

**12. Tailnet Membership Verification**
- **What:** Ensuring device is on correct tailnet
- **How:** CLI checks device belongs to user's tailnet
- **Coupling Level:** HIGH
- **Alternatives:** Manual verification or trust-based
- **Impact if unavailable:** Risk of config being applied to wrong device

---

## II. "Whitegloving" Decision: leger.run vs Tailscale URLs

### Option A: Tailscale-Native URLs (Lower Effort)

**User-Facing URLs:**
```
https://ai.<tailnet>.ts.net              # OpenWebUI
https://jupyter.<tailnet>.ts.net         # Jupyter
https://search.<tailnet>.ts.net          # SearXNG
https://blueprint.<tailnet>.ts.net       # Cockpit
```

**Pros:**
- Zero additional infrastructure
- Automatic HTTPS certificates from Tailscale
- No DNS management required
- Built-in to Tailscale (reliable, maintained)
- Users see Tailscale branding (builds trust if partnership exists)

**Cons:**
- Less Leger branding
- URLs expose user's tailnet name
- Dependent on Tailscale DNS infrastructure
- Cannot customize domain

**Implementation Effort:** Minimal (already implemented)

---

### Option B: Leger-Branded URLs (Higher Effort)

**User-Facing URLs:**
```
https://ai.leger.run                     # OpenWebUI
https://jupyter.leger.run                # Jupyter
https://search.leger.run                 # SearXNG
https://system.leger.run                 # Cockpit
```

**Technical Approach:**
1. User's device runs Caddy with:
   - Subdomain routing (ai.leger.run → local port)
   - TLS certificate provisioning (Let's Encrypt)
2. Leger DNS (CloudFlare) has:
   - Wildcard A record: *.leger.run → Tailscale IP per user
   - Or: User-specific subdomains (ai-user123.leger.run)

**Pros:**
- Consistent Leger branding
- Professional appearance
- Hides Tailscale implementation details
- Could support custom domains in future

**Cons:**
- Requires Leger to manage DNS infrastructure
- TLS certificate management complexity
- Must maintain mapping of user → Tailscale IP
- Privacy concern: Leger knows user's Tailscale IP
- Additional point of failure (Leger DNS)

**Implementation Effort:** High (custom DNS infrastructure)

---

### Option C: Hybrid Approach (Recommended)

**Default:** Tailscale URLs (ai.<tailnet>.ts.net)  
**Optional:** Leger URLs for users who want branded access

**Implementation:**
- V1: Tailscale-native URLs only
- V2: Add optional Leger-branded URLs for premium users
- Configuration flag: `use_leger_domains: true/false`

**Benefits:**
- Fast time-to-market with V1
- Branded experience available later
- Users choose their preference
- Graceful degradation if Leger DNS has issues

---

## III. Setec Deep Dive

### Current Understanding (Based on Tailscale Setec)

**What is Setec?**
- Secrets management service developed by Tailscale
- Open source: https://github.com/tailscale/setec
- Designed for Tailscale networks

**Authentication Model:**
- Uses Tailscale identity for authorization
- Namespaced secrets: `leger/<user-id>/<secret-name>`
- Access control via Tailscale ACLs

### Hosting Options for Leger

**Option 1: Tailscale-Hosted Setec (Partnership Required)**
```
Pros:
- Zero infrastructure management
- Maintained by Tailscale
- High availability
- Native integration

Cons:
- Requires partnership or paid plan
- Limited customization
- Dependent on Tailscale roadmap
- Pricing unclear (per user? per secret?)
```

**Option 2: Self-Hosted Setec (Full Control)**
```
Pros:
- Full control over deployment
- Can offer free tier
- Custom features possible
- Data sovereignty

Cons:
- Must maintain infrastructure
- Responsible for availability
- Security burden
- Additional ops complexity
```

**Option 3: Hybrid (Recommended for Free Service)**
```
Free Tier: Self-hosted Setec
- Limited secrets (e.g., 5 API keys)
- Community support
- Leger-managed infrastructure

Premium Tier: Tailscale-hosted Setec (if partnership)
- Unlimited secrets
- Tailscale SLA
- Priority support
```

### Technical Configuration for Self-Hosted Setec

**Infrastructure Requirements:**
```yaml
Compute:
  - 2 vCPU, 4GB RAM (start)
  - Scales with user count
  
Storage:
  - PostgreSQL for secret metadata
  - Encrypted at rest
  - Daily backups

Networking:
  - Accessible via Tailscale (setec.leger.ts.net)
  - Or public HTTPS with Tailscale auth
  
Security:
  - Tailscale ACLs for access control
  - Audit logging of all operations
  - Secrets encrypted with user-specific keys
```

**Per-User Isolation:**
```
leger/user-123/openai_api_key
leger/user-123/anthropic_api_key
leger/user-456/openai_api_key
```

**Access Pattern:**
```
1. User saves secret via Web UI
   → Web UI → Leger Backend → Setec API
   → Stored at: leger/<user-id>/<secret-name>

2. Device retrieves secret via CLI
   → CLI → Leger Backend → Setec API
   → Setec verifies: device Tailscale identity matches user-id
   → Returns encrypted secret
```

### Cost Implications (Free Service)

**Self-Hosted Setec Costs:**
```
Base Infrastructure:
- Compute: $20-40/month (DigitalOcean, Hetzner)
- Storage: $10/month (PostgreSQL managed)
- Bandwidth: Minimal (<$5/month)
Total: ~$40-60/month

Per 1000 Users:
- Additional compute: +$20/month
- Storage: +$5/month (assuming 5 secrets/user)
Total: ~$25 per 1000 users

Break-even: ~2000 users before considering monetization
```

---

## IV. Tailscale Engagement Scenarios

### Scenario 1: Deep Partnership (Ideal Case)

**What Leger Asks For:**
1. **Setec Hosting**
   - Access to Tailscale-hosted Setec instance
   - Dedicated namespace for Leger users
   - SLA for secret availability
   - Pricing: per-user or flat monthly fee

2. **Technical Support**
   - Designated technical contact
   - Priority support for Leger-related issues
   - Early access to new features (e.g., Funnel improvements)
   - Input on Tailscale roadmap for features Leger needs

3. **Co-Marketing**
   - Case study: "Leger builds on Tailscale"
   - Joint blog posts/webinars
   - Featured in Tailscale newsletter
   - Logo placement (Leger on Tailscale site, vice versa)

4. **Preferred Pricing**
   - Discounted Tailscale plan for Leger users
   - Revenue share if users upgrade to paid Tailscale
   - Free Tailscale seats for Leger team

5. **API/Feature Access**
   - Tailscale API quota increase
   - Beta access to new features
   - Custom MagicDNS subdomains (e.g., *.leger.tailscale.com)

**What Leger Offers:**
- Drive Tailscale adoption (each Leger user = Tailscale user)
- Showcase Tailscale for developer tools
- Feedback on Tailscale features
- Case study content
- Community engagement

**Likelihood:** Medium  
**Timeline:** 3-6 months (requires business development)

---

### Scenario 2: Third-Party Integration (Likely Case)

**What Leger Does:**
1. **Self-Hosted Setec**
   - Deploy and maintain own Setec instance
   - Use Tailscale auth but Leger infrastructure
   - Full control over secrets management

2. **Standard Tailscale Usage**
   - Users install Tailscale independently
   - Leger provides clear installation guides
   - No special Tailscale relationship required

3. **Documentation & Support**
   - Comprehensive Tailscale setup guide for users
   - Troubleshooting docs for Tailscale issues
   - Community forum for peer support
   - Fallback: encourage users to contact Tailscale support

4. **Tailscale API Usage**
   - Use public Tailscale API within rate limits
   - No special API access
   - Implement retry logic for rate limiting

**User Experience:**
```
Installation Flow:
1. User installs Blueprint Linux
2. User installs Tailscale (separate step)
3. User authenticates Tailscale
4. User installs Leger CLI
5. Leger verifies Tailscale is running
6. Deployment proceeds
```

**Pros:**
- No dependency on Tailscale business relationship
- Can launch immediately
- Full control over secrets infrastructure
- Clear separation of concerns

**Cons:**
- Additional setup steps for users
- No guaranteed Tailscale support
- Must maintain Setec infrastructure
- No co-marketing benefits

**Likelihood:** High  
**Timeline:** Immediate (current path)

---

### Scenario 3: Self-Sufficient (Fallback)

**If Tailscale Relationship Fails:**

1. **Fork to Headscale**
   - Replace Tailscale with Headscale (open-source coordinator)
   - Self-host coordination server
   - Same WireGuard tech, different control plane
   - **Effort:** 4-8 weeks development

2. **Custom WireGuard Setup**
   - Direct WireGuard configuration
   - Manual key management
   - Custom DNS solution
   - **Effort:** 8-12 weeks development
   - **Complexity:** Very high (not recommended)

3. **Public Internet with OAuth**
   - Drop VPN requirement entirely
   - Use standard OAuth for auth
   - TLS for encryption
   - Firewall rules for security
   - **Effort:** 4-6 weeks development
   - **Security:** Reduced (services exposed to internet)

**Recommendation:** Only pursue if:
- Tailscale fundamentally changes pricing/terms
- Tailscale shuts down (unlikely)
- Enterprise customers require air-gapped deployment

**Likelihood:** Low  
**Timeline:** 6-12 months (only if needed)

---

## V. Recommended Approach & Tailscale Pitch

### Immediate Actions (Month 1)

**1. Launch with Scenario 2 (Third-Party)**
- Build on standard Tailscale
- Self-host Setec
- Create excellent Tailscale setup docs
- Validate product-market fit

**2. Track Metrics**
- Number of Leger users
- Tailscale adoption rate
- Support burden related to Tailscale
- User feedback on setup complexity

### Partnership Outreach (Month 2-3)

**When to Approach Tailscale:**
- After 100+ active users
- With documented case study
- Clear value proposition ready

**Pitch Email Template:**

```
Subject: Leger Labs - Building AI Infrastructure on Tailscale

Hi [Tailscale BD/Partnership Contact],

I'm [Your Name], founder of Leger Labs. We've built a managed AI 
infrastructure platform that helps non-technical users deploy local 
LLM workloads on AMD hardware.

Tailscale is a critical component of our architecture:
- Identity/auth for user verification
- MagicDNS for service discovery
- WireGuard for secure communication
- Setec for secrets management

We launched 2 months ago and now have [XXX] active users, all of whom 
use Tailscale. Our users love the zero-config networking experience.

I'd like to explore a deeper partnership:
1. Using Tailscale-hosted Setec for secrets
2. Co-marketing opportunities
3. Technical support for Leger-specific use cases

Would you be open to a 30-minute call to discuss?

Best,
[Your Name]

Current metrics:
- [XXX] active users
- [XXX] daily API calls to Tailscale
- [XXX] GitHub stars / community engagement
```

### Decision Tree

```
Launch with Scenario 2 (Third-Party)
         |
         v
After 3 months: Do we have product-market fit?
         |
    ┌────┴────┐
    NO        YES
    |          |
    v          v
Pivot or    Approach Tailscale
shutdown    for partnership
              |
         ┌────┴────┐
    Partnership  No partnership
    succeeds     (declined)
         |          |
         v          v
    Scenario 1   Continue
    (Deep)       Scenario 2
                     |
                     v
               Scale self-hosted
               infrastructure
```

---

## VI. Technical Integration Details

### systemd-resolved Configuration (from user)

```ini
[Resolve]
# DNSSEC validation
DNSSEC=allow-downgrade

# DNS-over-TLS when available
DNSOverTLS=opportunistic

# Enable DNS caching
Cache=yes

# Enable stub listener
DNSStubListener=yes

# Use systemd-resolved for all domains
Domains=~.
```

**Purpose:** Prepare system for Tailscale MagicDNS integration
**Why:** Ensures Tailscale can properly configure DNS resolution
**Alternative:** Tailscale can modify /etc/resolv.conf directly (less clean)

### Caddy Configuration (Tailscale Integration)

```
# Example Caddy configuration for Tailscale MagicDNS
ai.{$TAILSCALE_HOSTNAME}.ts.net {
    reverse_proxy openwebui:3000
    tls {
        # Tailscale automatically provisions certs
        get_certificate tailscale
    }
}

jupyter.{$TAILSCALE_HOSTNAME}.ts.net {
    reverse_proxy jupyter:8888
    tls {
        get_certificate tailscale
    }
}
```

**Environment Variable:**
```bash
TAILSCALE_HOSTNAME=$(tailscale status --json | jq -r '.Self.HostName')
```

### CLI Tailscale Verification

```bash
# Verify Tailscale is installed and running
leger init
  → Check: tailscale version
  → Check: tailscale status (exit code 0)
  → Check: Device is authenticated
  → Get: Device Tailscale IP
  → Get: Device hostname
  → Get: Tailnet name
  → Verify: User identity matches Leger account
```

---

## VII. Risk Assessment

### Critical Risks (Tailscale Dependency)

**Risk 1: Tailscale Changes Pricing**
- **Impact:** HIGH - Could make Leger unaffordable for users
- **Likelihood:** LOW - Tailscale has stable free tier
- **Mitigation:** 
  - Monitor Tailscale pricing announcements
  - Have Headscale migration plan ready
  - Offer to pay for user Tailscale seats if needed

**Risk 2: Tailscale API Rate Limits**
- **Impact:** MEDIUM - Could block config deployments
- **Likelihood:** LOW - Current usage minimal
- **Mitigation:**
  - Cache Tailscale API responses
  - Implement exponential backoff
  - Use webhooks instead of polling (if available)

**Risk 3: Tailscale Service Outage**
- **Impact:** MEDIUM - Cannot deploy new configs, but existing services continue
- **Likelihood:** LOW - Tailscale has good uptime
- **Mitigation:**
  - Graceful degradation in CLI
  - Cache last-known-good config locally
  - Clear user communication

**Risk 4: User Doesn't Have Tailscale**
- **Impact:** HIGH - Cannot use Leger
- **Likelihood:** MEDIUM - Setup friction
- **Mitigation:**
  - Excellent installation docs
  - One-click Tailscale install script
  - Video tutorials

**Risk 5: Setec Availability**
- **Impact:** CRITICAL - Cannot retrieve secrets
- **Likelihood:** Depends on hosting choice
- **Mitigation:**
  - If self-hosted: Multi-region deployment
  - If Tailscale-hosted: SLA agreement
  - Local secret caching (encrypted)

---

## VIII. Conclusions & Recommendations

### Short-Term (0-6 months)

**Recommended Path:** Scenario 2 (Third-Party Integration)

**Rationale:**
- No business dependencies
- Fast time to market
- Validates product-market fit
- Self-hosted Setec is manageable at small scale

**Action Items:**
1. Deploy self-hosted Setec instance
2. Create comprehensive Tailscale setup guide
3. Implement robust Tailscale verification in CLI
4. Use Tailscale-native URLs (ai.<tailnet>.ts.net)
5. Track usage metrics for future partnership pitch

### Medium-Term (6-12 months)

**If product gains traction (>500 users):**
- Approach Tailscale for partnership (Scenario 1)
- Pitch: co-marketing, Setec hosting, technical support
- Prepare metrics: user count, API usage, case study

**If partnership succeeds:**
- Migrate to Tailscale-hosted Setec
- Implement co-marketing initiatives
- Explore custom MagicDNS subdomains

**If partnership fails:**
- Continue with self-hosted Setec
- Scale infrastructure accordingly
- Maintain good standing with Tailscale community

### Long-Term (12+ months)

**If Tailscale relationship becomes problematic:**
- Evaluate Headscale migration (Scenario 3)
- Only pursue if absolutely necessary
- 6+ month development timeline

**If everything is working:**
- Deepen Tailscale integration
- Explore advanced features (Funnel, SSH, etc.)
- Contribute to Tailscale open-source projects

---

## IX. Questions for Tailscale Team

If you do get a partnership conversation, ask:

**Technical:**
1. Can Leger access a Tailscale-hosted Setec instance?
2. What are the authentication requirements for Setec access?
3. Are there API rate limits we should be aware of?
4. Can we get custom MagicDNS subdomains (*.leger.tailscale.com)?
5. What's the roadmap for Setec features?

**Business:**
1. What partnership tiers exist?
2. Is there a preferred pricing model for users (free/paid)?
3. Can we get discounted Tailscale plans for Leger users?
4. Are you interested in co-marketing opportunities?
5. What metrics would make Leger interesting as a partner?

**Support:**
1. What level of technical support is available?
2. Can we get a designated technical contact?
3. How do we escalate Leger-specific issues?
4. Are there SLAs we can depend on?

---

## X. Next Steps

**For You (Founder):**
1. Review this analysis and identify any missing dependencies
2. Decide on "whitegloving" approach (leger.run vs Tailscale URLs)
3. Clarify Setec hosting decision (self-host vs wait for partnership)
4. Draft initial Tailscale installation guide for users
5. Set metrics targets for when to approach Tailscale

**For Technical Team:**
1. Implement Tailscale verification in CLI
2. Deploy self-hosted Setec (if chosen)
3. Create systemd-resolved configuration templates
4. Build Caddy templates with Tailscale MagicDNS
5. Test full flow with Tailscale integration

**For Documentation:**
1. Write "Setting Up Tailscale for Leger" guide
2. Create troubleshooting docs for Tailscale issues
3. Document MagicDNS URL format
4. Explain why Tailscale is required (security, ease of use)

---

**Document Status:** Complete  
**Next Review:** After initial Tailscale outreach or 6 months  
**Owner:** Leger Labs Founder
