# Change: Enforce git network timeouts and make failure alerts retractable

## Why

A `Deployment Failed [unraid]` alert on 2026-09-08 reported `git fetch timed out
after 2m0s` with a duration of `16m30s`. The deploy was never in danger — the
next cycle pulled clean five seconds later — but the alert exposed two
specified-behaviour gaps that leave the operator misinformed and unable to tell
a transient stall from a real outage.

- **A declared git timeout is a label on an error message, not a ceiling on the
  operation.** `internal/reconcile/git.go:475` wraps the fetch in
  `context.WithTimeout(ctx, GitFetchTimeout)`, but go-git severs that context at
  the SSH transport: `plumbing/transport/ssh/common.go` builds its dial context
  from `context.Background()`, and bosun never sets `ssh.ClientConfig.Timeout`.
  `FetchContext` consults the context only *between* protocol steps, so a
  stalled dial or a stalled packfile read blocks indefinitely. `Clone`
  (`git.go:366`, `:406`) has the identical defect. The stalled run held the
  reconcile lock for the full 16m30s, queueing three triggers behind it. The
  error text then reported the *declared* bound rather than the elapsed time —
  eight times off, and the single most misleading fact in the incident.

- **A failure alert cannot be retracted.** Three independent suppressors sit in
  front of `sendRecoveryAlert`: `alerts.go:174` returns early on
  `!r.config.OnSuccess`; `reconcile.go:944` only calls it when
  `state.AttemptCount > 1`, while failure alerts fire at attempt 1
  (`state.go:167`, `alertThresholds = []int{1, 3, 10, 30}`); and the deploy-path
  skip at `reconcile.go:637-655` — the branch this incident took, because the
  recovering commit changed only docs — resets `AttemptCount` and
  `LastAlertedAttempt` to 0 and returns before reaching line 944 at all. The
  already-deployed branch at `:608` does the same. A single failure followed by
  a docs-only recovery therefore can never produce a retraction, whatever the
  configuration says.

Neither behaviour has an authoritative requirement to regress against. The
`reconcile` spec's Git Repository Sync requirement describes clone/pull
semantics and auth resolution but says nothing about network timeouts — the only
git-adjacent timeout in the spec is `BackupTimeout`, which governs a different
operation. The `alerting` spec specifies the Deploy Recovery alert's *shape*
(`alerting/spec.md:232`) and the `on_success`/`on_failure` gates
(`:209-218`), but never states *when* a recovery alert fires or which gate
controls it.

## What Changes

- **Enforced git network timeouts** — the reconciler's declared git network
  timeouts SHALL bound the wall-clock duration of the operation, not merely
  label its error. A new `GitSSHDialTimeout` SHALL bound connection
  establishment and the SSH handshake. Timeout errors SHALL report the actual
  elapsed time and SHALL be logged at the throw site with operation, sanitized
  URL, branch, elapsed, and configured bound.

- **Timeouts are configurable, not compile-time constants** — `GitCloneTimeout`,
  `GitFetchTimeout`, and `GitSSHDialTimeout` SHALL be `GitOps` fields defaulting
  to the existing constants, so an operator can tune them and a test can shorten
  them.

- **Auth is preserved when the dial timeout is applied** — applying a dial
  timeout SHALL NOT discard the resolved SSH auth method's `User`, `Auth`,
  `HostKeyCallback`, or `HostKeyAlgorithms`. This is a requirement because the
  obvious implementation (`client.InstallProtocol` with a bare `Timeout`
  config) destroys all four and takes GitOps offline.

- **A failure alert that fired is always retractable** — a new `on_recovery`
  gate, defaulting **true**, SHALL control Deploy Recovery dispatch, replacing
  the current implicit dependence on `on_success`. A system that alerts on
  failure and cannot retract is worse than one that never alerted.

- **Recovery fires at the run boundary, not the deploy boundary** — when a
  reconcile run ends clean and a failure alert was previously sent for that
  target, a recovery alert SHALL be dispatched — including on runs that skip
  deployment because no deploy-relevant files changed, and on runs that skip
  because the commit is already deployed. One failure that alerted SHALL earn
  one retraction; the current `AttemptCount > 1` condition is removed.

- **The webhook request log names its sender** — the HTTP request log SHALL
  carry the observed peer address, and separately the forwarded client address
  when and only when the peer is a trusted proxy. The two SHALL NOT be collapsed
  into one field: the listener binds all interfaces by design, so any container
  on the bridge can send a well-formed `X-Forwarded-For`, and preferring the
  header would make the next investigation confidently wrong.

## Impact

- Affected specs:
  - `reconcile` — ADDED: Git Network Timeout Enforcement, Git Timeout
    Configuration. (Builds on the existing Git Repository Sync requirement,
    which is unchanged.)
  - `alerting` — MODIFIED: Alert Configuration (adds `on_recovery`, default
    true). ADDED: Deploy Recovery Dispatch. (The Deploy Recovery alert's
    formatting under Reconciliation Lifecycle Alerts is unchanged.)
  - `observability` — ADDED: HTTP Request Client Attribution.
- Affected code:
  - `internal/reconcile/git.go:77-83` (constants → `GitOps` fields), `:366`,
    `:406` (clone), `:475-488` (fetch, error text, missing log line)
  - `internal/reconcile/reconcile.go:608`, `:637-655` (skip branches that reset
    counters and return early), `:944` (`AttemptCount > 1` condition),
    `:219-221` (doc comment naming `OnSuccess` as the recovery gate)
  - `internal/reconcile/alerts.go:174` (`OnSuccess` early return)
  - `internal/config/config.go:162` (`AlertConfig.OnSuccess`), `:1157-1158`
    (`alertConfigFromEnv`), `:1188-1193` (`extractAlertConfig`)
  - `internal/reconcile/target.go:233` (`DefaultConfig()`)
  - `internal/daemon/daemon.go:2349-2350` (gate copy into reconciler config)
  - `internal/daemon/server.go:155-187` (request-log middleware)
  - `internal/cmd/alert.go:166-171` (gate reporting)
- Not in scope, filed separately: `GitLocalTimeout` shares the unenforced-label
  defect on local operations; a timeout-bearing HTTP transport for the
  private-HTTPS git path (homelab authenticates over SSH).
