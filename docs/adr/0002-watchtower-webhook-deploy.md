# ADR-0002: Watchtower Webhook Deploy for App Code Changes

## Status

Accepted

## Context

The homelab uses two deployment patterns:

1. **Infrastructure changes** (Traefik, Authelia, compose files) - GitOps runner watches dotfiles repo, deploys on push via webhook
2. **App code changes** (llm-council, custom apps) - GitHub Actions builds image, pushes to GHCR

Problem: App code changes weren't being deployed because:

- GitOps only triggers on YAML/config changes in dotfiles
- Watchtower polls daily (86400s interval)
- Images built after last poll wait up to 24h for deployment

## Decision

Enable Watchtower HTTP API and trigger it via GitHub Actions webhook after successful image push.

```
push → GitHub Actions → build → GHCR → POST /watchtower/v1/update → instant deploy
```

### Implementation

1. **Watchtower HTTP API** - Enable `WATCHTOWER_HTTP_API_UPDATE=true` with bearer token auth
2. **Tailscale Funnel route** - Expose `/watchtower/` via existing gateway
3. **GitHub Actions step** - Call webhook after image push succeeds
4. **Secrets** - Store URL and token as GitHub repo secrets (not hardcoded)

### Security

- Bearer token required for all API requests
- URL stored as secret (hides tailnet domain)
- Tailscale Funnel provides HTTPS
- Daily polling remains as fallback

## Consequences

### Positive

- Instant deploys for app code changes (~seconds after image push)
- No infrastructure changes needed per-app (just add workflow step)
- Works with existing Watchtower (no new components)
- Maintains daily polling as fallback

### Negative

- Each app repo needs workflow update + secrets
- Watchtower updates ALL matching containers (not targeted)
- Requires Tailscale Funnel exposure of internal API

### Neutral

- GitOps pattern unchanged for infrastructure
- Two deployment patterns to understand (GitOps vs Watchtower webhook)

## Alternatives Considered

1. **Reduce poll interval** - Wastes resources checking constantly
2. **Self-hosted runner on Unraid** - Complex, overkill for this use case
3. **Webhook relay service** - Adds external dependency
4. **Extend GitOps to watch app repos** - Scope creep, mixing concerns

## References

- [Watchtower HTTP API Mode](https://containrrr.dev/watchtower/http-api-mode/)
- Obsidian: [[Watchtower Webhook Deploy]]
