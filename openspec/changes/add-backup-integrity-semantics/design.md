## Context

The reconcile pipeline takes a configuration backup, then mutates the target
(local copy or tar-over-SSH), then runs `docker compose up`, then gates on
critical-container health. Backup, deploy, and rollback evolved independently and
each has a fail-open seam: backup creation swallows transport errors and accepts
truncated archives; retention runs before deploy verification; deploy-prep
mkdir errors are discarded; the tar extract is only integrity-checked on a
non-nil tar error; and the health-gate rollback reuses the deploy-or-rollback
function, which re-runs the failing files. The net effect is a safety net that
reports success while leaving prod broken or unrecoverable.

## Goals / Non-Goals

- Goals:
  - Fail closed before mutating target state when no usable backup exists.
  - Surface the real root cause of pre-deploy failures to the operator.
  - Guarantee a recoverable last-known-good through the deploy window.
  - Make rollback restore the previous-good state, not re-apply the failing one.
- Non-Goals:
  - Reworking the backup archive format or switching tar-over-SSH to rsync
    (mentioned in findings as an option; left to implementation choice).
  - Orphan remote `tar`/temp-dir cleanup on context cancellation (#251, #276).
  - Changing the SSH host-injection allowlist (shipped in Cluster A).

## Decisions

- **Decision: Reverse "backup failure is non-fatal" for backups that protect a
  mutation.** The existing spec lets backup failure log a warning and continue;
  this leaves the deploy with no rollback target. The new rule is fail-closed: a
  required backup that cannot be created/verified aborts the reconcile before any
  target mutation. The genuinely-nothing-to-back-up case is distinct — it records
  "no backup available" rather than a failure, and downstream rollback skips.
  - Alternative considered: keep continuing-on-warning but mark the deploy
    "unrecoverable" — rejected; it preserves the silent-corruption window.

- **Decision: Build fail-closed on top of #319's bounded timeout, not in place of
  it.** #319 (already merged) gave `Configuration Backup` a `BackupTimeout`
  (`BOSUN_BACKUP_TIMEOUT`), context-bound verification, and a self-exclusion
  invariant — the original "non-fatal" rule existed only to stop a hung backup
  from wedging the reconcile. With the timeout in place a stuck backup now fails
  fast and loud, so the wedge risk that motivated "non-fatal" is gone, and
  aborting on a required-backup failure cannot hang the pipeline. Because an
  OpenSpec `MODIFIED` requirement replaces the whole block on archive, this change
  re-states all three #319 clauses verbatim so archiving does not silently revert
  them, and correspondingly updates the `Pipeline Orchestration` stage-7 carve-out
  from "non-fatal" to the conditional rule (nothing-to-back-up proceeds; a
  required-backup failure aborts before state mutation).
  - This resolves the merge-ordering hazard between #319 and this change: the two
    no longer contradict, and #319's behavior survives the archive.

- **Decision: Deep verification, not "non-empty + listable".** A truncated gzip
  can still list. Verification must round-trip (extract to a scratch location or
  validate the gzip trailer / file-count manifest) so a partial archive fails.

- **Decision: Defer retention until after deploy verification.** Pruning the
  prior last-known-good before the new deploy is proven destroys the only
  recoverable baseline at `BackupsToKeep == 1`. Cleanup moves after verification.
  Its verification and cleanup run under a second, independent `BackupTimeout`;
  expiry warns and preserves the unprocessed backups without revoking the
  already-verified deploy's success.

- **Decision: Distinct rollback path.** The health-gate failure case is
  "deploy succeeded but is unhealthy", so re-running the new files is a no-op that
  exits 0. Rollback must explicitly re-apply the backup compose files via a
  dedicated path (mirroring the isolated rollback at `compose.go:325`).

## Risks / Trade-offs

- **Fail-closed backups can block a first deploy on a fresh host** → mitigated by
  the explicit no-backup-available branch: nothing to back up is not a failure.
- **Deferred retention can briefly exceed `BackupsToKeep`** → acceptable; the
  extra backup is the safety margin and is pruned after verification.
- **Deep verification adds I/O per backup** → bounded by archive size; backups
  already stream the full tar, so the marginal cost is a single extract/list pass.

## Migration Plan

1. Operators relying on "backup failure continues anyway" must ensure the backup
   target is writable; a failing backup now aborts the reconcile with a clear
   cause. Fresh hosts with nothing to back up are unaffected.
2. Rollback: revert the change — no persisted state format changes, so rollback
   is code-only.

## Open Questions

- Verification mechanism: full extract to scratch vs gzip-trailer + file-count
  manifest. Lean toward file-count parity to bound cost; settle in implementation.
- Whether the "no backup available" state should additionally suppress the
  health-gate rollback attempt entirely or merely no-op it (lean: no-op with a
  clear log line).
