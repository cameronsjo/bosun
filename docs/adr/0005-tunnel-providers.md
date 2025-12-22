# ADR-0005: Swappable Tunnel Providers

## Status

Accepted

## Context

The bosun needs to receive GitHub webhooks from the internet. This requires exposing an endpoint securely without opening firewall ports.

Two main approaches exist:
1. **Tailscale Funnel** - Zero-config, built into Tailscale
2. **Cloudflare Tunnel** - Free tier, broader feature set

Both are valid. The architecture should support either.

## Decision

**Support both Tailscale Funnel and Cloudflare Tunnel as swappable tunnel providers.**

Default to Tailscale Funnel (simpler setup), but document Cloudflare Tunnel as alternative.

## Comparison

| Feature | Tailscale Funnel | Cloudflare Tunnel |
|---------|------------------|-------------------|
| **Setup** | 1 command | Tunnel + DNS config |
| **Cost** | Free (included) | Free tier available |
| **Auth** | Tailscale ACLs | Cloudflare Access |
| **DDoS** | Basic | Enterprise-grade |
| **Caching** | None | Edge caching |
| **Analytics** | Minimal | Detailed |
| **Custom domains** | `*.ts.net` only | Any domain |
| **Dependencies** | Tailscale account | Cloudflare account + domain |

## Tailscale Funnel (Default)

### Pros
- Already have Tailscale for internal networking
- Zero additional config - just `tailscale funnel`
- Automatic TLS
- No domain required

### Cons
- URLs are `*.ts.net` (not custom domain)
- Limited to Tailscale ecosystem
- Less edge features

### Setup

```yaml
# bosun/docker-compose.yml
services:
  tailscale-gateway:
    image: tailscale/tailscale:latest
    environment:
      TS_AUTHKEY: ${TS_AUTHKEY}
      TS_SERVE_CONFIG: /config/serve.json
    volumes:
      - ./serve.json:/config/serve.json:ro
```

```json
// serve.json
{
  "TCP": { "443": { "HTTPS": true } },
  "Web": {
    "gateway.tail${TAILNET}.ts.net:443": {
      "Handlers": {
        "/hooks/": { "Proxy": "http://bosun:8080" }
      }
    }
  }
}
```

## Cloudflare Tunnel (Alternative)

### Pros
- Custom domain support
- DDoS protection
- Edge caching for static assets
- Cloudflare Access for additional auth layer
- Better analytics

### Cons
- Requires Cloudflare account
- Domain must be on Cloudflare DNS
- More moving parts

### Setup

```yaml
# bosun/docker-compose.cloudflare.yml
services:
  cloudflared:
    image: cloudflare/cloudflared:latest
    command: tunnel run
    environment:
      TUNNEL_TOKEN: ${CLOUDFLARE_TUNNEL_TOKEN}
```

```yaml
# Cloudflare tunnel config (via dashboard or CLI)
ingress:
  - hostname: hooks.example.com
    service: http://bosun:8080
  - service: http_status:404
```

### Cloudflare Access (Optional Auth Layer)

```yaml
# Additional protection via Cloudflare Access policies
# - Require GitHub OAuth for webhook endpoint
# - IP allowlist for GitHub webhook IPs
# - Rate limiting at edge
```

## Swappable Architecture

The bosun doesn't care which tunnel is used. It just listens on port 8080:

```
                    ┌─────────────────┐
                    │    Internet     │
                    └────────┬────────┘
                             │
         ┌───────────────────┴───────────────────┐
         │                                       │
         ▼                                       ▼
┌─────────────────┐                   ┌─────────────────┐
│ Tailscale Funnel│                   │Cloudflare Tunnel│
│  *.ts.net:443   │                   │ hooks.example.com│
└────────┬────────┘                   └────────┬────────┘
         │                                     │
         └───────────────┬─────────────────────┘
                         │
                         ▼
              ┌─────────────────┐
              │      Bosun      │
              │   :8080/hooks   │
              └─────────────────┘
```

## Configuration

```yaml
# unops.yml (future)
tunnel:
  provider: tailscale  # or: cloudflare

  tailscale:
    authkey: ${TS_AUTHKEY}
    hostname: gateway

  cloudflare:
    token: ${CLOUDFLARE_TUNNEL_TOKEN}
    hostname: hooks.example.com
```

## Migration Path

Switching tunnels:
1. Deploy new tunnel alongside existing
2. Update GitHub webhook URL
3. Verify webhooks arriving
4. Remove old tunnel

No bosun changes required.

## References

- [Tailscale Funnel](https://tailscale.com/kb/1223/funnel)
- [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/)
- [GitHub Webhook IPs](https://api.github.com/meta)
