## Context

The daemon's #701 drift implementation runs once per local target. It derives
an effective target config, uses that config's state file, and calls
`runRestartBreaker` with the target's effective Compose project. Named targets
with no explicit project name retain the existing `compose` namespace fallback.
An implicit default target may still have an empty project name. The read-only
drift collector intentionally treats an empty project as a broad comparison,
but the restart breaker is destructive and cannot reuse that scope.

## Goals / Non-Goals

- Goals: make an empty restart-breaker scope a no-op before Docker inspection;
  preserve restart tracking; retain independent target state and namespaces;
  make the guard apply to every `RunRestartBreaker` caller.
- Non-Goals: redesign target resolution; add a global project-name helper;
  derive a scope from a directory name or root file snapshot; change doctor or
  startup reporting; narrow read-only drift matching.

## Decisions

- Decision: add `projectName string` to `reconcile.RunRestartBreaker` and check
  it before `collectRestartCounts`. An empty value returns a result whose
  `Updated` map is the existing `state.RestartTracking` and performs no inspect
  or stop call.
- Decision: pass the scope from the existing per-target `runRestartBreaker`
  call. The daemon remains responsible for selecting the target config, state,
  and named-target `compose` fallback; the reconcile package enforces the
  destructive boundary.
- Decision: leave stale tracked entries unchanged when the scope is empty.
  Advancing them would rely on an unscoped host-wide sample that the guard has
  refused to trust.
- Decision: keep the guard warning at the cycle where it blocks action. This
  unit does not add the separate #668 startup/doctor posture redesign.

## Risks / Trade-offs

- An implicit default target with no project name no longer receives restart
  protection, but this is the fail-closed result required to avoid stopping
  unrelated containers. Operators can set `project_name` on that target or
  disable the breaker explicitly.
- The internal function signature changes all current callers and tests. The
  compiler will identify any missed consumer; the consumer inventory in the
  proposal lists the current set.

## Migration Plan

No persisted-state migration is needed. Existing restart tracking remains
readable. When scope is unresolved, the next check preserves it until a valid
target scope is available.

## Open Questions

None within the approved unit. Root-level project-name fallback and operator
posture reporting belong to a separate decision and are intentionally omitted.
