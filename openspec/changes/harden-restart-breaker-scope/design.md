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
  make the guard apply to every `RunRestartBreaker` caller; report the
  unresolved implicit-default posture once at daemon startup and through
  `bosun doctor`.
- Non-Goals: redesign target resolution; add a global project-name resolver;
  use the directory-name fallback as a destructive scope; change the existing
  per-target deployment namespace or named-target `compose` fallback; narrow
  read-only drift matching.

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
- Decision: keep the empty-scope cycle message at debug level so an unresolved
  target does not flood logs. A one-time startup warning and a doctor warning
  report the same condition with the fix (`project_name`) before an operator
  relies on restart-loop protection.
- Decision: retain the explicit root-level `project_name` from the local config
  only as diagnostic posture input. It does not populate `reconcile.Config` or
  override a target's effective deployment namespace; the existing per-target
  reload and state model remains authoritative.
- Decision (bosun#719): the root-level name does not suppress the startup or
  doctor warning. Both reports judge the target-level project name, the value
  the drift path passes to the breaker, so they cannot claim a bounded scope while the
  breaker skips every cycle. When a root-level name is present, the warning
  says it takes effect only after a reconcile loads it from the repository,
  and names the startup fix (`project_name` on a target named `default`, or
  `BOSUN_TARGETS`).

## Risks / Trade-offs

- An implicit default target with no project name no longer receives restart
  protection, but this is the fail-closed result required to avoid stopping
  unrelated containers. Startup and doctor report the condition; operators can
  set `project_name` on that target or disable the breaker explicitly.
- The internal function signature changes all current callers and tests. The
  compiler will identify any missed consumer; the consumer inventory in the
  proposal lists the current set.

## Migration Plan

No persisted-state migration is needed. Existing restart tracking remains
readable. When scope is unresolved, the next check preserves it until a valid
target scope is available.

## Open Questions

None within the approved unit. Root-level project-name fallback remains
diagnostic-only; it is not a new runtime scope resolver or deployment override.
