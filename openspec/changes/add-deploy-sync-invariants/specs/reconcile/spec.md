## ADDED Requirements

### Requirement: Deploy Sync Invariants

The reconciler SHALL enforce three invariants between deploy sync (stage 8) and `docker compose up` (stage 10) to prevent silent-success failures where rendered templates fail to overwrite the destination files.

**Invariant 1 — Declared services present.** When template rendering completes and `ExtractDeclaredState` runs, the reconciler SHALL distinguish two failure modes:

- `ErrComposeDirMissing` — the configured staging compose directory does not exist on disk
- `ErrNoDeclaredServices` — the compose directory exists but contains no parseable services

Either error SHALL fail the reconcile run, unless the operator opts in via `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true`. When the override is set, the reconciler SHALL log a clearly-formatted warning (not `info`) and continue.

**Invariant 2 — Written files exist with fresh mtime.** After deploy sync completes, for each path in `WrittenFiles` across all targets, the reconciler SHALL stat the destination path and assert `mtime >= reconcileStartTime`. If any destination is missing or stale, the reconciler SHALL fail before compose-up runs.

**Invariant 3 — Non-empty source must produce written files.** For each deploy target whose source staging directory contains at least one regular file, the reconciler SHALL assert that the target's `WrittenFiles` slice is non-empty. An empty `WrittenFiles` against a non-empty source indicates the sync silently no-op'd, and SHALL fail the reconcile run.

The invariant check (invariants 2 and 3) MAY be skipped via `BOSUN_SKIP_DEPLOY_INVARIANT=true` for diagnostic or development scenarios. When skipped, the reconciler SHALL emit a `warn` log noting that invariants are disabled.

Per-file write decisions SHALL be observable: `CopyDirIfChanged` and `CopyFileIfChanged` SHALL emit a `Debug` log on every file write (with `src`, `dst`, `bytes`) and every skip (with `reason=hash_match`). This gives operators a way to confirm sync behavior from the log stream without inspecting destination mtimes externally.

#### Scenario: Reconcile fails when declared services is zero

- **WHEN** `ExtractDeclaredState` returns `ErrNoDeclaredServices`
- **AND** `BOSUN_ALLOW_EMPTY_DECLARED_STATE` is unset or `false`
- **THEN** the reconciler fails the pipeline at stage 6
- **AND** the error message names the staging compose directory path
- **AND** compose up does not run

#### Scenario: Override allows empty declared services

- **WHEN** `ExtractDeclaredState` returns `ErrNoDeclaredServices`
- **AND** `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true`
- **THEN** the reconciler logs a warning and continues
- **AND** post-deploy verification still respects the existing "declared services were extracted" precondition

#### Scenario: Missing compose directory always fails

- **WHEN** `ExtractDeclaredState` returns `ErrComposeDirMissing`
- **THEN** the reconciler fails the pipeline regardless of `BOSUN_ALLOW_EMPTY_DECLARED_STATE`
- **AND** the error message names the expected directory path
- **AND** the operator is directed to verify the staging path configuration

#### Scenario: Stale destination mtime blocks compose-up

- **WHEN** deploy sync completes
- **AND** a destination file in `WrittenFiles` has `mtime < reconcileStartTime`
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the stale destination path

#### Scenario: Empty WrittenFiles against non-empty source blocks compose-up

- **WHEN** a deploy target's source staging directory contains regular files
- **AND** the target's `WrittenFiles` returned by `CopyDirIfChanged` is empty
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the target whose sync produced no writes

#### Scenario: Healthy deploy passes invariant check

- **WHEN** deploy sync writes at least one file per non-empty target
- **AND** every destination has `mtime >= reconcileStartTime`
- **THEN** the invariant check passes silently
- **AND** compose up proceeds normally

#### Scenario: Operator skips invariants for diagnostics

- **WHEN** `BOSUN_SKIP_DEPLOY_INVARIANT=true`
- **THEN** invariants 2 and 3 are bypassed
- **AND** the reconciler emits a `warn` log noting that invariants are disabled
- **AND** invariant 1 (declared services) is still enforced

#### Scenario: Per-file write logs emitted on Debug level

- **WHEN** `CopyDirIfChanged` writes 3 files and skips 2 files
- **AND** the log level is `Debug` or finer
- **THEN** five log lines are emitted total
- **AND** each write line includes `src`, `dst`, and `bytes`
- **AND** each skip line includes `reason=hash_match`

## MODIFIED Requirements

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

A failure at any stage SHALL abort the remaining stages and release the lock. The health gate (stage 12) failing SHALL trigger rollback before aborting. The invariant check (stage 9) failing SHALL abort before compose up runs; no rollback is needed because no compose changes have been applied at that point.

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
