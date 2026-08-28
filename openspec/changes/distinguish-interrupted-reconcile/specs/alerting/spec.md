## ADDED Requirements

### Requirement: Cancellation-Resilient Reconcile Alert Delivery

When a reconcile result is classified as interrupted, the alert system SHALL
attempt exactly one deploy-failure lifecycle alert when reconciliation failure
alerts are enabled. The alert reason SHALL identify the result as interrupted.

Interruption alert delivery SHALL preserve values from the caller context while
detaching caller cancellation and enforcing a maximum delivery budget of 30
seconds. It SHALL bypass repeated-failure attempt throttling and SHALL NOT update
`last_alerted_attempt`, because an interruption does not consume the deploy
circuit-breaker failure budget.

The run-boundary interruption finalizer SHALL exclusively own this alert. A
stage-specific alert path that receives an error wrapping `context.Canceled`
while the caller context reports `context.Canceled` SHALL return the causal error
without calling its ordinary `sendThrottledFailureAlert` or
`sendGateFailureAlerts` path. For a declared-scope health gate, suppression SHALL
include the companion rollback notification; rollback results SHALL remain
logged. A stage error that does not satisfy both classifier conditions SHALL
retain its existing stage-owned alert behavior and SHALL NOT receive a finalizer
interruption alert.

Provider delivery failure SHALL be logged and SHALL NOT erase the persisted
interruption outcome or extend shutdown beyond the delivery budget.

#### Scenario: Cancelled deploy still attempts an alert

- **GIVEN** reconciliation failure alerts are enabled
- **WHEN** a mid-deploy shutdown is classified as interrupted
- **THEN** exactly one deploy-failure lifecycle alert is attempted
- **AND** its reason identifies the reconcile as interrupted
- **AND** the alert context is not cancelled with the caller context
- **AND** the alert context retains the reconcile correlation value
- **AND** the alert context has a deadline no later than 30 seconds after
  interruption handling begins

#### Scenario: Interruption does not consume failure alert throttle

- **GIVEN** a commit has existing failure-attempt and alert-throttle state
- **WHEN** a later run of that commit is classified as interrupted
- **THEN** an interruption alert is attempted regardless of the next scheduled
  repeated-failure alert
- **AND** `last_alerted_attempt` remains unchanged

#### Scenario: Stage alert is suppressed in favor of finalizer ownership

- **GIVEN** a pipeline stage normally sends a throttled deploy-failure alert
- **WHEN** its causal error wraps `context.Canceled`
- **AND** the caller context reports `context.Canceled`
- **THEN** the stage-specific path attempts no alert
- **AND** the run-boundary finalizer attempts exactly one interruption alert

#### Scenario: Cancelled declared health gate sends no companion alerts

- **GIVEN** the declared health gate normally sends deploy-failure and rollback
  companion notifications
- **WHEN** its gate error and caller context satisfy interruption classification
- **THEN** `sendGateFailureAlerts` is not called
- **AND** no rollback companion notification is attempted
- **AND** the run-boundary finalizer attempts exactly one interruption alert

#### Scenario: Real stage error racing with shutdown keeps stage ownership

- **WHEN** the caller context reports `context.Canceled`
- **AND** a stage returns an error that does not wrap `context.Canceled`
- **THEN** the existing stage-specific alert behavior is retained
- **AND** the run-boundary finalizer attempts no interruption alert

#### Scenario: Disabled failure alerts suppress interruption delivery

- **GIVEN** reconciliation failure alerts are disabled
- **WHEN** a run is classified as interrupted
- **THEN** no interruption alert is attempted
- **AND** the interrupted outcome is still persisted

#### Scenario: Provider failure preserves interruption state

- **WHEN** every configured provider fails to deliver an interruption alert
- **THEN** the provider failure is logged
- **AND** the interrupted outcome remains persisted
- **AND** delivery returns no later than the 30-second budget

#### Scenario: Multi-target shutdown has one aggregate alert budget

- **GIVEN** a reconciliation cycle has multiple targets
- **WHEN** shutdown interrupts the in-flight target
- **THEN** exactly one interruption alert is attempted for that target
- **AND** no later target attempts an interruption alert
- **AND** aggregate interruption-alert delivery for the cycle is bounded by 30
  seconds
