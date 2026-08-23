## 1. HTTPS Git authentication

- [ ] 1.1 Add a single Git authentication resolver that validates
  `BOSUN_GIT_USERNAME`/`BOSUN_GIT_TOKEN`, rejects URL userinfo, and preserves
  existing anonymous HTTPS plus SSH agent/key behavior. Credentials remain
  operation-scoped and are never stored in config or state.
- [ ] 1.2 Apply the resolved Basic authentication to both `GitOps.Clone` and
  `GitOps.Pull` without transmitting it over plaintext HTTP or SSH.
- [ ] 1.3 Enforce an authenticated redirect policy: allow only same-origin HTTPS
  hops and reject downgrade or cross-origin redirects without forwarding the
  Authorization header.
- [ ] 1.4 Sanitize repository URLs and transport errors before every reconcile
  log/trace, validation diagnostic, returned error, daemon health/status field,
  and `/config` response.
- [ ] 1.5 Validate the shared contract in standalone `bosun reconcile`, daemon
  `ValidateConfig` before servers/loops start, and every `bosun validate` path.
- [ ] 1.6 Keep credentials environment-only with no YAML fields, legacy aliases,
  state serialization, config hot reload, metrics, trace attributes, or response
  fields; retain existing `BOSUN_REPO_URL` precedence for the effective URL.

## 2. Tests

- [ ] 2.1 Add in-process HTTPS Git server tests (using a test-only trusted
  certificate/client) proving authenticated clone and authenticated fetch send
  the expected Basic credentials.
- [ ] 2.2 Test anonymous public HTTPS remains unchanged and authentication
  rejection returns an actionable error.
- [ ] 2.3 Test missing username/token pairs, credentials on non-HTTPS URLs, and
  malformed or hostless HTTPS URLs, plus username-only/password/percent-encoded
  URL userinfo fail before network I/O; prove SCP-like SSH syntax is not
  misclassified as userinfo.
- [ ] 2.4 Test authenticated HTTPS redirects: same-origin HTTPS is allowed,
  while downgrade and cross-origin destinations receive no Authorization header
  and synchronization fails safely.
- [ ] 2.5 Test standalone reconcile, daemon startup/reconcile, and all
  `bosun validate` consumers use the same resolution; test credentials use only
  `BOSUN_` names and apply to the effective URL selected by existing repo-URL
  precedence.
- [ ] 2.6 Inject recognizable username/token/userinfo values plus their escaped
  and Basic-header encodings and prove they are absent from clone/fetch errors,
  reconcile logs/traces, validation output, `/config`, `/status`, `/api/status`,
  and `/health` responses.
- [ ] 2.7 Test credentials are absent from config/state serialization and project
  config reload cannot define or rotate them; document/test restart-based
  rotation semantics.
- [ ] 2.8 Run focused tests repeatedly and under the race detector.

## 3. Documentation

- [ ] 3.1 Document `BOSUN_GIT_USERNAME` and `BOSUN_GIT_TOKEN`, the required pair,
  HTTPS-only and redirect restrictions, anonymous behavior, restart-based
  rotation, and URL-userinfo migration in `AGENTS.md`, `README.md`, `llms.txt`,
  `docs/gitops.md`, and `docs/commands.md`.
- [ ] 3.2 Update `skills/onboard/resources/configuration.md` and
  `skills/onboard/resources/gitops.md` with the same contract.
- [ ] 3.3 Update generated/setup deployment surfaces (`internal/cmd/init.go`,
  `bosun/docker-compose.yml`, and `unraid-templates/`) so operators can supply
  the pair without embedding credentials in the repository URL and secret/token
  fields are rendered as sensitive inputs where supported.

## 4. Verification

- [ ] 4.1 Run `go test -p=1 ./... -count=1`.
- [ ] 4.2 Run relevant race tests, `go vet ./...`, `go build ./...`, and
  `golangci-lint run --timeout=5m --new-from-rev=origin/main ./...`.
- [ ] 4.3 Run `openspec validate add-https-git-auth --strict` and full strict
  OpenSpec validation.
