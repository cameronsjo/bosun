## Context

Bosun generates Traefik configuration through two mechanisms: Docker Compose labels (via provisions) and Traefik dynamic config files (via the `traefik` output target). The current baseline is minimal — it routes traffic but provides no security hardening, HTTPS enforcement, or compression out of the box. Existing users have hand-tuned their configs over time. New users get an insecure default.

This change needs to thread the needle: improve defaults for new users without breaking existing deployments, and give existing users a guided path to adopt improvements.

## Goals / Non-Goals

**Goals:**
- New projects get a production-quality Traefik config out of the box (HTTPS, security headers, compression, auto-routing)
- Existing users can adopt improvements incrementally via `bosun upgrade traefik`
- The `reverse-proxy` provision chains security middleware by default
- `bosun doctor` validates Traefik config against the recommended baseline
- All defaults are overridable — "batteries included, swappable"

**Non-Goals:**
- Not replacing Traefik with another reverse proxy (Caddy, Nginx)
- Not managing Traefik's own Docker container lifecycle (that's the user's compose file)
- Not implementing Traefik's plugin system integration
- Not adding circuit breaker or rate limiting to the default middleware chain (available as opt-in provisions later)
- Not adding docker-socket-proxy as a default (info-level suggestion only)

## Decisions

### Decision: Additive upgrade model, not migration

The `bosun upgrade traefik` command adds missing configuration blocks. It never removes or replaces existing config. This means:
- Users who already have custom security headers keep them
- Users who use a different cert resolver keep it
- The upgrade is safe to run multiple times (idempotent)

**Alternatives considered:**
- Full config regeneration: Would overwrite user customizations. Rejected.
- Config merge (deep merge user + defaults): Complex conflict resolution, hard to reason about. Rejected.
- Side-by-side diff + manual apply: Too much friction for the typical user. Rejected (but `--dry-run` provides this for power users).

### Decision: Middleware referenced by name, not inlined

The `reverse-proxy` provision references middleware by name (`secure-defaults@file`) rather than inlining the header definitions in labels. This means:
- One place to change security headers (the dynamic config file)
- All services automatically pick up changes
- Users can replace the middleware definition without touching every service

**Alternatives considered:**
- Inline labels per service: Repetitive, hard to update globally. Rejected.
- Middleware defined in compose labels: Would require updating every service's labels to change defaults. Rejected.

### Decision: defaultRule uses container name + project domain

The `defaultRule` template is: `Host({{ trimPrefix "/" .Name }}.DOMAIN)` where DOMAIN comes from `bosun.yaml` or secrets. This means:
- A container named `grafana` automatically gets `grafana.example.com`
- Services can still override with explicit `traefik.http.routers.X.rule` labels
- The `reverse-proxy` provision's explicit `Host()` rule takes precedence over `defaultRule`

**Alternatives considered:**
- Use Docker Compose labels for domain: Would require a new label standard. Rejected — `defaultRule` is native Traefik.
- Derive subdomain from service name in manifest: Already happens when `subdomain` config is set. `defaultRule` is the fallback for services without the reverse-proxy provision.

### Decision: Domain stored in bosun.yaml, not just secrets

The `domain` field in `bosun.yaml` is plaintext (not a secret). It's used for:
- `defaultRule` generation in Traefik static config
- Fallback for provisions when `domain` is not in service config
- `bosun doctor` validation

Secrets can still override via Go template in the static config template (`{{ $secrets.network.domain }}`), but having it in `bosun.yaml` enables non-secret workflows and simplifies the upgrade command.

## Risks / Trade-offs

### Risk: Existing reverse-proxy provision users get unexpected middleware
**Mitigation:** The `secure-defaults@file` middleware must exist in the Traefik dynamic config for the reference to work. If a user upgrades the provision but not the dynamic config, Traefik will log an error but still route traffic (middleware references to undefined middleware are non-fatal in Traefik). `bosun doctor` will catch and warn about this mismatch.

### Risk: defaultRule conflicts with explicit per-service rules
**Mitigation:** Traefik's explicit `traefik.http.routers.X.rule` labels always take precedence over `defaultRule`. Services with the `reverse-proxy` provision already set explicit rules and are unaffected.

### Risk: HTTPS redirect breaks services that need plain HTTP
**Mitigation:** The redirect is on the `web` entrypoint only. Services can still expose custom entrypoints. The upgrade command warns about this and allows skipping the redirect.

### Risk: Upgrade command modifies templated files
**Mitigation:** The upgrade command operates on the example/generated files. For Go-templated files (`.tmpl`), it detects template syntax and warns the user that manual review is needed rather than auto-applying changes.

## Migration Plan

### New users (bosun init)
1. `bosun init` prompts for domain
2. Generates Traefik static config with all defaults
3. Generates dynamic config with `secure-defaults` and `default-compress` middleware
4. No migration needed — they start with the good baseline

### Existing users (bosun upgrade traefik)
1. Run `bosun upgrade traefik --dry-run` to see what's recommended
2. Run `bosun upgrade traefik` to interactively apply changes
3. Run `bosun doctor` to verify the configuration
4. Commit and push — bosun daemon deploys the updated config

### Provision users (reverse-proxy update)
1. Update bosun binary (new provisions bundled)
2. Re-run `bosun provision` to regenerate compose/traefik configs
3. Verify `secure-defaults@file` middleware exists in dynamic config (doctor warns if not)
4. Commit and push

**Rollback:** All changes are git-tracked. `git revert` the config changes, push, daemon deploys the previous config. Bosun's built-in backup system also snapshots the pre-deploy config.

## Open Questions

- Should `default-ratelimit` be included in the standard middleware chain, or kept as a separate opt-in provision? (Leaning opt-in — rate limits are context-dependent)
- Should `bosun upgrade` be a top-level command that supports future upgrade targets (e.g., `bosun upgrade provisions`, `bosun upgrade config`), or should it be Traefik-specific? (Leaning top-level with subcommands for extensibility)
- Should the `domain` field in `bosun.yaml` be required or optional? (Leaning optional — existing configs without it should continue to work, provisions still require explicit `domain` in service config if not set globally)
