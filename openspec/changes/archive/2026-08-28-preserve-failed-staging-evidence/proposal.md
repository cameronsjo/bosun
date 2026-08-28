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

- Move successful staging cleanup until after the configured health gate,
  post-sync hooks, and post-deploy verification have completed successfully.
- Preserve the exact rendered tree in its existing per-target `StagingDir` after
  any post-render deployment failure, including health-gate failure with either a
  successful or failed rollback.
- Treat the staging path as a single evidence slot: the next render replaces it,
  so repeated failures do not create unbounded timestamped copies.
- Protect every staging slot with a `0700` root before rendered output is written
  and throughout verification, while preserving the descendant modes that
  deployment copies to its destination. Harden every retained directory to `0700`
  and regular file to `0600`. Reject symlinks, irregular entries, traversal outside
  the effective slot, and entry-replacement races rather than following them.
- If Bosun cannot prove that a staging tree is private, delete it rather than
  knowingly leave secret-bearing plaintext broadly readable; report either
  outcome without logging file contents.
- Keep every target's evidence and cleanup isolated to that target's effective
  staging directory, and reject equal or nested effective staging paths before any
  target executes.

## Impact

- Affected specs: `reconcile` (`Pipeline Orchestration` is MODIFIED; `Failed
  Staging Evidence Lifecycle` is ADDED).
- Affected code:
  - `internal/reconcile/reconcile.go` — `Reconciler.Run`, `renderTemplates`, and
    `cleanupStaging`; health-gate, rollback, verification, and success paths
  - `internal/reconcile/target.go` — canonical disjoint-path validation for the
    effective per-target staging directories (no configuration shape change)
- All consumers:
  - Single-target `bosun reconcile` — preserves or cleans its configured staging
    slot according to the target outcome
  - Multi-target CLI reconciliation — rejects overlapping staging slots up front,
    then continues valid sibling targets after a per-target failure only while the
    shared cycle context remains live, while keeping their evidence isolated
  - Daemon reconciliation — applies the same preflight and per-target behavior
    across automated retries
  - Template rendering — securely prepares and replaces the prior evidence slot
    only when the target reaches the next render phase
- Docs: `docs/gitops.md`, `skills/onboard/resources/gitops.md`, and staging-path
  descriptions in `skills/onboard/resources/configuration.md`.
