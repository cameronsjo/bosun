# Change: Distinguish Interrupted Reconcile Attempts

## Why

Bosun already delivers deploy-failure alerts through a bounded context after the
reconcile context is cancelled, but a daemon shutdown can still consume the
same per-commit attempt budget as a bad commit. Three otherwise healthy daemon
restarts during reconciliation can therefore trip the deploy circuit breaker,
and the persisted state does not distinguish that operator interruption from a
pipeline failure.

## What Changes

- Classify a reconcile result as interrupted only when the caller context is
  cancelled and the returned error is caused by `context.Canceled`.
- Persist a distinct `interrupted` last-attempt outcome while preserving the
  previous per-commit failure and alert-throttle budgets.
- Keep possibly partial deploy work retryable without counting it toward deploy
  recovery alerts or the three-failure circuit breaker.
- Deliver one existing-format deploy-failure alert for each interrupted run
  through a value-preserving, cancellation-detached context with a 30-second
  maximum budget.
- Continue treating reconcile deadlines and non-cancellation errors as ordinary
  failures that consume the circuit-breaker budget.
- Document the interruption state, retry, breaker, and alert behavior in the
  operator docs and onboard skill when implementation begins.

## Impact

- Affected specs: `reconcile`, `alerting`
- Expected implementation areas: `internal/reconcile` attempt state and alert
  finalization, deploy-state serialization/tests, daemon lifecycle integration
  tests, `docs/alerting.md`, and `skills/onboard/resources/gitops.md`
- Compatibility: the new deploy-state outcome field is optional and old state
  files continue to load with no interruption marker
- Related work: the cancellation-detached failure-alert transport landed in
  commit `70d9df436ef4253d764e2afaab06b6acd17195fc`; this change makes that behavior
  normative and defines the remaining state and breaker semantics from #242
- Adjacent active changes: `add-daemon-lifecycle` owns daemon task admission and
  shutdown cancellation, while `add-multi-target-reconcile` owns target
  orchestration and state paths. This change does not alter either topology; it
  applies the same per-run classification independently to every target state.
