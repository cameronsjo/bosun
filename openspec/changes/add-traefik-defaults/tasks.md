## 1. Traefik Static Config Template

- [ ] 1.1 Update `examples/homelab/unraid/compose/core.yml.tmpl` Traefik service with: `defaultRule`, HTTP→HTTPS redirect entrypoint, `exposedByDefault=false`, `network=proxynet`, Let's Encrypt ACME resolver
- [ ] 1.2 Add `traefik.yml.tmpl` static config template as alternative to CLI flags (file-based config for complex setups)
- [ ] 1.3 Ensure domain is templated from secrets (`$secrets.network.domain`) for `defaultRule`

## 2. Traefik Dynamic Config Defaults

- [ ] 2.1 Update `examples/homelab/unraid/appdata/traefik/conf.d/dynamic.yml.tmpl` — promote `secure-headers` to `secure-defaults` with full header set (HSTS, nosniff, frameDeny, XSS, referrer-policy)
- [ ] 2.2 Add `default-compress` middleware to dynamic config (gzip/brotli, minResponseBodyBytes=1024)
- [ ] 2.3 Add `default-ratelimit` middleware to dynamic config (average=100/1m, burst=50)
- [ ] 2.4 Keep `authelia` forwardAuth middleware as-is (already present)

## 3. Provision Updates

- [ ] 3.1 Update `manifest/provisions/reverse-proxy.yml` — add `secure-defaults@file,default-compress@file` to router middlewares in both compose labels and traefik dynamic config
- [ ] 3.2 Create `manifest/provisions/protected.yml` — chains `${auth_middleware}` (default: `authelia@file`) middleware on the router, includes `reverse-proxy`
- [ ] 3.3 Update `manifest/provisions/webapp.yml` — no change needed (inherits reverse-proxy updates)
- [ ] 3.4 Update golden test files to reflect new middleware chain in reverse-proxy output

## 4. Default Domain Config

- [ ] 4.1 Add `domain` field to `configFile` struct in `internal/config/config.go` (YAML tag: `domain`)
- [ ] 4.2 Add `Domain()` getter to `Config` struct
- [ ] 4.3 Add `extractDomain()` helper following existing pattern
- [ ] 4.4 Wire domain into `Load()` alongside existing extract calls
- [ ] 4.5 Add tests for domain config loading

## 5. Upgrade Command

- [ ] 5.1 Create `internal/cmd/upgrade.go` with `upgrade` parent command and `upgrade traefik` subcommand
- [ ] 5.2 Implement Traefik static config detection (parse compose file for traefik service, extract command flags or config file path)
- [ ] 5.3 Implement Traefik dynamic config detection (locate conf.d directory from volumes)
- [ ] 5.4 Implement check functions: `checkHTTPSRedirect`, `checkExposedByDefault`, `checkDefaultRule`, `checkSecurityHeaders`, `checkCompression`, `checkACMEResolver`
- [ ] 5.5 Implement interactive diff display showing proposed additions with color-coded output
- [ ] 5.6 Implement `--dry-run` flag (display only, no writes)
- [ ] 5.7 Implement `--yes` flag (apply all without prompting)
- [ ] 5.8 Add `upgrade` command to `rootCmd` in `init()`

## 6. Doctor Diagnostics

- [ ] 6.1 Add Traefik config validation section to `internal/cmd/diagnostics.go`
- [ ] 6.2 Implement checks: HTTPS redirect, security headers, exposedByDefault, ACME resolver, defaultRule, socket proxy
- [ ] 6.3 Display checks as pass/warn/info in doctor output using existing ui helpers
- [ ] 6.4 Add tests for diagnostic checks

## 7. Init Wizard Updates

- [ ] 7.1 Add domain prompt to `bosun init` wizard
- [ ] 7.2 Generate Traefik static config with all defaults using provided domain
- [ ] 7.3 Generate Traefik dynamic config with secure-defaults and compress middleware
- [ ] 7.4 Store domain in generated `bosun.yaml`

## 8. Documentation & Skills

- [ ] 8.1 Update `docs/commands.md` with `upgrade traefik` command
- [ ] 8.2 Update `skills/onboard/resources/commands.md` with upgrade command
- [ ] 8.3 Update `skills/onboard/resources/configuration.md` with `domain` field
- [ ] 8.4 Document the Traefik defaults baseline in `docs/` (what's included and why)

## 9. Testing

- [ ] 9.1 Add unit tests for upgrade check functions
- [ ] 9.2 Add unit tests for domain config loading
- [ ] 9.3 Update manifest golden tests for new reverse-proxy middleware chain
- [ ] 9.4 Add test for protected provision rendering
- [ ] 9.5 `make test` passes
- [ ] 9.6 `make build` succeeds
