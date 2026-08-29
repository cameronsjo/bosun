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
- Hook change-source selection SHALL preserve its existing priority: remote
  deploys fire all hooks; local content-hash results are authoritative even when
  empty; standard-copy local results use non-empty `WrittenFiles` or
  `DeletedFiles` as direct evidence; and only an empty standard-copy result uses
  normalized git diff. This proposal SHALL NOT add a parallel authority marker.
- Stage 9 SHALL verify every reported path exists with the source entry's type and
  a fresh mtime. A directory-only change set SHALL still run the regular-file
  content-equality check that detects silent sync failures.
- A managed file-to-directory transition SHALL report the transitioned path and
  every descendant directory created from the source subtree, including
  a top-level target transition.
- Consumer-facing GitOps, invariant, troubleshooting, and onboard-skill
  documentation SHALL describe the expanded path-based contract.

## Non-Goals

- Persisting empty-directory ownership in `DeployState.DeployedFiles`, and using
  that ownership to authorize a later directory-to-file transition, is a
  separate change. Reverse transitions remain governed only by the existing
  regular-file-backed managed-state semantics; this proposal does not broaden
  persisted ownership.
- Remote tar-over-SSH and standard-copy deployments remain without authoritative
  per-path change tracking; their existing hook fallback behavior is unchanged.
- This proposal does not rename the compatibility field `WrittenFiles`, despite
  expanding its entries from regular files to created-or-written filesystem
  paths.

## Impact

- Affected specs: `reconcile-fuse-hooks` (ADDED directory-aware change-set
  requirement; depends on the active `add-reconcile-fuse-hooks` capability
  proposal) and `reconcile` (MODIFIED `Deploy Sync Invariants` requirement).
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
    namespace discovered appdata-target paths; the separate compose deployment
    path applies the same prefix contract.
  - Invariant consumer: `verifyDeployTarget` validates every returned path and
    separately preserves source-file content equality when no regular file was
    written.
  - Hook consumers: `executePostSyncHooks` and `EvaluatePostSyncHooks` treat the
    merged `WrittenFiles` / `DeletedFiles` values as the complete deploy path set
    in local content-hash mode, including authoritative empty and deletion-only
    results; standard-copy local results use non-empty slices as direct evidence.
  - Fallback consumers: standard-copy local deploys use normalized git diff only
    when both path slices are empty; remote deploys retain their existing
    unconditional all-hooks behavior regardless of either slice's length.
  - Persistence consumer: `DeployState.DeployedFiles` remains a regular-file
    manifest and does not gain empty-directory ownership in this change.
- Tests: focused fileutil directory-creation and collision cases; local deploy
  aggregation, prefixing, type-transition, hook-matching, invariant existence,
  wrong-type, stale-mtime, directory-only content, content-hash no-op and
  deletion-only authority, standard-copy direct and fallback selection, remote
  selection, and repeat no-op regressions.
- Docs: `docs/adr/0013-deploy-sync-invariant-gates.md`, `docs/gitops.md`,
  `docs/error-handling.md`, `docs/troubleshooting.md`, and
  `skills/onboard/resources/gitops.md`; configuration docs only where they
  describe the unchanged `BOSUN_SKIP_DEPLOY_INVARIANT` behavior.
- Ordering: archive `add-reconcile-fuse-hooks` first so the base
  `reconcile-fuse-hooks` capability exists, then archive this dependent delta;
  implementation PR #551 remains gated on this proposal carrying
  `ready-to-build`. The label stabilizes the specification; implementation PR
  #551 MUST NOT merge until the required onboard GitOps resource update in task
  4.2 lands with the behavior it documents.
