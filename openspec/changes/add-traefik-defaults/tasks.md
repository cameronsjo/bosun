## 1. Traefik Static Config Template

- [x] 1.1 Update `examples/homelab/unraid/compose/core.yml.tmpl` Traefik service with: `defaultRule`, HTTP→HTTPS redirect entrypoint, `exposedByDefault=false`, `network=proxynet`, Let's Encrypt ACME resolver
- [x] 1.2 Add `traefik.yml.tmpl` static config template as alternative to CLI flags (file-based config for complex setups)
- [x] 1.3 Ensure domain is templated from secrets (`$secrets.network.domain`) for `defaultRule`

## 2. Traefik Dynamic Config Defaults

- [x] 2.1 Update `examples/homelab/unraid/appdata/traefik/conf.d/dynamic.yml.tmpl` — promote `secure-headers` to `secure-defaults` with full header set (HSTS, nosniff, frameDeny, XSS, referrer-policy)
- [x] 2.2 Add `default-compress` middleware to dynamic config (gzip/brotli, minResponseBodyBytes=1024)
- [x] 2.3 Keep `authelia` forwardAuth middleware as-is (already present)

## 3. Provision Updates

- [x] 3.1 Update `manifest/provisions/reverse-proxy.yml` — add `secure-defaults@file,default-compress@file` to router middlewares in both compose labels and traefik dynamic config
- [x] 3.2 Create `manifest/provisions/protected.yml` — chains `${auth_middleware}` (default: `authelia@file`) middleware on the router, includes `reverse-proxy`
- [x] 3.3 Verify `manifest/provisions/webapp.yml` inherits reverse-proxy middleware chain changes correctly (no code change needed, but verify rendering)
- [x] 3.4 Update golden test files in `internal/manifest/render_test.go` to reflect new middleware chain in reverse-proxy output
- [x] 3.5 Update `internal/manifest/provision_test.go` to verify middleware chain in loaded provisions

## 4. Default Domain Config

- [x] 4.1 Add `domain` field to `configFile` struct in `internal/config/config.go` (YAML tag: `domain`)
- [x] 4.2 Add `Domain()` getter to `Config` struct
- [x] 4.3 Add `extractDomain()` helper following existing pattern
- [x] 4.4 Wire domain into `Load()` alongside existing extract calls
- [x] 4.5 Add tests for domain config loading

## 5. Upgrade Command

- [x] 5.1 Create `internal/cmd/upgrade.go` with `upgrade` parent command and `upgrade traefik` subcommand
- [x] 5.2 Implement Traefik static config detection (parse compose file for traefik service, extract command flags or config file path)
- [x] 5.3 Implement Traefik dynamic config detection (locate conf.d directory from volumes)
- [x] 5.4 Implement check functions: `checkHTTPSRedirect`, `checkExposedByDefault`, `checkDefaultRule`, `checkSecurityHeaders`, `checkCompression`, `checkACMEResolver`
- [x] 5.5 Implement interactive diff display showing proposed additions with color-coded output
- [x] 5.6 Implement `--dry-run` flag (display only, no writes)
- [x] 5.7 Implement `--yes` flag (apply all without prompting)
- [x] 5.8 Add `upgrade` command to `rootCmd` in `init()`

## 6. Doctor Diagnostics

- [x] 6.1 Add Traefik config validation section to `internal/cmd/diagnostics.go`
- [x] 6.2 Implement checks: HTTPS redirect, security headers, exposedByDefault, ACME resolver, defaultRule, socket proxy
- [x] 6.3 Display checks as pass/warn/info in doctor output using existing ui helpers
- [x] 6.4 Add tests for diagnostic checks

## 7. Init Wizard Updates

- [x] 7.1 Add domain prompt to `bosun init` wizard
- [x] 7.2 Generate Traefik static config with all defaults using provided domain
- [x] 7.3 Generate Traefik dynamic config with secure-defaults and compress middleware
- [x] 7.4 Store domain in generated `bosun.yaml`

## 8. Documentation & Skills

- [x] 8.1 Update `docs/commands.md` with `upgrade traefik` command
- [x] 8.2 Update `skills/onboard/resources/commands.md` with upgrade command
- [x] 8.3 Update `skills/onboard/resources/configuration.md` with `domain` field
- [x] 8.4 Document the Traefik defaults baseline in `docs/` (what's included and why)

## 9. Testing

- [x] 9.1 Add unit tests for upgrade check functions
- [x] 9.2 Add unit tests for domain config loading
- [x] 9.3 Update manifest golden tests for new reverse-proxy middleware chain
- [x] 9.4 Add test for protected provision rendering
- [x] 9.5 `make test` passes
- [x] 9.6 `make build` succeeds
