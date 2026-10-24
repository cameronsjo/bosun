## MODIFIED Requirements

### Requirement: Interrupted Reconciliation Outcome

The reconciler SHALL classify an attempt as interrupted only when the caller
context reports `context.Canceled` and the returned pipeline error wraps
`context.Canceled`.

For a classified interruption, the reconciler SHALL persist an optional
`last_attempt_outcome` object containing the canonical outcome `interrupted`,
the affected commit when known, and an interruption timestamp, and an `alerted` flag set only after the interruption alert was delivered. The object SHALL
not contain arbitrary pipeline error text. State files that omit the object
SHALL remain valid and SHALL mean that no interruption outcome is recorded.

The reconciler SHALL preserve the `last_attempted_commit`, `attempt_count`, and
`last_alerted_attempt` failure budgets that existed before the interrupted
attempt. It SHALL NOT clear an existing `needs_redeploy` marker during
interruption finalization, so a later trigger retries possibly partial deploy
work even when Git HEAD is unchanged. Cancellation before deploy mutation SHALL
NOT set `needs_redeploy` solely because the run was interrupted. A later attempt
that reaches a terminal success SHALL clear the interruption outcome, after the
recovery alert it owes has been dispatched or disabled. A later ordinary failure
SHALL leave the interruption outcome in place, so the recovery is still owed when
a later attempt succeeds. A later interruption replaces the outcome.

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
- **WHEN** a later attempt reaches terminal success
- **THEN** the stale interrupted outcome is no longer reported as the last
  attempt outcome

#### Scenario: Later ordinary failure keeps the interruption outcome

- **GIVEN** state records an interrupted attempt
- **WHEN** a later attempt reaches an ordinary failure
- **THEN** the interrupted outcome remains persisted

#### Scenario: Best-effort hook cancellation is not a terminal interruption

- **GIVEN** a post-sync hook returns an error wrapping `context.Canceled`
- **AND** `runPostSyncHooksWithSpan` records and swallows that best-effort error
- **WHEN** the remaining pipeline completes successfully
- **THEN** the reconcile is not classified as interrupted on the basis of the
  swallowed hook error
- **AND** post-sync hook cancellation does not consume or restore the deploy
  failure budget

