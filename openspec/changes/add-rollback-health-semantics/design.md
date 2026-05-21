## Context

Cluster E of the April 2026 reconcile-path bug hunt covers two P0 findings in
the post-deploy safety path. Both failures share a root pattern: a guard appears
to run but does nothing useful in its failure mode.

- #229: the health-gate "rollback" reuses `ComposeUpMultipleWithRollback`, which
  is a *deploy-or-rollback* helper. It runs `docker compose up` against the
  caller-supplied compose files first and only falls back to the backup if that
  fails. The health-gate caller supplies the **new** (failing) files, so the
  initial `up` exits 0 (containers exist, just unhealthy) and the backup is never
  consulted.
- #230: `pollContainerHealth` keeps its deadline check inside the success branch
  of `CollectActualState`, and its select has no deadline case. A sustained
  Docker API error therefore loops forever.

The fix is semantic, not cosmetic: "rollback" must mean "restore the prior good
state," and "poll for health" must be bounded on every exit path. Encoding these
as spec requirements gives the implementation something to regress against.

## Goals / Non-Goals

- Goals:
  - Rollback after a failed health gate restores the backed-up prior compose
    files, not the new render.
  - Health polling terminates on Docker API errors and at the deadline.
  - A failed rollback is reported distinctly from a successful one.
- Non-Goals:
  - Changing the health-gate classification table (healthy / no-healthcheck /
    unhealthy / starting / missing) — that stays as specified.
  - Adding new config or env vars. Existing `HealthCheckTimeout`,
    `HealthCheckInterval`, `HealthGateTimeout`, and backup paths are sufficient.
  - Remote-deploy rollback (the health gate is already skipped for remote
    targets).

## Decisions

- **Decision: separate "restore from backup" from "deploy or rollback."** The
  health-gate failure path gets a dedicated restore call that deploys
  `r.lastBackupPath`'s compose files directly, modeled on the existing
  `ComposeUpIsolated` rollback at `compose.go:~325`. The deploy-failure path
  keeps using `ComposeUpMultipleWithRollback`.
  - Alternatives considered: make `ComposeUpMultipleWithRollback` detect the
    already-running case and force the backup. Rejected — overloading one helper
    with two opposite intents is exactly what produced the bug.

- **Decision: deadline check runs unconditionally each iteration; select carries
  a deadline case.** A persistent `CollectActualState` error yields a failed
  `HealthCheckResult` at the deadline rather than an infinite loop.
  - Alternatives considered: bail on the first Docker API error. Rejected — a
    transient blip should not fail an otherwise healthy deploy; the timeout is
    the correct bound. The distinction is *unhealthy* (keep polling) vs *cannot
    query* (bounded by deadline, then fail).

- **Decision: preserve the `ErrRollbackSucceeded` / `ErrRollbackFailed` sentinel
  contract.** Service Orchestration already defines these sentinels for the
  deploy-failure rollback. The health-gate restore path reuses them so callers
  and logs stay consistent, and a failed restore is logged at ERROR as a
  critical state.

## Risks / Trade-offs

- A backup that is itself broken means restore cannot recover → surfaced as
  `ErrRollbackFailed` (critical state), which is the honest outcome rather than a
  false success.
- Treating sustained Docker API errors as a deadline-bounded failure could mask a
  genuinely slow-but-recovering daemon → mitigated because the bound is the
  configured `HealthCheckTimeout`, the same window operators already tune.

## Migration Plan

No config migration. Behavior change is strictly safer: rollback that previously
silently no-op'd now restores the backup; health polling that previously hung now
terminates. No state-file schema change.

## Open Questions

- None blocking. Whether the restore path should also re-run post-restore health
  verification is left to implementation; the spec requires only that the prior
  good files are redeployed and the outcome is surfaced via the sentinels.
