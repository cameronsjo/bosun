# Change: Harden filesystem confinement and reconcile locks

## Why

Bosun deploys rendered configuration as a privileged process into directories
that may be writable by a container. A pathname checked before a later write,
hash comparison, stale-file removal, template include read, or extraction write
can be redirected by replacing an ancestor or final component. Reconcile locks
also need owner-only Unix permissions so an unrelated local user cannot open the
lock and block deployment.

## What Changes

- Pin local destination mutations and destination comparisons to a trusted root
  handle, including content-hash skip decisions and stale-file pruning.
- Preserve standard atomic-swap mode's trusted sibling temporary and parent
  rename semantics, while documenting its narrower path-based boundary.
- Bind template source and allowlisted include/fromJsonFile reads to a pinned
  source root and the descriptor used for validation.
- Extract rollback archives beneath a pinned temporary root and reject chained
  symlink or hardlink escapes.
- Create and acquire Unix reconcile locks owner-only, fail closed when trust or
  permission tightening cannot be established, and report an actionable path
  and fix. Keep current Windows locking behavior and document its residual.
- Add deterministic tests for swapped ancestors, special files, skip-path
  comparison, stale removal, template races, extraction links, and lock modes.

## Impact

- Affected specs: `reconcile`.
- Affected code: `internal/fileutil`, `internal/reconcile/deploy.go`,
  `internal/reconcile/template.go`, `internal/reconcile/remote_rollback.go`,
  `internal/reconcile/reconcile.go`, and platform lock files.
- All consumers: `CopyFile`, directory copy and hash helpers; local deploy and
  stale pruning; template render and custom template functions; remote rollback
  extraction; reconcile lock setup and acquisition; onboard/security docs.
- Excluded: remote emergency/restore writes tracked by #669, ingress/SSH/
  restart hardening, and #671's concurrent differential harnesses unless a
  deterministic test can be committed in this unit.
