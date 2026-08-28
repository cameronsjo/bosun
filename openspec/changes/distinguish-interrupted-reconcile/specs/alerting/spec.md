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
