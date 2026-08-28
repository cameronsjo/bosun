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
