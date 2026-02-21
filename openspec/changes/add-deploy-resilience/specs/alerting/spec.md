## ADDED Requirements

### Requirement: Alert on All Reconciliation Failure Paths

The alert system SHALL send a deployment failure notification for every reconciliation failure path, including:
- Secret decryption failure
- Template rendering failure
- Deployment failure (compose up, rsync, etc.)
- Circuit breaker activation

The circuit breaker path SHALL send a failure alert that includes the attempt count and commit hash.

#### Scenario: Circuit breaker triggers alert

- **GIVEN** a commit has failed 3 consecutive times
- **WHEN** the circuit breaker activates on the next reconciliation attempt
- **THEN** a deployment failure alert is sent with the attempt count and commit hash
- **AND** the alert indicates the circuit breaker is active

#### Scenario: Failure alert includes step context

- **WHEN** any reconciliation step fails
- **THEN** the failure alert includes which step failed (decrypt, template, deploy)
- **AND** the alert includes the error message

### Requirement: Alert Throttling for Repeated Failures

The alert system SHALL throttle repeated failure notifications to prevent notification spam.

Alerts SHALL be sent on attempt 1, 3, 10, 30, then every 30th attempt. Circuit breaker activation SHALL always trigger an alert regardless of throttle state.

The system SHALL track `LastAlertedAttempt` in the deploy state file to persist throttle state across daemon restarts.

#### Scenario: First failure always alerts

- **GIVEN** no previous failures on this commit
- **WHEN** the first reconciliation attempt fails
- **THEN** a failure alert is sent

#### Scenario: Intermediate failures are throttled

- **GIVEN** 5 consecutive failures on the same commit
- **WHEN** the 6th reconciliation attempt fails
- **THEN** no alert is sent (next alert at attempt 10)

### Requirement: Recovery Alert After Failure Streak

The alert system SHALL send a recovery notification when a deployment succeeds after one or more consecutive failures.

The recovery alert SHALL include the number of failed attempts before recovery.

#### Scenario: Recovery after failures

- **GIVEN** 3 consecutive failures on a commit
- **WHEN** a subsequent deployment succeeds (new commit or force)
- **THEN** a recovery alert is sent indicating 3 prior failures

### Requirement: Unhealthy Container Alert

The alert system SHALL send a warning-severity notification when post-deploy health inspection discovers unhealthy containers.

The alert SHALL list the names of unhealthy containers.

#### Scenario: Unhealthy containers detected post-deploy

- **GIVEN** a successful deployment
- **WHEN** post-deploy health inspection finds containers `obsidian` and `vaultwarden` are unhealthy
- **THEN** a warning alert is sent listing both container names
