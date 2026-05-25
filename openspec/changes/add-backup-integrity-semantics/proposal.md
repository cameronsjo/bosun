# Change: Backup integrity and deploy error semantics (Cluster H)

## Why

The April 2026 reconcile-path bug hunt found that the backup-and-rollback safety
net is fictional in several failure modes, and that pre-deploy errors are
swallowed so the operator chases red herrings.

The reconcile spec currently states "Backup failures SHALL log a warning but
SHALL NOT abort the deployment pipeline" — but the implementation then deploys
with no usable rollback target, and three concrete bugs make that worse:

- `BackupRemote` discards the SSH error (`_ = sshErr`) and pipes remote `tar`
  stderr to `/dev/null`. An auth failure, host-key rejection, or mid-stream
  network drop still leaves a truncated-but-listable archive that passes
  verification; a later rollback restores prod to a state that was never on disk
  (#240, P0 silent corruption).
- When no existing paths are present (fresh host or misconfigured appdata path),
  `Backup` returns success without writing `configs.tar.gz`, sets
  `lastBackupPath` to an empty directory, and skips verification. A subsequent
  health-gate rollback then "restores" from a backup that never existed (#244).
- `CleanupBackups` runs immediately after creating the new backup. With
  `BackupsToKeep == 1` it deletes the prior cycle's last-known-good baseline
  before the new deploy is verified — so a deploy that breaks past the health
  gate leaves only a snapshot of the broken state (#243).
- The tar-over-SSH deploy path checks integrity only when `tarErr != nil`. A tar
  that exits 0 with an empty/partial archive passes, the move runs, and the
  target directory is replaced with garbage (#252).
- Every pre-deploy directory creation (`EnsureRemoteDir`, `MkdirAll`) uses
  `_ = ...`. When mkdir fails (FUSE mount down, permission denied, SSH dropped),
  the operator sees a confusing downstream "scp failed" / "file not found"
  instead of the real cause (#250).
- The health-gate rollback calls the deploy-or-rollback function with the NEW
  (failing) compose files; because containers already exist, `docker compose up`
  exits 0 and the function returns before ever consulting the backup. Bosun logs
  "rollback attempted" while Docker still runs the broken release (#229, P0 —
  the documented critical-container safety net does nothing).

These are spec gaps: the `reconcile` capability has no error semantics for
backup creation/verification, no ordering guarantee for retention versus deploy
success, no archive-integrity guarantee for the deploy path, and no requirement
that rollback restore the previous-good state rather than re-apply the failing
one. This change tightens those requirements to **fail closed before mutating
target state**.

## What Changes

- **Verified, fail-closed backups** — backup creation MUST be verified for deep
  integrity (the archive lists AND extracts/round-trips, not merely "non-empty
  and listable") before the deploy mutates target state. An unverifiable backup
  MUST abort the reconcile rather than proceed with no safety net. **BREAKING**:
  reverses the current "backup failure logs a warning and continues" behavior.
- **Empty backup is not a rollback target** — when there is genuinely nothing to
  back up, the reconciler MUST record "no backup available" (empty
  `lastBackupPath`) so downstream rollback skips cleanly with a clear message
  instead of failing opaquely against an empty directory.
- **Surfaced remote backup errors** — remote backup MUST propagate the SSH/tar
  error and MUST NOT suppress remote stderr; a transport-layer failure MUST fail
  the backup loudly rather than producing a passing-but-truncated archive.
- **Retention preserves the last-known-good** — backup pruning MUST NOT delete
  the prior-cycle backup until the current deploy has passed verification, so a
  `BackupsToKeep == 1` operator always retains a recoverable baseline through the
  deploy window.
- **Propagated deploy-prep errors** — directory-creation and remote-prep errors
  (`EnsureRemoteDir`, `MkdirAll`) MUST propagate with their real cause wrapped to
  name the failing layer, not be discarded.
- **Deploy archive integrity** — the tar-over-SSH deploy path MUST verify the
  extracted archive (e.g. file-count parity) before the atomic move; a partial
  or empty archive MUST abort the deploy and preserve the existing target.
- **Backup-backed rollback** — when a deploy fails or the health gate trips,
  rollback MUST re-apply the backed-up previous-good compose files and MUST NOT
  re-run the new (failing) file set; an idempotent `docker compose up` exit 0 on
  the failing files MUST NOT be treated as a successful rollback.
- **Coordinated with #319 (already merged)** — #319 landed a bounded
  `BackupTimeout` (`BOSUN_BACKUP_TIMEOUT`), context-bound verification, and a
  backup self-exclusion invariant on the `Configuration Backup` requirement. This
  change preserves all three clauses verbatim (so archiving does not revert them)
  and is what makes fail-closed safe: a stuck backup now fails fast and loud
  within the timeout rather than wedging the reconcile, so aborting on a required
  backup failure cannot hang the pipeline. The stage-7 carve-out in
  `Pipeline Orchestration` is updated from "backup failures are non-fatal" to the
  conditional rule: nothing-to-back-up proceeds, a required-backup failure aborts
  before state mutation.

## Impact

- Affected specs: `reconcile` — MODIFIED `Configuration Backup`,
  `File Deployment`, and `Pipeline Orchestration`; ADDED
  `Backup-Backed Rollback Integrity`.
- Affected code:
  - `internal/reconcile/backup.go` — `Backup` (`:54-110`), `BackupRemote`
    discarded `sshErr` and `2>/dev/null` (`:150-184`), `VerifyBackup`,
    `CleanupBackups` (`:187-225`)
  - `internal/reconcile/reconcile.go` — backup/cleanup ordering (`:1067-1085`),
    swallowed `EnsureRemoteDir`/`MkdirAll` (`:1218`, `:1233`, `:1330`, `:1340`,
    `:1351`), health-gate rollback (`:872-877`)
  - `internal/reconcile/ssh.go` — tar-over-SSH extract/move integrity (`:224-262`)
  - `internal/reconcile/compose.go` — `ComposeUpMultipleWithRollback`
    (`:172-192`) vs the isolated rollback path (`:325`)
- Findings addressed: #240, #243, #244, #250, #252, #229.
- Out of scope (tracked elsewhere): #251 and #276 (orphan remote `tar`/temp-dir
  cleanup on context cancellation) — pure subprocess-orphan cleanup, not backup
  integrity. SSH option-injection hardening already shipped in Cluster A.
- Docs: `skills/onboard/resources/gitops.md` (backup/rollback semantics),
  `docs/troubleshooting.md`, `CLAUDE.md` reconcile-pipeline notes.
