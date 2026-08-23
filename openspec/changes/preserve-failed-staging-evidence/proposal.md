# Change: Preserve failed staging evidence through deployment verification

## Why

Bosun deletes the rendered staging tree immediately after compose succeeds, before
the health gate and post-deploy verification run. When either check fails, the
operator loses the exact render that produced the bad deployment and cannot diff
it against the restored backup (#274).

The render may contain plaintext values interpolated from SOPS secrets, so keeping
timestamped copies under `BackupDir` would turn short-lived diagnostic data into a
durable secret archive. Evidence retention therefore needs an explicit bounded and
fail-closed security lifecycle, not only a reordered cleanup call.

## What Changes

- Move successful staging cleanup until after the health gate, post-sync hooks, and
  post-deploy verification have completed successfully.
- Preserve the exact rendered tree in its existing per-target `StagingDir` after
  any post-render deployment failure, including health-gate failure with either a
  successful or failed rollback.
- Treat the staging path as a single evidence slot: the next render replaces it,
  so repeated failures do not create unbounded timestamped copies.
- Restrict retained evidence to owner-only access without following symlinks. If
  permissions cannot be hardened, delete the evidence rather than knowingly leave
  secret-bearing plaintext broadly readable; report either outcome without logging
  file contents.
- Keep every target's evidence and cleanup isolated to that target's effective
  staging directory.

## Impact

- Affected specs: `reconcile` (`Pipeline Orchestration` is MODIFIED; `Failed
  Staging Evidence Lifecycle` is ADDED).
- Affected code:
  - `internal/reconcile/reconcile.go` — `Reconciler.Run`, `renderTemplates`, and
    `cleanupStaging`; health-gate, rollback, verification, and success paths
  - `internal/reconcile/target.go` — per-target staging derivation consumed by the
    lifecycle (no configuration shape change expected)
- All consumers:
  - Single-target `bosun reconcile` — preserves or cleans its configured staging
    slot according to the target outcome
  - Multi-target CLI reconciliation — continues other targets while keeping each
    target's evidence isolated
  - Daemon reconciliation — same per-target behavior across automated retries
  - Template rendering — replaces the prior evidence slot only when the target
    reaches the next render phase
- Docs: `docs/gitops.md`, `skills/onboard/resources/gitops.md`, and staging-path
  descriptions in `skills/onboard/resources/configuration.md`.
