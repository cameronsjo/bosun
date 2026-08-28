## MODIFIED Requirements

### Requirement: Circuit Breaker

The reconciler SHALL track consecutive failure count per commit in the state file.
After `MaxAttempts` (3) consecutive failures on the same commit, the reconciler
SHALL stop retrying and return a circuit breaker error.

Before executing the pipeline, the reconciler SHALL increment `attempt_count`
for the same commit or reset to 1 for a new commit.

When an attempt is classified as interrupted because the caller context is
cancelled and the returned pipeline error wraps `context.Canceled`, the
reconciler SHALL restore the failure count that existed before that attempt.
`context.DeadlineExceeded`, an independently returned cancellation error while
the caller context remains live, and a non-cancellation error that races with
caller cancellation SHALL retain normal failure accounting.

The circuit breaker SHALL be overridable via the `force` flag.

A circuit breaker activation SHALL always trigger a failure alert, regardless of
the alert throttling schedule.

#### Scenario: Bad commit triggers circuit breaker

- **WHEN** a commit causes the pipeline to fail 3 consecutive times
- **THEN** the reconciler stops retrying on subsequent triggers
- **AND** logs an ERROR with the failing commit and attempt count
- **AND** includes "use --force to override" in the error message

#### Scenario: New commit resets circuit breaker

- **WHEN** a new commit is pushed after a circuit breaker trip
- **THEN** the attempt count resets to 1
- **AND** the pipeline executes normally

#### Scenario: Force flag overrides circuit breaker

- **WHEN** a trigger with `force=true` is received while circuit breaker is tripped
- **THEN** the pipeline executes regardless of attempt count

#### Scenario: Shutdown interruption preserves prior failure budget

- **GIVEN** the current commit has one previously counted pipeline failure
- **WHEN** a later attempt returns an error wrapping `context.Canceled`
- **AND** the caller context is cancelled by daemon shutdown
- **THEN** the persisted attempt count remains 1
- **AND** the interruption does not move the commit closer to the circuit breaker

#### Scenario: Reconcile deadline remains a failure

- **WHEN** an attempt returns `context.DeadlineExceeded`
- **THEN** the attempt remains counted as a failure for the current commit
- **AND** repeated deadline failures can activate the circuit breaker

#### Scenario: Real error racing with shutdown remains a failure

- **WHEN** the caller context is cancelled during shutdown
- **AND** the pipeline returns an error that does not wrap `context.Canceled`
- **THEN** the attempt remains counted as a failure for the current commit

## ADDED Requirements

### Requirement: Interrupted Reconciliation Outcome

The reconciler SHALL classify an attempt as interrupted only when the caller
context reports `context.Canceled` and the returned pipeline error wraps
`context.Canceled`.

For a classified interruption, the reconciler SHALL persist an optional
`last_attempt_outcome` object containing the canonical outcome `interrupted`,
the affected commit when known, and an interruption timestamp. The object SHALL
not contain arbitrary pipeline error text. State files that omit the object
SHALL remain valid and SHALL mean that no interruption outcome is recorded.

The reconciler SHALL preserve the `last_attempted_commit`, `attempt_count`, and
`last_alerted_attempt` failure budgets that existed before the interrupted
attempt. It SHALL NOT clear an existing `needs_redeploy` marker during
interruption finalization, so a later trigger retries possibly partial deploy
work even when Git HEAD is unchanged. Cancellation before deploy mutation SHALL
NOT set `needs_redeploy` solely because the run was interrupted. A later attempt
that reaches a terminal success or ordinary failure SHALL clear or replace the
interruption outcome.

#### Scenario: Mid-deploy shutdown is persisted as interrupted

- **GIVEN** a reconcile has begun deploying the current commit
- **WHEN** daemon shutdown cancels the caller context
- **AND** deployment returns an error wrapping `context.Canceled`
- **THEN** `last_attempt_outcome.outcome` is persisted as `interrupted`
- **AND** the outcome identifies the current commit and interruption time
- **AND** `needs_redeploy` remains true
- **AND** a later trigger retries the pipeline even when Git HEAD is unchanged

#### Scenario: First interrupted run of a new commit consumes no failure budget

- **GIVEN** a commit has no counted pipeline failures
- **WHEN** its first attempt is classified as interrupted
- **THEN** its counted failure total remains zero
- **AND** the next ordinary failure on that commit is counted as failure 1

#### Scenario: Legacy state has no interruption outcome

- **WHEN** a state file written by an older Bosun version omits
  `last_attempt_outcome`
- **THEN** the state file loads successfully
- **AND** no interruption outcome is inferred

#### Scenario: Later completed attempt supersedes interruption outcome

- **GIVEN** state records an interrupted attempt
- **WHEN** a later attempt reaches an ordinary failure or success
- **THEN** the stale interrupted outcome is no longer reported as the last
  attempt outcome

#### Scenario: Best-effort hook cancellation is not a terminal interruption

- **GIVEN** a post-sync hook returns an error wrapping `context.Canceled`
- **AND** `runPostSyncHooksWithSpan` records and swallows that best-effort error
- **WHEN** the remaining pipeline completes successfully
- **THEN** the reconcile is not classified as interrupted on the basis of the
  swallowed hook error
- **AND** post-sync hook cancellation does not consume or restore the deploy
  failure budget

### Requirement: Non-Live Cycle Context Stops Target Iteration

Ordinary per-target failures SHALL retain the existing behavior of logging and
alerting the failure before proceeding to the next configured target while the
cycle context remains live. When the shared cycle context reports either
`context.Canceled` or `context.DeadlineExceeded`, the daemon SHALL stop the
reconciliation cycle before constructing or running any later target, regardless
of the in-flight target's returned error.

Only propagated caller cancellation SHALL receive interruption accounting. A
shared reconcile deadline that expires during a target SHALL remain an ordinary
counted failure with the active target's existing alert behavior, but the daemon
SHALL NOT pass the already-expired context to later targets.

Only an in-flight target that satisfies interruption classification SHALL
finalize an interrupted outcome and alert. State files and alert streams for
later targets SHALL remain unchanged. If a terminal cycle context is observed
between targets, the daemon SHALL stop before starting the next target without
creating an interrupted outcome, failure, or alert for a target that did not
begin.

#### Scenario: Shutdown during first target stops later targets

- **GIVEN** targets `unraid`, `pi`, and `nas` are configured in that order
- **AND** `unraid` is currently reconciling
- **WHEN** daemon shutdown cancels the cycle context
- **AND** `unraid` returns an error wrapping `context.Canceled`
- **THEN** `unraid` finalizes one interrupted outcome
- **AND** the daemon does not construct or run `pi` or `nas`
- **AND** the state files and alert streams for `pi` and `nas` remain unchanged

#### Scenario: Cancellation between targets invents no interrupted attempt

- **GIVEN** one target has completed and another target has not started
- **WHEN** daemon shutdown cancels the cycle context between those targets
- **THEN** target iteration stops before the next target begins
- **AND** no interruption outcome or alert is created for the untouched target

#### Scenario: Ordinary target failure still continues

- **WHEN** a target returns an error that does not satisfy interruption
  classification
- **AND** the cycle context remains live
- **THEN** the target failure retains its existing state and alert behavior
- **AND** reconciliation proceeds to the next configured target

#### Scenario: Real target error racing with shutdown stops iteration

- **WHEN** a target returns an error that does not wrap `context.Canceled`
- **AND** the cycle context reports `context.Canceled`
- **THEN** that target retains ordinary failure accounting and alert behavior
- **AND** reconciliation does not start the next configured target

#### Scenario: Shared reconcile deadline charges only the active target

- **GIVEN** multiple targets share one cycle-level reconcile deadline
- **WHEN** the deadline expires while one target is active
- **AND** that target returns `context.DeadlineExceeded`
- **THEN** the active target records an ordinary counted failure and retains its
  existing failure-alert behavior
- **AND** reconciliation does not start any later target
- **AND** later target state and alert streams remain unchanged
