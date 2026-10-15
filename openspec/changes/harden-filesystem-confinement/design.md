## Context

Bosun's local deploy path can write as host root below application-data paths
that a service container can modify. `os.Stat`/`os.Open`/`os.Rename` sequences
resolved by pathname leave check-to-use races across ancestors. The same issue
applies to source template reads and to a content-hash comparison that decides
to skip a write. The Go `os.Root` API gives a descriptor-backed namespace for
operations below a directory; its use must be scoped carefully because the root
itself still has to be opened and standard atomic swaps retain parent-rename
semantics.

## Goals / Non-Goals

- Goals: prevent an attacker-controlled ancestor from redirecting privileged
  local deploy mutations, skip decisions, template reads, stale removals, and
  rollback extraction outside their trusted roots; fail closed on insecure Unix
  lock ownership; preserve existing IncludeDir and deploy behavior.
- Non-goals: remote emergency/restore sites in #669, network authorization,
  SSH host trust, restart breaker policy, or a full concurrent harness for #671.

## Decisions

- Destination roots are opened once and all mkdir, temp-create, open, remove,
  rename, and directory-sync operations for that root use the handle. A path is
  converted to a relative name only after lexical containment validation.
- Content-hash and byte-compare reads use the same root namespace as the write;
  a failed or escaping comparison is an error, never a silent skip.
- Stale removals walk the managed set under the same destination root and remove
  relative names through that handle.
- Managed file/directory type transitions publish and quarantine entries with
  descriptor-relative no-replace renames on Unix and the native handle-bound
  Windows equivalent, preserving the private backup/rollback sequence while
  avoiding a path re-resolution after identity checks.
- Template source and include roots are opened once per render operation. The
  source descriptor is validated as regular before parsing/reading, and the
  include helper reads from the pinned IncludeDir namespace. IncludeDir still
  accepts the existing absolute override and infra-relative override.
- Rollback extraction uses one `os.Root` for the fresh temporary directory and
  validates link targets before creating them, then resolves every created
  link through that root after the stream ends, including hardlink aliases
  that retain a symlink inode after its original name is replaced. This catches
  sibling and forward link chains; confined dangling links remain allowed. Symlinked
  `.tmpl` files fail directory rendering with the path and the remedy to
  replace the link with a regular file, including links raced in after discovery.
  Standard atomic swap creates its
  temporary tree directly beside the target and uses the existing parent
  rename sequence; its guarantee is described precisely instead of claiming
  every mutation is root handle-pinned. Standard mode requires the target's
  parent and ancestors to remain trusted and non-adversarial while that sibling
  is selected and renamed.
- Unix lock setup traverses parent directories through trusted handles, creates
  missing components owner-only, requires root- or daemon-owned ancestors, and
  requires a daemon-owned `0700` final directory. Existing lock files are
  opened through that final handle, checked as daemon-owned regular files, and
  tightened to `0600`; any trust or permission failure returns an actionable
  path-specific error. Windows retains `LockFileEx` and its existing path
  behavior; the residual is documented.

## Risks / Trade-offs

- Descriptor-backed APIs are unavailable on older or unsupported platforms.
  Platform-specific fallbacks retain the current behavior and are explicitly
  documented rather than overstating guarantees.
- A trusted root can still contain an intentional in-root symlink target. The
  boundary is escape prevention; callers retain existing type checks and
  managed-set restrictions.
- Owner-only lock migration can reject deployments with an operator-supplied
  shared lock path. The actionable error directs the operator to make the
  parent and lock owner-only.

## Migration Plan

No data migration is required. On first acquisition, an existing Unix lock is
tightened to `0600`; an insecure or differently owned final parent, untrusted
ancestor, or symlinked path fails closed until the operator fixes ownership and
permissions. Windows behavior is unchanged.

## Open Questions

- The full racing/differential harness requested by #671 remains deferred unless
  a bounded deterministic harness can be added without introducing a new test
  runtime or flaky scheduling dependency.
