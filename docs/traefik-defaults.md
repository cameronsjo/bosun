# Traefik Security Defaults

Bosun ships a batteries-included Traefik configuration baseline. These defaults follow Traefik's own security best practices and are designed to be safe for production homelab use.

## What's Included

### Static Config (Command Flags)

| Setting | Flag | Why |
|---------|------|-----|
| HTTPS Redirect | `--entrypoints.web.http.redirections.entrypoint.to=websecure` | Force all HTTP traffic to HTTPS |
| Exposed By Default | `--providers.docker.exposedbydefault=false` | Containers must opt-in to Traefik routing via `traefik.enable=true` |
| Default Rule | `--providers.docker.defaultRule=Host(...)` | Consistent subdomain routing without per-service labels |
| ACME Resolver | `--certificatesresolvers.letsencrypt.acme.*` | Automatic TLS certificates from Let's Encrypt |

### Dynamic Config (Middleware)

| Middleware | Name | What It Does |
|------------|------|--------------|
| Security Headers | `secure-defaults` | HSTS (1 year, preload), Content-Type nosniff, Frame deny, XSS filter, Referrer-Policy, Permissions-Policy |
| Compression | `default-compress` | Gzip/Brotli compression for responses over 1024 bytes |

### Middleware Chain

The `reverse-proxy` provision applies both middlewares automatically:

```yaml
traefik.http.routers.myservice.middlewares: secure-defaults@file,default-compress@file
```

Services using the `protected` provision get an additional auth middleware:

```yaml
traefik.http.routers.myservice.middlewares: authelia@file,secure-defaults@file,default-compress@file
```

## Checking Your Configuration

### Doctor Checks

`bosun doctor` automatically checks for Traefik configuration issues when a Traefik service is detected:

```
  * Traefik: HTTPS redirect configured
  * Traefik: exposedByDefault set to false
  ! Traefik: No secure-defaults middleware found
      Run: bosun upgrade traefik
  * Traefik: Docker socket not directly mounted
```

### Upgrade Command

`bosun upgrade traefik` runs all 6 checks and can interactively apply fixes:

```bash
bosun upgrade traefik              # Show recommendations
bosun upgrade traefik --yes        # Apply all fixes
bosun upgrade traefik --dry-run    # Display-only mode
```

The upgrade is **additive only** -- it adds missing config, never removes or replaces existing settings. Safe to run multiple times.

## Security Headers Detail

The `secure-defaults` middleware sets these response headers:

| Header | Value | Purpose |
|--------|-------|---------|
| `Strict-Transport-Security` | `max-age=31536000; includeSubDomains; preload` | Force HTTPS for 1 year, including subdomains |
| `X-Content-Type-Options` | `nosniff` | Prevent MIME-type sniffing |
| `X-Frame-Options` | `DENY` | Prevent clickjacking via iframes |
| `X-XSS-Protection` | `1; mode=block` | Legacy XSS filter (still useful for older browsers) |
| `Referrer-Policy` | `strict-origin-when-cross-origin` | Limit referrer leakage to cross-origin requests |
| `Permissions-Policy` | `camera=(), microphone=(), geolocation=()` | Disable sensitive browser APIs |

## Docker Socket Security

`bosun doctor` warns when Traefik mounts the Docker socket directly:

```yaml
# Flagged as a warning
volumes:
  - /var/run/docker.sock:/var/run/docker.sock:ro
```

Direct Docker socket access gives Traefik (and any attacker who compromises it) full control over the Docker daemon. The recommended approach is [docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy), which exposes only the API endpoints Traefik needs.

## Domain Configuration

Set a base domain in `bosun.yaml` to use with Traefik's `defaultRule`:

```yaml
# bosun.yaml
domain: example.com
```

This enables automatic subdomain routing: a container named `myapp` becomes accessible at `myapp.example.com` without per-service routing labels.

## Template Considerations

If your compose file is a Go template (`.tmpl` extension or contains `{{ }}`), `bosun upgrade traefik` will display recommendations but **will not auto-apply** fixes. This prevents breaking template syntax. Review and apply the suggested changes manually.
