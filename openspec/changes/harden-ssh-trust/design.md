## Context

Bosun has two SSH consumers with different libraries: go-git performs Git
transport handshakes in-process, while remote deployment invokes OpenSSH for
secret-bearing tar, SCP, backup, compose, and signal operations. Both resolve
the same operator-controlled candidates: `BOSUN_SSH_KNOWN_HOSTS`, followed by
`/config/known_hosts`. The existing Git callback silently falls back to
`InsecureIgnoreHostKey`, and the deploy path silently falls back to OpenSSH
TOFU (`accept-new`).

## Goals / Non-Goals

- Goals: preserve the candidate order; fail closed on absent or malformed Git
  trust files; require strict verification for deploy when no configured file
  resolves; preserve the explicit opt-out; keep agent and key authentication
  usable when trust is configured; make daemon startup report the same failure.
- Non-goals: change HTTPS credentials, redirect policy, SSH authentication
  precedence, target-host validation, remote filesystem mutation, or the
  default trust stores used by OpenSSH when strict mode has no configured-file
  override.

## Decisions

- Git `getHostKeyCallback` returns `(xssh.HostKeyCallback, error)`. An explicit
  case-insensitive `BOSUN_SSH_INSECURE_HOST_KEY=true` returns
  `xssh.InsecureIgnoreHostKey`; otherwise the first existing candidate must
  parse successfully, and no candidate or a parse error returns an actionable
  error naming `known_hosts` and the opt-out.
- A malformed first candidate is fatal rather than skipped. This prevents a
  broken configured pin from silently selecting a different trust source.
- Deploy `hostKeyOptions` continues to use
  `StrictHostKeyChecking=yes` plus `UserKnownHostsFile=<path>` for the first
  usable configured candidate. With no candidate it emits only
  `StrictHostKeyChecking=yes`, allowing OpenSSH's normal configured trust
  stores to verify already-pinned hosts while refusing unknown hosts.
- SSH agent/key resolution obtains the host-key callback before returning an
  authentication method and closes an agent connection if trust-policy
  resolution fails. This makes startup validation and later Git operations
  share one fail-closed contract.
- All remote SSH/SCP legs retain their existing command seam, so one policy
  helper covers deployment, remote backups, compose, and signal operations.

## Risks / Trade-offs

- Existing deployments that relied on first-contact TOFU or implicit Git
  acceptance will stop before network access until an operator supplies a
  valid configured trust file or explicitly opts out.
- Deploy can still use an already-pinned host in OpenSSH's normal trust stores
  when no Bosun-specific file exists; Git intentionally does not consult the
  user profile because container profiles are ephemeral and can disagree with
  the configured deployment trust.
- The explicit opt-out remains intentionally dangerous and is documented as a
  bootstrap-only escape hatch.

## Migration Plan

1. Provision a parseable `known_hosts` file and set
   `BOSUN_SSH_KNOWN_HOSTS`, or mount `/config/known_hosts` before starting the
   daemon or running reconcile.
2. For one-time bootstrap only, set
   `BOSUN_SSH_INSECURE_HOST_KEY=true`, then remove it and rerun with strict
   trust once the host key is provisioned.

## Open Questions

- None for this split unit. HTTPS authentication and redirect behavior remain
  governed by the current main implementation.
