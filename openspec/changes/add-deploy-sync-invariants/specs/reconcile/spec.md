## ADDED Requirements

### Requirement: Deploy Sync Invariants

The reconciler SHALL enforce three invariants between deploy sync (stage 8) and `docker compose up` (stage 10) to prevent silent-success failures where rendered templates fail to overwrite the destination files.

**Invariant 1 — Declared services present.** When template rendering completes and `ExtractDeclaredState` runs, the reconciler SHALL distinguish two failure modes:

- `ErrComposeDirMissing` — the configured staging compose directory does not exist on disk
- `ErrNoDeclaredServices` — the compose directory exists but contains no parseable services

`ErrComposeDirMissing` SHALL fail the reconcile run unconditionally; the override does not apply because a missing compose directory indicates a misconfigured staging path, not a genuinely empty repo.

`ErrNoDeclaredServices` SHALL fail the reconcile run unless the operator opts in via `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true`. When the override is set, the reconciler SHALL log at `Warn` level (not `Info`) and continue.

**Invariant 2 — Written files exist with fresh mtime.** After deploy sync completes, for each path in `WrittenFiles` across all targets, the reconciler SHALL stat the destination path and assert `mtime >= reconcileStartTime`. If any destination is missing or stale, the reconciler SHALL fail before compose-up runs.

**Invariant 3 — Non-empty source must be reflected at the destination.** For each deploy target whose source staging directory contains at least one regular file but whose `WrittenFiles` slice is empty, the reconciler SHALL inspect the destination directly: it SHALL assert that every regular file in the source is present at its corresponding destination path AND byte-identical to the source (SHA-256 content equality — the same comparison `CopyFileIfChanged` uses to decide a write is skippable; no mtime assertion, since a content-hash match means the files were written on a prior run). Symlinks in the source SHALL be skipped (Lstat semantics), matching the copy path, which never deploys them; a symlink-only source therefore imposes no requirement on the destination. If every source file is present and content-equal, the zero-write result is a legitimate no-op (the destination already byte-matches the source) and the invariant SHALL pass. If any source file is absent from the destination or differs in content, the sync silently failed and the reconciler SHALL fail the run, naming the first mismatching destination path.

This refines the original formulation, which failed *any* zero-write target against a non-empty source. That was too aggressive: with content-hash sync a target legitimately records zero writes when the destination already matches, so a single byte-identical config could abort an entire reconcile (see GH#330). Asserting the real post-condition — files present *and content-equal* at the destination — preserves protection against silent-sync failures while permitting genuine no-ops. Existence alone is insufficient: a stale destination file occupying the right path would pass an existence check yet serve outdated config, so content equality closes that gap.

The invariant check (invariants 2 and 3) MAY be skipped via `BOSUN_SKIP_DEPLOY_INVARIANT=true` for diagnostic or development scenarios. When skipped, the reconciler SHALL log at `Warn` level noting that invariants are disabled.

Per-file write decisions SHALL be observable: `CopyDirIfChanged` and `CopyFileIfChanged` SHALL emit a `Debug` log on every file write (formatted `wrote src=<src> dst=<dst> bytes=<n>`) and every skip (formatted `skipped src=<src> dst=<dst> reason=hash_match`). This gives operators a way to confirm sync behavior from the log stream without inspecting destination mtimes externally.

#### Scenario: Reconcile fails when declared services is zero

- **WHEN** `ExtractDeclaredState` returns `ErrNoDeclaredServices`
- **AND** `BOSUN_ALLOW_EMPTY_DECLARED_STATE` is unset or `false`
- **THEN** the reconciler fails the pipeline at stage 6
- **AND** the error message names the staging compose directory path
- **AND** compose up does not run

#### Scenario: Override allows empty declared services

- **WHEN** `ExtractDeclaredState` returns `ErrNoDeclaredServices`
- **AND** `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true`
- **THEN** the reconciler logs at `Warn` level and continues
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

#### Scenario: No-op sync against a content-matched destination passes

- **WHEN** a deploy target's source staging directory contains regular files
- **AND** the target's `WrittenFiles` returned by `CopyDirIfChanged` is empty
- **AND** every source file already exists at its corresponding destination path and is byte-identical to the source
- **THEN** the invariant check passes at stage 9 (legitimate no-op — destination already byte-matches)
- **AND** compose up proceeds normally

#### Scenario: Empty WrittenFiles with a missing destination file blocks compose-up

- **WHEN** a deploy target's source staging directory contains regular files
- **AND** the target's `WrittenFiles` returned by `CopyDirIfChanged` is empty
- **AND** at least one source file is absent from the destination
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the first mismatching destination path

#### Scenario: Empty WrittenFiles with a stale-content destination file blocks compose-up

- **WHEN** a deploy target's source staging directory contains regular files
- **AND** the target's `WrittenFiles` returned by `CopyDirIfChanged` is empty
- **AND** a destination file exists at the corresponding path but its content differs from the source (a stale write the content-hash sync failed to replace)
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the first mismatching destination path

#### Scenario: Symlinks in the source impose no destination requirement

- **WHEN** a deploy target's source staging directory contains only symlinks (no regular files)
- **AND** the target's `WrittenFiles` is empty
- **THEN** the invariant check passes at stage 9 (symlinks are never deployed, so nothing is required at the destination)
- **AND** compose up proceeds normally

#### Scenario: Healthy deploy passes invariant check

- **WHEN** deploy sync writes at least one file per non-empty target
- **AND** every destination has `mtime >= reconcileStartTime`
- **THEN** the invariant check passes silently
- **AND** compose up proceeds normally

#### Scenario: Operator skips invariants for diagnostics

- **WHEN** `BOSUN_SKIP_DEPLOY_INVARIANT=true`
- **THEN** invariants 2 and 3 are bypassed
- **AND** the reconciler logs at `Warn` level noting that invariants are disabled
- **AND** invariant 1 (declared services) is still enforced

#### Scenario: Per-file write logs emitted on Debug level

- **WHEN** `CopyDirIfChanged` writes 3 files and skips 2 files
- **AND** the log level is `Debug` or finer
- **THEN** five log lines are emitted total
- **AND** each write line includes `src`, `dst`, and `bytes`
- **AND** each skip line includes `src`, `dst`, and `reason=hash_match`

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
