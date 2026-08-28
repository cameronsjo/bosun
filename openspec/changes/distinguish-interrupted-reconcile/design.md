## Context

The reconciler persists its attempt count before running stages so crashes and
ordinary failures cannot retry forever. Daemon shutdown cancels the same context
used by Git, backup, copy, compose, and health operations. Those terminal stages
can return cancellation, but the pre-recorded attempt currently remains in the
deploy-failure budget. Post-sync hook errors are different: their wrapper records
telemetry and intentionally does not return the error to the pipeline.

Failure alerts no longer inherit a cancelled transport context: commit
`70d9df436ef4253d764e2afaab06b6acd17195fc` added a value-preserving 30-second
delivery context. Attempt classification still needs a durable state-machine
decision so shutdowns cannot create phantom circuit-breaker failures.

## Goals / Non-Goals

- Goals:
  - distinguish caller cancellation from a commit-caused pipeline failure;
  - preserve all real failure history across an interrupted run;
  - leave interrupted work retryable and operator-visible;
  - keep alert delivery bounded during process shutdown;
  - define deterministic behavior for cancellation races and timeouts.
- Non-Goals:
  - changing daemon shutdown admission, goroutine ownership, or wait budgets;
  - making reconciliation transactional or undoing writes completed before
    cancellation;
  - exempting reconcile timeouts, provider failures, or independently generated
    `context.Canceled` errors from the circuit breaker;
  - changing alert-provider message size, redaction, or fan-out policy;
  - changing best-effort post-sync hook errors or cancellation into terminal
    reconcile results.

## Decisions

### Classify only propagated caller cancellation as interruption

An attempt is interrupted only when both the run context reports
`context.Canceled` and the returned pipeline error wraps `context.Canceled`.
Checking both signals prevents a shutdown that races with a real pipeline error
from laundering that error into an interruption. `context.DeadlineExceeded`
continues to count as a failure because reconcile deadlines bound wedged or
commit-dependent work and need breaker protection.

### Restore the pre-attempt failure budget

Before attempt tracking mutates state, the reconciler retains the current
`attempt_count`, `last_attempted_commit`, and `last_alerted_attempt`. On an
interruption it restores the prior failure and throttle budgets. The interrupted
commit remains identifiable through the last-attempt outcome record, and
an existing `needs_redeploy` marker is not cleared, so partial deploy effects are
retried even when Git HEAD has not changed. Cancellation before deploy mutation
does not create that marker solely because the run was interrupted.

This preserves earlier real failures on the same commit. It also means an
interrupted first run of a new commit leaves that commit with zero counted
failures rather than inheriting the previous commit's streak.

### Persist a bounded outcome marker, not arbitrary error text

State records an optional `last_attempt_outcome` object with outcome
`interrupted`, the affected commit when known, and the interruption timestamp.
The value is absent in legacy state, is replaced or cleared when a later attempt
reaches a real success or failure, and does not change the state schema version.
Persisting a canonical outcome avoids writing arbitrary command or provider
errors—which can contain sensitive values—while still making the prior shutdown
visible to the next operator.

### Alert interruptions independently from failure throttling

Every classified interruption sends one deploy-failure lifecycle alert when
failure alerts are enabled. Its reason identifies the run as interrupted. The
send bypasses the repeated-failure attempt schedule and does not change
`last_alerted_attempt`, because the run did not consume a failure attempt.

Delivery preserves logging and reconcile-ID values from the caller context but
detaches cancellation and applies a maximum 30-second timeout. Provider errors
remain best-effort: they are logged, the interruption outcome remains persisted,
and shutdown is not extended beyond the delivery budget.

### Give the run-boundary finalizer exclusive interruption-alert ownership

Every stage-specific alert path receives or retains the causal stage error long
enough to run the same interruption classifier. When that classifier matches,
the stage returns its wrapped cancellation without calling either
`sendThrottledFailureAlert` or `sendGateFailureAlerts`. This includes both
critical and declared health-gate paths; declared-scope rollback companion
notifications are suppressed with the gate failure because the finalizer owns
the only notification for that interrupted run.

The run-boundary finalizer first restores and persists interruption state, then
attempts the one cancellation-detached alert. Ordinary errors, including a real
stage error that races with shutdown, retain their existing stage alert and do
not receive a second finalizer alert. Explicit ownership removes double-send
races instead of trying to deduplicate after provider I/O.

### Apply classification once at the run boundary

Interruption finalization belongs at the reconcile run boundary, after stage
errors have been wrapped but before the result is returned. A central decision
keeps Git, backup, deploy, and health cancellation consistent and avoids
adding subtly different cancellation branches at each failure site. Panic
recovery and errors that do not wrap caller cancellation retain their existing
failure accounting.

Post-sync hook errors and cancellation are explicitly outside this
classification. `runPostSyncHooksWithSpan` records them on its span and returns
no error, so they cannot become the run's terminal result. This proposal does
not make best-effort hooks fail or interrupt an otherwise completed reconcile.

### Stop multi-target iteration when the cycle context is cancelled

Ordinary target failures keep the existing continue-to-next-target behavior.
Once the cycle context reports `context.Canceled`, however, the daemon stops
before constructing or running any later target. If the in-flight target also
returns propagated caller cancellation, only that target restores its attempt
budget, persists an interrupted outcome, and attempts the bounded alert. If a
real target error races with cancellation, it retains ordinary failure state and
alert ownership, but target iteration still stops because the cycle context is
no longer live. Later target state and alert streams remain untouched.

The daemon also checks for caller cancellation between targets. Cancellation in
that gap stops iteration without inventing an interrupted target attempt or
alert. This bounds one shutdown cycle to at most one 30-second interruption
alert delivery budget. Reconcile deadlines remain ordinary failures for the
active target under this proposal; the shutdown-specific cycle stop is keyed to
`context.Canceled`.

## Risks / Trade-offs

- A shutdown may happen after a live-target write completed. Keeping
  `needs_redeploy` set favors a safe idempotent retry over assuming the partial
  result was complete.
- An interruption alert may not finish before an external process manager kills
  Bosun after its own grace period. A detached but bounded send improves delivery
  without promising durability beyond process lifetime.
- A new optional state object adds serialization surface. Backward-compatible
  omission and round-trip tests limit upgrade and downgrade risk.
- Suppressing declared health-gate failure and rollback companion alerts during
  propagated cancellation trades stage-specific detail for one unambiguous
  interruption alert; rollback results remain logged.
- Stopping a multi-target cycle on shutdown is a narrow exception to ordinary
  target-failure continuation and prevents later targets from consuming state or
  delivery budgets after the cycle has no live context.

## Migration Plan

1. Add the optional outcome state and backward-compatible serialization tests.
2. Add pure interruption classification and attempt-budget restoration helpers.
3. Suppress stage-owned alerts for classified cancellation and route the result
   through one interruption finalizer and bounded alert context.
4. Stop multi-target iteration whenever the cycle context reports caller
   cancellation.
5. Add daemon shutdown integration coverage and update operator/onboard docs.
6. Roll back by reverting the implementation; older binaries ignore the optional
   state field and existing `needs_redeploy` behavior remains safe.

## Open Questions

None. The change deliberately keeps deadlines and ambiguous cancellation races
on the existing failure path.
