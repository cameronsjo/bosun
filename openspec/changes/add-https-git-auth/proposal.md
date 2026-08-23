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
- Redact credentials and repository URL userinfo from logs, returned errors,
  status output, and validation diagnostics.
- Document the environment variables, transport constraints, and migration
  away from URL-embedded credentials.

## Impact

- Affected specs: `reconcile`
- Affected code: `internal/reconcile/git.go`,
  `internal/reconcile/reconcile.go`, `internal/cmd/validate.go`, and daemon
  status/config surfaces that expose the repository URL
- All consumers: `GitOps.Clone`, `GitOps.Pull`, standalone `bosun reconcile`,
  the daemon reconciliation loop, `bosun validate`, and daemon status responses
- Documentation: `AGENTS.md`, `README.md`, `docs/gitops.md`, and
  `skills/onboard/resources/configuration.md` plus
  `skills/onboard/resources/gitops.md`
