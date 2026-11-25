## ADDED Requirements

### Requirement: Dry Run Persists No Deploy State

A dry run SHALL NOT create, modify, or delete the deploy state file. Decisions during the run, including skip, circuit breaker, and alert throttle, SHALL use the state as loaded at the start of the run.

#### Scenario: Successful dry run leaves state untouched

- **GIVEN** a state file recording a deployed commit
- **WHEN** a dry run completes on a newer commit
- **THEN** the state file is byte-identical to its content before the run

#### Scenario: Failed or interrupted dry run leaves state untouched

- **WHEN** a dry run fails at any pipeline stage, or is cancelled by its caller
- **THEN** the state file is byte-identical to its content before the run
- **AND** no attempt, alert throttle, or interruption marker is recorded

#### Scenario: Dry run without a state file creates none

- **GIVEN** no state file exists
- **WHEN** a dry run completes
- **THEN** no state file exists afterwards

#### Scenario: Repeated dry runs repeat the pipeline

- **WHEN** two dry runs execute against the same unchanged state
- **THEN** each runs the full pipeline instead of skipping as already deployed

### Requirement: Dry Run Withholds Recovery Alerts

A dry run SHALL NOT send a failure-recovery or interruption-recovery alert, because it cannot record that the owed retraction was delivered. It SHALL keep sending failure, success, and interruption alerts under their existing gates, and `--no-alerts` SHALL still suppress every alert. This narrows the unchanged-alert-behavior sentence of Standalone Reconcile Alert Suppression.

#### Scenario: Clean dry run after an alerted failure sends no recovery

- **GIVEN** a state file recording an alerted failure
- **WHEN** a dry run completes cleanly
- **THEN** no recovery alert is sent
- **AND** the state file is byte-identical to its content before the run

#### Scenario: Failing dry run still sends the failure alert

- **WHEN** a dry run fails and failure alerts are enabled
- **THEN** a failure alert is sent as for a real run

### Requirement: Dry-Run Daemon Takes No Periodic Action

Notwithstanding Periodic Drift Checks and Drift Status Persistence, a daemon in dry run mode SHALL observe drift without side effects. Its periodic drift check SHALL NOT save the state file, run the restart breaker, stop a container, consume a self-heal attempt, trigger a reconciliation, or send a drift alert.

#### Scenario: Dry-run drift check leaves state untouched

- **GIVEN** a dry-run daemon and a state file with declared services
- **WHEN** the periodic drift check finds drift
- **THEN** the state file is byte-identical to its content before the check
- **AND** the drift is logged and visible in the daemon's runtime status

#### Scenario: Dry-run daemon never stops a restart-looping container

- **GIVEN** a dry-run daemon with the restart breaker enabled
- **WHEN** a managed container exceeds the restart threshold
- **THEN** no container is stopped
- **AND** no restart-breaker alert is sent

#### Scenario: A daemon outside dry run mode is unchanged

- **GIVEN** a daemon not in dry run mode
- **WHEN** the periodic drift check runs
- **THEN** state, alerts, restart breaker, and self-heal behave as before this change
