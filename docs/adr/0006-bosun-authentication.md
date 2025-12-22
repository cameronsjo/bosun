# ADR-0006: Bosun Authentication

## Status

Proposed

## Context

The bosun webhook endpoint receives GitHub push events. It needs authentication to:
1. Verify requests come from GitHub (webhook signature)
2. Optionally require additional auth layer (defense in depth)

Current: GitHub webhook secret only.
Proposed: Support optional Authelia/OIDC integration.

## Decision

**Batteries included, but swappable:**

1. **Default**: GitHub webhook signature verification (sufficient for most)
2. **Optional**: Authelia ForwardAuth for additional protection
3. **Future**: Generic OIDC support

## Authentication Layers

```
┌─────────────────────────────────────────────────────────────┐
│                     Defense in Depth                        │
├─────────────────────────────────────────────────────────────┤
│  Layer 1: Tunnel Auth (Cloudflare Access / Tailscale ACL)   │
├─────────────────────────────────────────────────────────────┤
│  Layer 2: Authelia ForwardAuth (optional)                   │
├─────────────────────────────────────────────────────────────┤
│  Layer 3: GitHub Webhook Signature (HMAC-SHA256)            │
└─────────────────────────────────────────────────────────────┘
```

## Layer 1: Tunnel-Level Auth

### Tailscale
- ACLs can restrict which devices reach the funnel
- Limited to Tailscale network members

### Cloudflare Access
- Require specific identity providers
- IP allowlisting (GitHub webhook IPs)
- Rate limiting at edge

## Layer 2: Authelia ForwardAuth (Optional)

For users who want Authelia protecting the webhook endpoint:

```yaml
# traefik dynamic config
http:
  routers:
    bosun-hooks:
      rule: "Host(`hooks.example.com`) && PathPrefix(`/hooks`)"
      middlewares:
        - authelia@docker  # Optional: remove for webhook-secret-only
      service: bosun
```

### Authelia Bypass for Webhooks

GitHub can't authenticate with Authelia. Two approaches:

**Option A: Bypass Rule (Recommended)**
```yaml
# authelia configuration.yml
access_control:
  rules:
    # Allow GitHub webhook IPs without auth
    - domain: hooks.example.com
      policy: bypass
      networks:
        - 192.30.252.0/22   # GitHub webhook IPs
        - 185.199.108.0/22
        - 140.82.112.0/20
        - 143.55.64.0/20
```

**Option B: Service Token**
```yaml
# authelia configuration.yml
access_control:
  rules:
    - domain: hooks.example.com
      policy: one_factor
      subject:
        - "group:service-accounts"
```

Then use Authelia's API keys or service tokens (if/when supported).

## Layer 3: GitHub Webhook Signature

Always enabled. Non-negotiable.

```bash
# Verify signature in bosun
verify_signature() {
    local payload="$1"
    local signature="$2"
    local secret="$GITHUB_WEBHOOK_SECRET"

    expected=$(echo -n "$payload" | openssl dgst -sha256 -hmac "$secret" | cut -d' ' -f2)

    if [[ "sha256=$expected" != "$signature" ]]; then
        echo "Invalid signature"
        return 1
    fi
}
```

## Configuration

```yaml
# bosun/config.yml
auth:
  # GitHub webhook secret (required)
  github_webhook_secret: ${GITHUB_WEBHOOK_SECRET}

  # Additional auth layer (optional)
  middleware: none  # or: authelia, cloudflare-access

  authelia:
    # Only used if middleware: authelia
    bypass_networks:
      - 192.30.252.0/22
      - 185.199.108.0/22
      - 140.82.112.0/20
      - 143.55.64.0/20
```

## Recommendations

| Environment | Recommended Layers |
|-------------|-------------------|
| **Homelab** | GitHub signature only |
| **Small team** | + Cloudflare Access IP allowlist |
| **Enterprise** | + Authelia with bypass rules |

## Security Considerations

1. **Webhook secret rotation**: Support graceful rotation (accept old+new during transition)
2. **IP allowlisting**: GitHub IPs change; fetch from `https://api.github.com/meta`
3. **Rate limiting**: Implement at tunnel layer, not bosun
4. **Audit logging**: Log all webhook attempts (success and failure)

## Future Work

- Generic OIDC provider support (not just Authelia)
- Mutual TLS for service-to-service auth
- API key management for non-GitHub triggers

## References

- [GitHub Webhook Security](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries)
- [Authelia Access Control](https://www.authelia.com/configuration/security/access-control/)
- [GitHub Meta API](https://api.github.com/meta)
