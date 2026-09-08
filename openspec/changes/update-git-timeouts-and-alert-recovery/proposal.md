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
  stalled dial, a stalled handshake, or a stalled packfile read blocks
  indefinitely. `Clone` is worse: it applies `GitCloneTimeout` **only when the
  caller context carries no deadline** (`git.go:362-367`), and the daemon sets a
  `ReconcileTimeout` deadline on every reconcile cycle (`daemon.go:1008`) — so on
  the daemon path the declared clone bound is never applied at all, while the
  error text names it regardless.

  What the incident establishes is narrower than where the time went. The
  measured facts are an error saying `2m0s` and a run logging
  `duration_ms: 989971` — a **run** duration that also covers lock acquisition,
  `validateBranch`, `IsDirty`, `GetLatestCommit` and `PlainOpen`
  (`git.go:433-472`). That proves the error text reports the declared bound
  rather than the measured one; it does not identify which layer stalled, and a
  fetch that expired correctly at 2m with the remaining 14m30s spent elsewhere
  fits the same two observations. Both layers are unbounded by code-reading;
  only the dial is bounded here, and neither is confirmed as the cause. The
  fetch-scoped elapsed time this change adds is what will settle it next time.

- **A failure alert cannot be retracted.** Three independent suppressors sit in
  front of `sendRecoveryAlert`: `alerts.go:174` returns early on
  `!r.config.OnSuccess`; `reconcile.go:944` only calls it when
  `state.AttemptCount > 1`, while failure alerts fire at attempt 1
  (`state.go:167`, `alertThresholds = []int{1, 3, 10, 30}`); and the deploy-path
  skip at `reconcile.go:637-655` — the branch this incident took, because the
  recovering commit changed only docs — zeroes `AttemptCount`,
  `LastAttemptedCommit` and `LastAlertedAttempt` and returns before reaching
  line 944 at all. A single failure followed by a docs-only recovery therefore
  can never produce a retraction, whatever the configuration says.

  The already-deployed branch at `reconcile.go:608-616` fails **differently**,
  and conflating the two produces a wrong fix. It zeroes only `AttemptCount` and
  `LastAttemptedCommit`, and conditionally; `LastAlertedAttempt` survives it. Its
  defect is the early return before dispatch, not the destruction of evidence —
  so a recovery dispatched there without also clearing `LastAlertedAttempt`
  would re-fire on every subsequent already-deployed run.

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
  timeouts SHALL bound every phase they can reach, and report honestly on the
  phases they cannot, rather than merely labelling an error. A new `GitSSHDialTimeout` SHALL bound TCP connection
  establishment. The handshake and packfile transfer **cannot** be bounded from
  bosun: doing so needs a custom `transport.Transport`, whose session layer
  exists only in go-git's `internal/` tree. That residual is stated in operator
  documentation rather than faked, and tracked as #655. An operation's own
  timeout SHALL apply even when the caller context already carries a deadline —
  the case in which `Clone`'s bound is silently never applied today. Timeout
  errors SHALL report the actual elapsed time and SHALL be logged at the throw
  site with operation, sanitized URL, branch, elapsed, and the effective bound.
  The bound SHALL NOT be met by detaching the work to a goroutine and returning.

- **Timeouts become fields, not compile-time constants** — `GitCloneTimeout`,
  `GitFetchTimeout`, and `GitSSHDialTimeout` SHALL be `GitOps` fields defaulting
  to the existing constants, so a test can shorten them and a caller can set
  them. No operator-facing config key or environment variable is added here.

- **Auth is preserved when the dial timeout is applied** — applying a dial
  timeout SHALL NOT discard the resolved SSH auth method's `User`, `Auth`,
  `HostKeyCallback`, or `HostKeyAlgorithms`. This is a requirement because the
  obvious implementation (`client.InstallProtocol` with a bare `Timeout`
  config) destroys all four and takes GitOps offline.

- **A failure alert that fired is always retractable** — a new `on_recovery`
  gate, defaulting **true**, SHALL control Deploy Recovery dispatch, replacing
  the current implicit dependence on `on_success`. The principle is that the
  retract gate must never be more restrictive than the alert gate. `on_recovery`
  SHALL be hot-reloadable on the same terms as the other two gates, not wired at
  startup only.

- **Recovery fires at the run boundary, not the deploy boundary** — when a
  reconcile run ends clean and a failure alert was previously sent for that
  target, a recovery alert SHALL be dispatched — including on runs that skip
  deployment because no deploy-relevant files changed, and on runs that skip
  because the commit is already deployed. The predicate for "a retraction is
  owed" SHALL be `LastAlertedAttempt > 0`. One failure that alerted SHALL earn
  one retraction, reported as **one** prior failure: the `AttemptCount > 1`
  condition is removed, and so is the `-1` that exists only because of it.

- **Webhook path corrections and follow-ups** — five documentation sites still
  print `/hooks/github-push`, an endpoint the daemon never registered, which is
  the likely origin of a nine-month-dead webhook. Corrected here. Three
  separately-filed follow-ups are linked rather than re-filed.

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
  - `internal/reconcile/git.go:77-83` (constants → `GitOps` fields),
    `:362-367` (clone's conditional timeout application), `:401-407` (clone
    timeout log, missing URL/branch/`timeout_ms`), `:475-488` (fetch, error
    text, no log line at all)
  - `internal/reconcile/reconcile.go:637-655` (deploy-path skip: zeroes all
    three counters, returns early), `:608-616` (already-deployed skip: returns
    early, zeroes two counters conditionally, leaves `LastAlertedAttempt`),
    `:944-945` (`AttemptCount > 1` condition and the `-1` argument),
    `:219-221` (doc comment naming `OnSuccess` as the recovery gate),
    `:35-36` (reload DTO), `:565` (per-run reload call site)
  - `internal/reconcile/alerts.go:167-174` (doc comment and `OnSuccess` early
    return)
  - `internal/reconcile/config_reload.go:175-183` (gate applier), `:234-235`
    (reload log)
  - `internal/config/config.go:162` (`AlertConfig`), `:458-459` (reload
    builder), `:1139` (`AlertConfigFromEnv`, exported),
    `:1188-1193` (`extractAlertConfig`)
  - `internal/reconcile/target.go:233` (`DefaultConfig()`)
  - `internal/daemon/daemon.go:2349-2350` (gate copy into reconciler config),
    `:1008` (the `ReconcileTimeout` deadline that disables clone's timeout)
  - `internal/daemon/server.go:155-187` (request-log middleware), `:587`, `:593`,
    `:608`, `:658`, `:664`, `:695` (hardcoded `remote_addr` strings to converge)
  - `internal/log/fields.go` (new `FieldRemoteAddr`)
  - `internal/cmd/alert.go:166-175` (gate reporting)
- Not in scope, filed separately: bounding the SSH handshake and packfile
  transfer, blocked on a go-git transport hook that does not exist (#655); the
  `on_failure`-logged-false-yet-alert-delivered contradiction (#652); `GitLocalTimeout` sharing the unenforced-label
  defect on local operations (#653); a timeout-bearing HTTP transport for the
  private-HTTPS git path (#654 — homelab authenticates over SSH). Operator-facing
  configuration of the git timeouts is also out of scope; the fields exist to be
  testable and caller-settable, and no `BOSUN_*` variable is added.
