# Change: Bound restart-breaker destructive actions to a resolved target scope

## Why

The #701 per-target daemon drift path can still pass an empty Compose project
name to the restart breaker for an implicit default target. Docker's existing
empty-project matching treats that as host-wide, so a restart loop in an
unrelated project could be inspected and stopped by Bosun.

## What Changes

- Pass each target's already-resolved Compose project name into the restart
  breaker action boundary.
- Fail closed before Docker inspection when that scope is empty, preserving
  restart tracking unchanged and stopping no container.
- Preserve #701's target snapshots, per-target config and state files, project
  reload behavior, and the named-target blank-project `compose` fallback.
- Add regression coverage for foreign containers, independent target state,
  and an unresolved implicit target.
- Warn once at daemon startup and from `bosun doctor` when the enabled
  implicit default target has no `project_name`; demote the per-cycle message
  to debug to avoid repeated warnings.
- Document the bounded per-target semantics for operators and onboarded agents.

The #668 global project resolver and unrelated security-hardening changes
remain out of scope. The explicit root-level `project_name` is retained only
for startup/doctor posture reporting and never replaces the existing
per-target runtime scope.

## Impact

- Affected specs: `reconcile` restart-breaker requirements.
- Affected code: `internal/reconcile/restart_breaker.go`,
  `internal/daemon/daemon.go`, `internal/config/config.go`,
  `internal/cmd/diagnostics_doctor.go`, and their tests.
- Consumer documentation: `docs/gitops.md` and
  `skills/onboard/resources/gitops.md`.
- All consumers: `Daemon.runDriftCheckForTarget` supplies the target-specific
  scope; `Daemon.runRestartBreaker` forwards it; `reconcile.RunRestartBreaker`
  enforces the destructive-action guard; restart-breaker tests call the
  internal function directly and must pass the explicit scope.
