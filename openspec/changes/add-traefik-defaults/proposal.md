# Change: Add batteries-included Traefik defaults with upgrade path

## Why

Bosun's Traefik integration currently requires explicit per-service configuration for basic features that should be zero-config defaults. Users must manually configure HTTP-to-HTTPS redirects, security headers, compression, and per-service routing rules — all of which have well-established best practices. New users get a bare Traefik that routes traffic but does nothing to secure or optimize it. Existing users have no guided path to adopt improvements.

## What Changes

- Add a `traefik-defaults` provision/template that generates a complete Traefik static config with sensible defaults: `defaultRule`, HTTP→HTTPS redirect, `exposedByDefault=false`
- Add a `secure-defaults` middleware chain (security headers + compression) applied automatically by the `reverse-proxy` provision
- Add a `protected` provision that chains ForwardAuth (Authelia/Authentik) middleware for services requiring authentication
- Add `bosun upgrade traefik` command that detects the current Traefik config, shows a diff of recommended changes, and applies them interactively
- Update `bosun doctor` to warn when Traefik is missing recommended defaults (no HTTPS redirect, no security headers, `exposedByDefault=true`, direct socket mount)
- Update `bosun init` to generate the improved Traefik baseline for new projects

## Impact

- Affected specs: `manifest-system` (new provisions, modified reverse-proxy), `reconcile` (doctor diagnostics)
- Affected code:
  - `manifest/provisions/reverse-proxy.yml` — chain `secure-defaults` middleware
  - `manifest/provisions/protected.yml` — new provision
  - `examples/homelab/unraid/compose/core.yml.tmpl` — updated Traefik static config
  - `examples/homelab/unraid/appdata/traefik/conf.d/dynamic.yml.tmpl` — updated middleware defaults
  - `internal/cmd/upgrade.go` — new `upgrade traefik` command
  - `internal/cmd/diagnostics.go` — new Traefik config checks
  - `internal/cmd/init.go` — generate improved Traefik baseline
  - `internal/preflight/` — Traefik config validation checks
- All consumers:
  - `internal/manifest/render.go` — renders provisions (no change needed, provision format unchanged)
  - `internal/reconcile/reconcile.go` — deploys Traefik configs (no change needed)
  - `internal/config/config.go` — may need new config fields for default domain
  - `docs/commands.md` — new `upgrade` command docs
  - `skills/onboard/resources/commands.md` — skill docs update
  - `skills/onboard/resources/configuration.md` — new config fields
