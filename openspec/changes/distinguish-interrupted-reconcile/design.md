## Context

The reconciler persists its attempt count before running stages so crashes and
ordinary failures cannot retry forever. Daemon shutdown cancels the same context
used by Git, backup, copy, compose, health, and hook operations. Those operations
correctly return cancellation, but the pre-recorded attempt currently remains in
the deploy-failure budget.

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
  - changing alert-provider message size, redaction, or fan-out policy.

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

### Apply classification once at the run boundary

Interruption finalization belongs at the reconcile run boundary, after stage
errors have been wrapped but before the result is returned. A central decision
keeps Git, backup, deploy, health, and hook cancellation consistent and avoids
adding subtly different cancellation branches at each failure site. Panic
recovery and errors that do not wrap caller cancellation retain their existing
failure accounting.

## Risks / Trade-offs

- A shutdown may happen after a live-target write completed. Keeping
  `needs_redeploy` set favors a safe idempotent retry over assuming the partial
  result was complete.
- An interruption alert may not finish before an external process manager kills
  Bosun after its own grace period. A detached but bounded send improves delivery
  without promising durability beyond process lifetime.
- A new optional state object adds serialization surface. Backward-compatible
  omission and round-trip tests limit upgrade and downgrade risk.
- Central finalization must not double-send the existing stage failure alert.
  Implementation tests must assert exactly one interruption alert.

## Migration Plan

1. Add the optional outcome state and backward-compatible serialization tests.
2. Add pure interruption classification and attempt-budget restoration helpers.
3. Route classified cancellations through one interruption finalizer and the
   bounded alert context.
4. Add daemon shutdown integration coverage and update operator/onboard docs.
5. Roll back by reverting the implementation; older binaries ignore the optional
   state field and existing `needs_redeploy` behavior remains safe.

## Open Questions

None. The change deliberately keeps deadlines and ambiguous cancellation races
on the existing failure path.
