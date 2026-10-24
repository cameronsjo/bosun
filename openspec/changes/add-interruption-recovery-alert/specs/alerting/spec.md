## ADDED Requirements

### Requirement: Interruption Alert Retraction

When an interrupted reconcile has sent an interruption alert, the first later run that ends without error SHALL send one recovery alert for that target, gated on `on_recovery`. The persisted interrupted outcome is the retraction debt. It SHALL be cleared only after the recovery alert was dispatched or `on_recovery` is disabled, and SHALL be kept when every provider failed so a later clean run retries. A failed run SHALL keep the interrupted outcome; only a clean run settles it. The recovery text SHALL say the deployment recovered after an interrupted deploy, not report a failure count. A run that already dispatched a recovery alert SHALL NOT send a second one for the interruption. Interruptions SHALL NOT change `LastAlertedAttempt`.

#### Scenario: Interruption followed by a clean run

- **GIVEN** a run was interrupted and the interruption alert was sent
- **WHEN** a later run for the same target ends without error
- **THEN** exactly one recovery alert is sent
- **AND** the interrupted outcome is cleared

#### Scenario: Repeated clean runs

- **GIVEN** the recovery for an interruption was already sent
- **WHEN** further runs end without error
- **THEN** no further recovery alert is sent

#### Scenario: Recovery alerts disabled

- **GIVEN** `on_recovery` is false
- **WHEN** a clean run follows an interruption
- **THEN** no recovery alert is sent
- **AND** the interrupted outcome is cleared

#### Scenario: Recovery delivery fails

- **GIVEN** every provider fails the recovery alert
- **WHEN** a clean run follows an interruption
- **THEN** the interrupted outcome is kept
- **AND** the next clean run sends the recovery alert again

#### Scenario: Failed run between interruption and recovery

- **GIVEN** a run was interrupted and the interruption alert was sent
- **WHEN** a later run fails and a run after it ends without error
- **THEN** the failed run sends no recovery alert and keeps the interrupted outcome
- **AND** the clean run sends exactly one recovery alert
