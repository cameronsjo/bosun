# Change: Add authenticated HTTPS Git synchronization

## Why

Bosun can clone public HTTPS repositories and authenticate SSH repositories,
but private HTTPS repositories have no credential path. Operators using a
private Gitea or GitHub repository therefore receive a generic go-git
authentication failure even when HTTPS is their only reachable transport.

## What Changes

- Add explicit `BOSUN_GIT_USERNAME` and `BOSUN_GIT_TOKEN` credentials for Git
  HTTP Basic authentication over HTTPS.
- Preserve anonymous access when both variables are unset, while rejecting
  partial credential pairs, credential use over non-HTTPS transports, and
  credentials embedded in repository URLs before any network request.
- Apply the same authentication contract to initial clone and subsequent fetch
  operations used by both standalone reconcile and daemon reconcile paths.
- Reject cross-origin or HTTPS-to-HTTP redirects before credentials can be
  forwarded; permit only same-origin HTTPS redirects for authenticated Git
  traffic.
- Validate the same contract in standalone reconcile, daemon startup, and
  `bosun validate`, before any Git network request or daemon listener starts.
- Redact credentials and repository URL userinfo from logs, returned errors,
  validation diagnostics, health/status responses, and the daemon `/config`
  response.
- Keep the credential pair environment-only and process-scoped: no YAML fields,
  legacy aliases, deploy-state persistence, config reload, or status exposure.
- Document the environment variables, transport constraints, and migration
  away from URL-embedded credentials.

## Impact

- Affected specs: `reconcile`
- Affected code: `internal/reconcile/git.go`,
  `internal/reconcile/reconcile.go`, `internal/cmd/reconcile.go`,
  `internal/cmd/validate.go`, `internal/daemon/daemon.go`,
  `internal/daemon/pure.go`, and the socket/API response types that present
  repository URLs or reconciliation errors
- All consumers: `GitOps.Clone`, `GitOps.Pull`, standalone `bosun reconcile`,
  daemon `ConfigFromEnv`/`ValidateConfig` plus poll/webhook reconciliation,
  `bosun validate` (environment, reconcile-config, and dry-run paths),
  reconcile logs/errors, daemon `/config`, `/status`, `/api/status`, and
  `/health` responses, and installer/deployment templates that enumerate the
  process environment
- Documentation and deployment surfaces: `AGENTS.md`, `README.md`,
  `llms.txt`, `docs/gitops.md`, `docs/commands.md`,
  `skills/onboard/resources/configuration.md`,
  `skills/onboard/resources/gitops.md`, `internal/cmd/init.go`,
  `bosun/docker-compose.yml`, and `unraid-templates/`
