## MODIFIED Requirements

### Requirement: Configuration Backup

The reconciler SHALL create a timestamped tar.gz backup of existing configuration files before deploying new ones, and SHALL fail closed when a required backup cannot be created or verified. Backups SHALL be named `backup-YYYYMMDD-HHMMSS`.

The reconciler SHALL verify backup integrity after creation by deep inspection — the archive SHALL list AND round-trip (extract or file-count parity), not merely be non-empty and listable. A truncated, empty, or corrupted archive SHALL cause the backup to fail. Backup verification SHALL NOT be skipped on any path that produced an archive. Verification SHALL run under the same cancellation context as creation, so a caller deadline or cancellation aborts verification rather than blocking indefinitely.

When the backup spans an active transport (remote tar-over-SSH), the reconciler SHALL propagate the transport error and SHALL NOT suppress the remote command's stderr. A transport-layer failure SHALL fail the backup loudly rather than producing a passing-but-truncated archive.

The backup archive SHALL NOT include the backup destination directory or any prior backup it contains. When the configured backup destination is nested within a backed-up path (for example, the reconciler's own appdata directory), the reconciler SHALL exclude the destination so the archive cannot recursively include its own growing output.

Backup creation and verification SHALL run under a configurable timeout (`BackupTimeout`, default 5 minutes, overridable via `BOSUN_BACKUP_TIMEOUT` accepting a Go duration or a plain number of seconds). When the timeout elapses, the backup SHALL be treated as a failure. For a required backup this aborts the reconcile under the fail-closed rule below; a cold-state reconcile with nothing to back up never starts a backup and is therefore unaffected.

When a required backup cannot be created or verified, the reconciler SHALL abort the reconcile before mutating target state, rather than proceeding with no rollback target.

When there is genuinely nothing to back up (no existing configuration paths present), the reconciler SHALL record that no backup is available by leaving the last backup path empty, rather than recording an empty directory as a successful backup.

Old backups SHALL be pruned to retain only the most recent N backups (configurable via `BackupsToKeep`, default 5). Pruning SHALL NOT delete the prior-cycle backup until the current deploy has passed verification, so the previous last-known-good baseline survives through the deploy window.

The last backup path SHALL be stored for potential rollback during compose up. An empty last backup path SHALL signal "no backup available" to downstream rollback.

#### Scenario: Local backup created and verified

- **WHEN** a local deployment runs with existing config files
- **THEN** a timestamped tar.gz is created containing the config paths
- **AND** the archive is verified by deep inspection (lists and round-trips), not merely non-empty

#### Scenario: Remote backup via SSH

- **WHEN** a remote deployment runs
- **THEN** a tar command runs over SSH to create the archive
- **AND** transient SSH errors are retried with exponential backoff
- **AND** the completed archive is verified by deep inspection

#### Scenario: Remote backup propagates transport failure

- **WHEN** a remote backup's SSH session fails (auth rejection, host-key mismatch, or mid-stream network drop)
- **THEN** the remote command's stderr is surfaced, not discarded
- **AND** the backup fails with the transport error rather than reporting success on a truncated archive

#### Scenario: Backup destination excluded from the archive

- **WHEN** the configured backup destination is nested within a backed-up path
- **THEN** the created archive does not contain the backup destination directory or any prior backup
- **AND** the archive size does not grow with the number of prior backups present

#### Scenario: Backup exceeds its timeout

- **WHEN** required backup creation or verification runs longer than `BackupTimeout`
- **THEN** the backup is aborted and treated as a failure
- **AND** the reconcile aborts before mutating target state
- **AND** an error names the timeout as the cause

#### Scenario: Truncated archive fails verification

- **WHEN** a backup archive is produced but is truncated or partial
- **THEN** deep verification detects the corruption
- **AND** the backup is reported as failed

#### Scenario: Unverifiable backup aborts the reconcile

- **WHEN** a required backup cannot be created or cannot be verified
- **THEN** the reconcile aborts before any target file is written or compose up runs
- **AND** an error names the backup failure as the cause

#### Scenario: Nothing to back up yields no rollback target

- **WHEN** no existing configuration paths are present (fresh host or empty appdata path)
- **THEN** the reconciler records that no backup is available with an empty last backup path
- **AND** a later rollback skips cleanly with a "no backup available" message instead of failing against an empty directory

#### Scenario: Old backups pruned

- **WHEN** more than `BackupsToKeep` backups exist after the current deploy passes verification
- **THEN** the oldest backups are removed, keeping only the most recent N
- **AND** the prior-cycle backup remains available throughout the deploy window

#### Scenario: Retention preserves last-known-good through deploy

- **WHEN** `BackupsToKeep` is 1 and a new backup is created at the start of a deploy cycle
- **THEN** the prior-cycle backup is retained until the current deploy passes verification
- **AND** pruning to the configured count occurs only after successful verification

#### Scenario: Backup failure does not block deploy

- **WHEN** no existing configuration paths are present, so no required backup is attempted
- **THEN** the empty-source result is not treated as a backup failure
- **AND** the deployment continues with an empty last backup path

### Requirement: File Deployment

The reconciler SHALL support two deployment modes: local (direct file operations) and remote (SSH+tar or SCP), and SHALL propagate directory-preparation errors with their real cause rather than discarding them.

Local deployment SHALL use atomic-like operations: copy source to a temp directory in the same parent, then rename to the target path. This provides atomic directory replacement with `--delete` semantics (files in target not in source are removed).

Remote deployment SHALL use tar-over-SSH for directories (tar source, pipe to SSH for extraction in a temp dir, atomic move to target) and SCP for individual files (SCP to temp file, then atomic move).

Pre-deploy directory creation (local `MkdirAll` and remote `EnsureRemoteDir`) SHALL propagate failures, wrapped to name the failing path and layer. Such errors SHALL NOT be discarded, so a downstream copy/scp failure never masks an mkdir or mount failure as the apparent cause.

The tar-over-SSH deployment SHALL verify the extracted archive (e.g. file-count parity between the local source and the remote extraction) before performing the atomic move. A partial or empty archive — even when the tar command exits 0 — SHALL abort the deploy and SHALL preserve the existing target directory.

All remote operations SHALL retry on transient SSH errors (connection refused, timeout, network unreachable) with exponential backoff (1s, 2s, 4s, max 3 attempts).

All remote operations SHALL validate the host string against an allowlist pattern and reject strings starting with `-` to prevent SSH option injection.

#### Scenario: Local atomic-like directory deployment

- **WHEN** deploying a directory locally
- **THEN** files are copied to a temp directory in the target's parent
- **AND** the old target is renamed aside (e.g., `target.old`)
- **AND** the temp directory is renamed to the target path
- **AND** the aside directory is removed after successful rename
- **AND** if the final rename fails, the aside directory is renamed back to restore the previous state

#### Scenario: Remote deployment with SSH retry

- **WHEN** a remote SSH operation fails with "connection refused"
- **THEN** it retries up to 3 times with exponential backoff
- **AND** succeeds on a subsequent attempt

#### Scenario: SSH host validation rejects injection

- **WHEN** a target host contains shell metacharacters or starts with `-`
- **THEN** the operation fails with a validation error before any SSH command runs

#### Scenario: Directory-prep failure surfaces the real cause

- **WHEN** a pre-deploy `MkdirAll` or `EnsureRemoteDir` fails (FUSE mount down, permission denied, SSH dropped)
- **THEN** the deploy fails with an error that names the failing path and the underlying cause
- **AND** the error is not masked by a confusing downstream "file not found" or "scp failed"

#### Scenario: Partial extracted archive aborts the deploy

- **WHEN** the tar-over-SSH extraction produces an empty or partial archive even though the tar command exits 0
- **THEN** the extracted-file integrity check fails before the atomic move
- **AND** the existing target directory is preserved unchanged

### Requirement: Pipeline Orchestration

The reconciler SHALL execute stages in this fixed order:

1. Acquire lock
2. Git repository sync
3. Load deploy state and evaluate skip/circuit-breaker logic
4. Decrypt secrets (SOPS)
5. Render templates (Go text/template + Sprig)
6. Extract declared state from rendered compose
7. Create configuration backup
8. Deploy files (local or remote)
9. Deploy sync invariant check (see Deploy Sync Invariants)
10. Run `docker compose up`
11. Clean up staging directory
12. Critical container health gate (if configured)
13. Execute post-sync hooks
14. Post-deploy verification (drift check)
15. Record successful deployment in state file
16. Release lock

A failure at any stage SHALL abort the remaining stages and release the lock. Configuration backup (stage 7) is the sole conditional case: when there is genuinely nothing to back up (no existing configuration paths), the empty result is recorded and the pipeline proceeds; but when a required backup cannot be created or verified — including on timeout — the reconciler SHALL abort before mutating target state (stage 8 onward), consistent with the fail-closed Configuration Backup requirement. The health gate (stage 12) failing SHALL trigger rollback before aborting. The invariant check (stage 9) failing SHALL abort before compose up runs; no rollback is needed because no compose changes have been applied at that point.

The lock SHALL always be released via defer, even on panic.

#### Scenario: Full pipeline succeeds

- **WHEN** a reconciliation is triggered and a new commit is available
- **THEN** all stages execute in order
- **AND** the deploy state file records the deployed commit
- **AND** a success alert is sent

#### Scenario: Pipeline aborts on stage failure

- **WHEN** secret decryption fails
- **THEN** template rendering, backup, deploy, invariant check, and compose stages are skipped
- **AND** a throttled failure alert is sent
- **AND** the lock is released

#### Scenario: Required backup failure aborts before deploy

- **WHEN** a required backup (existing config paths present) cannot be created or verified, including on timeout
- **THEN** stages 8–15 (deploy through state recording) are skipped
- **AND** no target file is written and the existing state is left intact
- **AND** the lock is released

#### Scenario: Invariant check aborts before compose

- **WHEN** stage 9 invariants fail
- **THEN** compose up, cleanup, health gate, hooks, and verification are skipped
- **AND** the lock is released
- **AND** a failure alert is sent
- **AND** the state file is NOT updated

#### Scenario: Dry run mode

- **WHEN** `DryRun` is true
- **THEN** backup, deploy, invariant check, compose up, health gate, post-sync hooks, and post-deploy verification are skipped
- **AND** template rendering still executes to validate templates

#### Scenario: Health gate failure triggers rollback

- **WHEN** compose up succeeds but a critical container fails the health gate
- **THEN** the reconciler triggers rollback to the backup compose files
- **AND** the deployment is NOT recorded as successful
- **AND** a failure alert is sent
- **AND** the lock is released

## ADDED Requirements

### Requirement: Backup-Backed Rollback Integrity

Rollback SHALL re-apply the backed-up previous-good compose files and SHALL NOT re-run the new (failing) compose file set. When a deploy fails or the critical-container health gate trips, the reconciler SHALL restore service state from the backup, not redeploy the release that just failed.

An idempotent `docker compose up` that exits 0 against the failing files (because the unhealthy containers already exist) SHALL NOT be treated as a successful rollback. Rollback success SHALL be determined by re-applying the previous-good files, and the reconciler's logs and result status SHALL reflect the actual rollback outcome rather than a misleading "rollback attempted".

When no backup is available (empty last backup path), the reconciler SHALL NOT attempt a backup-based rollback and SHALL log that no rollback target exists.

#### Scenario: Health-gate failure restores previous-good files

- **WHEN** compose up exits 0 but a critical container fails the health gate
- **THEN** rollback re-applies the backed-up previous-good compose files
- **AND** the failing new file set is not re-run as the rollback action

#### Scenario: Idempotent re-up is not counted as rollback success

- **WHEN** a rollback would re-run the new files and `docker compose up` exits 0 because the unhealthy containers already exist
- **THEN** the reconciler does not report this as a successful rollback
- **AND** the result status reflects that the previous-good state was restored or that rollback failed

#### Scenario: No backup available skips rollback cleanly

- **WHEN** a deploy fails and the last backup path is empty
- **THEN** the reconciler logs that no rollback target is available
- **AND** does not attempt to restore from an empty or missing backup
