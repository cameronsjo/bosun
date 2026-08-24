# Change: Add directory-aware deploy change tracking

## Why

`CopyDirIfChanged` creates descendant directories but reports only regular-file
writes. A deploy that adds an empty directory therefore produces no observable
change for post-sync hooks, and a directory-only `WrittenFiles` result can bypass
the content check that protects stage 9 from silent sync failures (#358).

The active `add-reconcile-fuse-hooks` change defines the deployment result as the
authoritative local hook change set, but it does not define how created
directories participate in that set or in deploy invariants. This proposal adds
that missing contract before PR #551 is eligible to merge.

## What Changes

- Content-hash sync SHALL report every descendant directory it actually creates,
  including empty directories, while omitting ordinary deploy-root creation,
  plumbing ancestors, and pre-existing directories.
- Local deployment SHALL preserve those paths as canonical staging-relative
  `DeployResult.WrittenFiles` entries so post-sync hooks can observe directory
  creation without retriggering on a no-op reconcile.
- Stage 9 SHALL verify every reported path exists with the source entry's type and
  a fresh mtime. A directory-only change set SHALL still run the regular-file
  content-equality check that detects silent sync failures.
- A managed file-to-directory transition SHALL report the transitioned path and
  every descendant directory created in the private replacement tree, including
  a top-level target transition.
- Consumer-facing GitOps, invariant, troubleshooting, and onboard-skill
  documentation SHALL describe the expanded path-based contract.

## Non-Goals

- Persisting empty-directory ownership in `DeployState.DeployedFiles` is a
  separate change. Consequently, recognizing a directory created on one
  successful reconcile as managed for a later directory-to-file transition is
  outside this proposal; this proposal only covers ownership available during
  the current reconcile and existing file-backed managed-state semantics.
- Remote tar-over-SSH and standard-copy deployments remain without authoritative
  per-path change tracking; their existing hook fallback behavior is unchanged.
- This proposal does not rename the compatibility field `WrittenFiles`, despite
  expanding its entries from regular files to created-or-written filesystem
  paths.

## Impact

- Affected specs: `reconcile-fuse-hooks` (ADDED requirement; depends on the
  active `add-reconcile-fuse-hooks` capability proposal).
- Affected code:
  - `internal/fileutil/fileutil.go` — `CopyDirIfChanged`, its directory-creation
    helper, and the returned relative-path contract.
  - `internal/reconcile/deploy.go` — `DeployResult`, `AddWritten`,
    `PrefixLatest`, `DeployOps.DeployLocal`, and managed file-to-directory
    transition staging.
  - `internal/reconcile/reconcile.go` — per-target local deploy aggregation,
    stage-9 verification dispatch, and `executePostSyncHooks` consumption.
  - `internal/reconcile/verify.go` — destination existence, source type, fresh
    mtime, and no-regular-file-write content invariants.
  - `internal/reconcile/hooks.go` — `EvaluatePostSyncHooks` consumes the
    canonical created/written/deleted path set without file-only assumptions.
  - `internal/reconcile/state.go` — explicitly unchanged file-only persisted
    ownership boundary for the reverse-transition non-goal.
- All consumers:
  - Producer: `fileutil.CopyDirIfChanged` returns target-relative regular-file
    writes and newly created descendant directories.
  - Aggregators: `DeployOps.DeployLocal`, managed type transitions,
    `DeployResult.AddWritten`, and `DeployResult.PrefixLatest` preserve and
    namespace the returned paths.
  - Invariant consumer: `verifyDeployTarget` validates every returned path and
    separately preserves source-file content equality when no regular file was
    written.
  - Hook consumers: `executePostSyncHooks` and `EvaluatePostSyncHooks` treat the
    merged `WrittenFiles` / `DeletedFiles` values as deploy paths.
  - Persistence consumer: `DeployState.DeployedFiles` remains a regular-file
    manifest and does not gain empty-directory ownership in this change.
- Tests: focused fileutil directory-creation and collision cases; local deploy
  aggregation, prefixing, type-transition, hook-matching, invariant existence,
  wrong-type, stale-mtime, directory-only content, and repeat no-op regressions.
- Docs: `docs/adr/0013-deploy-sync-invariant-gates.md`, `docs/gitops.md`,
  `docs/error-handling.md`, `docs/troubleshooting.md`, and
  `skills/onboard/resources/gitops.md`; configuration docs only where they
  describe the unchanged `BOSUN_SKIP_DEPLOY_INVARIANT` behavior.
