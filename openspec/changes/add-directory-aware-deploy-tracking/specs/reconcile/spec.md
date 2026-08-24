## MODIFIED Requirements

### Requirement: Deploy Sync Invariants

The reconciler SHALL enforce three invariants between deploy sync (stage 8) and `docker compose up` (stage 10) to prevent silent-success failures where rendered templates fail to overwrite the destination paths.

**Invariant 1 — Declared services present.** When template rendering completes and `ExtractDeclaredState` runs, the reconciler SHALL distinguish two failure modes:

- `ErrComposeDirMissing` — the configured staging compose directory does not exist on disk
- `ErrNoDeclaredServices` — the compose directory exists but contains no parseable services

`ErrComposeDirMissing` SHALL fail the reconcile run unconditionally; the override does not apply because a missing compose directory indicates a misconfigured staging path, not a genuinely empty repo.

`ErrNoDeclaredServices` SHALL fail the reconcile run unless the operator opts in via `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true`. When the override is set, the reconciler SHALL log at `Warn` level (not `Info`) and continue.

**Invariant 2 — Created and written paths have the expected type and fresh mtime.** After deploy sync completes, for each path in `WrittenFiles` across all targets, the reconciler SHALL inspect the source and destination entries without following symlinks, assert that the destination exists with the same regular-file-or-directory type as the source, and assert `mtime >= reconcileStartTime`. A destination symlink SHALL NOT satisfy either expected type. If any destination is missing, stale, or the wrong type, the reconciler SHALL fail before compose-up runs.

**Invariant 3 — Non-empty source must be reflected at the destination.** For each deploy target whose source staging directory contains at least one regular file but whose `WrittenFiles` slice contains no regular-file write (whether the slice is empty or contains only newly created directories), the reconciler SHALL:

- Inspect the destination directly rather than inferring corruption or success from the path count.
- Assert that every regular file in the source is present at its corresponding destination path.
- Assert that every such file is byte-identical to the source using SHA-256 content equality — the same comparison `CopyFileIfChanged` uses to decide a write is skippable. No mtime assertion is performed for these files, since a content-hash match means they may have been written on a prior run.
- Skip symlinks in the source using Lstat semantics, matching the copy path, which never deploys them.
- Pass the invariant when every source file is present and content-equal — a legitimate no-op, since the destination already byte-matches the source.
- Fail the run when any source file is absent from the destination or differs in content, naming the first mismatching destination path.

A symlink-only source therefore imposes no content requirement on the destination. Any directory path recorded in `WrittenFiles` remains subject to invariant 2 even when the source contains no regular files.

This refines the original formulation, which failed *any* zero-write target against a non-empty source. That was too aggressive: with content-hash sync a target legitimately records zero regular-file writes when the destination already matches, including a deploy that only creates directories, so a single byte-identical config could abort an entire reconcile (see GH#330). Asserting the real post-condition — files present *and content-equal* at the destination — preserves protection against silent-sync failures while permitting genuine no-ops and directory-only changes. Existence alone is insufficient: a stale destination file occupying the right path would pass an existence check yet serve outdated config, so content equality closes that gap.

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
- **AND** a destination file or directory recorded in `WrittenFiles` has `mtime < reconcileStartTime`
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the stale destination path

#### Scenario: Wrong destination type blocks compose-up

- **WHEN** deploy sync records a regular file or directory in `WrittenFiles`
- **AND** the corresponding destination is missing, has the opposite file-or-directory type, or is a symlink
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the affected destination path

#### Scenario: No-op sync against a content-matched destination passes

- **WHEN** a deploy target's source staging directory contains regular files
- **AND** the target's `WrittenFiles` returned by `CopyDirIfChanged` contains no regular-file writes
- **AND** every source file already exists at its corresponding destination path and is byte-identical to the source
- **THEN** the invariant check passes at stage 9 (legitimate no-op — destination already byte-matches)
- **AND** compose up proceeds normally

#### Scenario: Empty WrittenFiles with a missing destination file blocks compose-up

- **WHEN** a deploy target's source staging directory contains regular files
- **AND** the target's `WrittenFiles` is empty or contains only newly created directories
- **AND** at least one source file is absent from the destination
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the first mismatching destination path

#### Scenario: Empty WrittenFiles with a stale-content destination file blocks compose-up

- **WHEN** a deploy target's source staging directory contains regular files
- **AND** the target's `WrittenFiles` is empty or contains only newly created directories
- **AND** a destination file exists at the corresponding path but its content differs from the source (a stale write the content-hash sync failed to replace)
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the first mismatching destination path

#### Scenario: Symlinks in the source impose no destination requirement

- **WHEN** a deploy target's source staging directory contains only symlinks (no regular files or directories recorded in `WrittenFiles`)
- **AND** the target's `WrittenFiles` is empty
- **THEN** the invariant check passes at stage 9 (symlinks are never deployed, so nothing is required at the destination)
- **AND** compose up proceeds normally

#### Scenario: Healthy deploy passes invariant check

- **WHEN** deploy sync records regular-file writes or newly created directories
- **AND** every recorded destination exists with the source entry's type and has `mtime >= reconcileStartTime`
- **AND** any target with no regular-file writes already content-matches every regular source file
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
