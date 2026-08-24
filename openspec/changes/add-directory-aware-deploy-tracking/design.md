## Context

Local content-hash deployment already returns an authoritative set of paths
changed on disk, but its compatibility field `WrittenFiles` currently records
only regular-file writes. Directory creation therefore disappears between the
filesystem producer, per-target aggregation, stage-9 verification, and hook
matching. The same field is also consumed by modes that do not provide
authoritative per-path evidence, so expanding the contract must not make their
empty results authoritative.

## Goals / Non-Goals

- Record newly created descendant directories through the existing local
  content-hash change-set pipeline.
- Preserve canonical staging-relative paths for hooks and source-relative paths
  for per-target invariant verification.
- Distinguish ordinary target-root plumbing from a target root that is itself a
  managed file-to-directory transition.
- Keep persisted ownership file-only; ownership of empty directories and later
  directory-to-file transitions based on that ownership remain out of scope.
- Keep standard-copy and remote fallback behavior unchanged.

## Decisions

- **Expand `WrittenFiles` rather than add a parallel directory field.** Hooks
  consume changed deploy paths regardless of type, and stage 9 can classify each
  source entry before checking the destination. A second field would duplicate
  aggregation, prefixing, diagnostics, and hook merging.
- **Record only descendant directories created by the current operation.** A
  pre-existing directory is a no-op. An ordinarily created target root and its
  missing ancestors are plumbing, not source-tree changes, and remain excluded.
- **Treat a managed file-to-directory root as evidence, not plumbing.** Replacing
  a previously managed file changes the target path's type, so the transitioned
  path and created descendants are observable even when the transition occurs at
  the top-level target.
- **Select the content fallback by entry type, not slice length.** Stage 9 checks
  existence, source type, and fresh mtime for every recorded path. If none of
  those paths is a regular-file write, it still compares every regular source
  file with the destination so directory-only evidence cannot mask a silent file
  sync failure.
- **Do not expand `DeployState.DeployedFiles`.** It remains the regular-file
  ownership manifest. A later change may define persisted directory ownership
  and the safety rules for directory-to-file replacement.
- **Keep non-authoritative modes on their existing fallbacks.** Standard-copy
  local deploys continue to use git diff when no per-path evidence exists;
  remote deploys continue to fire all hooks because SSH/tar provides no
  authoritative path-level result.

## Risks / Trade-offs

- Existing callers may read `WrittenFiles` as file-only. The implementation and
  documentation audit every producer, aggregator, invariant consumer, hook
  consumer, and persistence boundary before changing the contract.
- Directory mtimes can have coarse filesystem resolution. The invariant retains
  the existing reconcile-start comparison semantics and associated platform
  tests rather than introducing a second freshness definition.
- Excluding ordinary roots while including transitioned roots creates a narrow
  exception. Separate root-plumbing and top-level-transition scenarios make the
  distinction testable.

## Migration Plan

No persisted format changes. Archive `add-reconcile-fuse-hooks` before this
dependent change, then merge the implementation only after this proposal is
labeled `ready-to-build`. Reverting the implementation restores file-only
change tracking without state migration.

## Open Questions

None. Persisted empty-directory ownership is intentionally deferred.
