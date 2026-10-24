# Change: Retract an interruption alert when a later run recovers

## Why

A caller-cancelled reconcile sends a "Deployment Failed ... reconciliation
interrupted by caller cancellation" alert. No recovery notice ever follows it.
Retraction is keyed on `DeployState.LastAlertedAttempt`, and an interruption
deliberately leaves that field untouched (it consumes no failure budget). The
operator stays told that production is broken after it has recovered.

Two concerns share one field: failure-alert throttling and retraction debt. An
interruption must not burn the throttle budget, but it does owe a retraction.

## What Changes

- The run-boundary finalizer sends one recovery alert when a clean run clears a
  persisted `interrupted` outcome, gated on `on_recovery`.
- The persisted `last_attempt_outcome` marker is the retraction debt for
  interruptions. It is cleared only after the recovery was dispatched or the
  gate is off; a delivery failure keeps it so the next clean run retries.
- A run that already retracted a throttled failure alert does not send a second
  recovery for the same run.
- No new state field and no change to throttle semantics.

## Impact

- Affected specs: `alerting`
- Affected code: `internal/reconcile/interruption.go`, `alerts.go`
