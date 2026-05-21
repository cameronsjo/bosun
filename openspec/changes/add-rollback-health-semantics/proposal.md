# Change: Restore-not-rerun rollback and bounded post-deploy health polling

## Why

The April 2026 reconcile-path bug hunt (Cluster E) found that the two safety
mechanisms guarding bad deploys are fictional in their failure modes.

When the critical-container health gate fails, the reconciler calls
`ComposeUpMultipleWithRollback(ctx, r.lastComposeFiles, r.lastBackupPath)`. But
`r.lastComposeFiles` is the **new, just-deployed, now-failing** file set, and
that helper first runs `docker compose up` against those same files. Because the
containers already exist and are merely unhealthy, compose exits 0, the helper
returns nil without ever touching the backup, and the reconciler logs "rollback
attempted" while Docker keeps running the broken release (#229, P0). Rollback is
silently a no-op — the documented "don't ship broken services" guarantee does
not hold.

Separately, post-deploy health polling (`pollContainerHealth`) places its
deadline check inside the success branch of its state-collection call. When the
Docker API errors for the whole health window (daemon restart, socket
permission, network blip), the deadline is never consulted and the select has no
deadline case, so the function polls forever — wedging health verification, the
state save, and the circuit breaker (#230, P0).

Neither behavior is pinned by an authoritative requirement, so there is nothing
to regress against. This proposal adds reconcile requirements that fix the
semantics: rollback restores the prior good state rather than re-applying the
broken render, health polling terminates on Docker API errors and at the
deadline, and a failed rollback is surfaced distinctly rather than swallowed.

## What Changes

- **Rollback restores prior good state** — when the post-deploy health gate
  fails, rollback SHALL re-deploy the backed-up prior compose files (a distinct
  "restore from backup" path), NOT re-run `docker compose up` against the new
  rendered output. Rollback ≠ "deploy again."
- **Bounded health polling** — post-deploy health verification polling SHALL
  evaluate its deadline every iteration regardless of Docker API success, SHALL
  carry a deadline/timeout case in its wait loop, and SHALL terminate with a
  failed result on persistent Docker API errors. It MUST distinguish "container
  unhealthy" (keep polling within the timeout) from "cannot query health"
  (bounded — fail at the deadline).
- **Rollback failure is surfaced distinctly** — a failed rollback after a failed
  health gate SHALL be reported as a critical state distinct from a successful
  rollback (preserving the `ErrRollbackSucceeded` / `ErrRollbackFailed` sentinel
  contract), and SHALL NOT be swallowed or logged as a generic success.

## Impact

- Affected specs: `reconcile` (ADDED requirements only; no existing requirement
  changes semantics, so `Service Orchestration`, `Critical Container Health
  Gate`, and `Post-Deploy Verification` are left intact).
- Affected code:
  - `internal/reconcile/reconcile.go:872-877` — health-gate failure path that
    calls `ComposeUpMultipleWithRollback(ctx, r.lastComposeFiles, ...)`
  - `internal/reconcile/compose.go:172-192` — `ComposeUpMultipleWithRollback`
    (deploy-or-rollback) reused incorrectly for the already-deployed-unhealthy
    case; line 180 `upFn(ctx, composeFiles)` re-runs the new files
  - `internal/reconcile/compose.go:~325` — `ComposeUpIsolated` rollback path,
    the existing "explicitly redeploy backup" pattern the gate path should reuse
  - `internal/reconcile/health.go:41-92` — `pollContainerHealth` deadline check
    trapped in the `else` branch; select lacks a deadline case
- All consumers of the rollback / health-gate surface (each needs a scenario):
  - Critical-container health-gate failure path (`reconcile.go`) — triggers the
    rollback
  - `ComposeUpMultipleWithRollback` (`compose.go`) — the deploy-failure rollback
    path that returns the sentinels
  - `pollContainerHealth` / post-deploy verification (`health.go`) — the polling
    loop bounded by `HealthCheckTimeout` / `HealthCheckInterval`
- Docs: `skills/onboard/resources/gitops.md` (rollback + health-gate semantics).
