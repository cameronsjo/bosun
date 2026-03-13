## MODIFIED Requirements

### Requirement: Post-Deploy Verification

After `docker compose up` succeeds, the reconciler SHALL perform a health
verification by polling container health until all declared services are healthy
or a configurable timeout expires.

Health verification SHALL use a polling loop with configurable interval
(`HealthCheckInterval`, default 5 seconds) and timeout (`HealthCheckTimeout`,
default 60 seconds). Each iteration SHALL collect actual container state via
`CollectActualState` and compare against declared services.

A container SHALL be considered healthy when:
- Its Docker health status is "healthy", OR
- It has no `HEALTHCHECK` directive and its state is "running"

A container SHALL be considered unhealthy when:
- Its Docker health status is "unhealthy", OR
- Its state is "restarting", "exited", or "dead"

When all declared services are healthy, verification SHALL succeed immediately
(early exit) without waiting for the full timeout.

When the timeout expires with unhealthy containers remaining, verification SHALL
fail and return an error. The reconciliation SHALL treat this as a deployment
failure: a failure alert SHALL be sent, the circuit breaker attempt count SHALL
be incremented, but rollback SHALL NOT be attempted (the containers are running,
and rollback could make things worse).

When `HealthCheckTimeout` is zero (not configured) and `StartupGracePeriod` is
set, the reconciler SHALL fall back to the legacy behavior: wait the grace
period, perform a single-shot drift check, log warnings, but NOT fail the
reconciliation. This preserves backwards compatibility.

The health verification timeout and interval SHALL be configurable via
`BOSUN_HEALTH_CHECK_TIMEOUT` and `BOSUN_HEALTH_CHECK_INTERVAL` environment
variables. Both SHALL accept Go duration strings or plain seconds.

Post-deploy verification SHALL only run when a Docker client is available, dry
run is false, and declared services were extracted.

#### Scenario: All containers healthy before timeout

- **WHEN** compose up completes and `HealthCheckTimeout` is set to 60s
- **AND** all declared services become healthy within 15 seconds
- **THEN** the verification exits early at 15 seconds (not waiting the full 60s)
- **AND** the reconciler logs success with the count of declared services
- **AND** records zero drift items in the state file

#### Scenario: Container stays unhealthy until timeout

- **WHEN** compose up completes and `HealthCheckTimeout` is set to 60s
- **AND** container "norish" remains unhealthy after 60 seconds
- **THEN** the verification fails with an error listing the unhealthy containers
- **AND** the reconciliation is marked as a deployment failure
- **AND** a failure alert is sent

#### Scenario: Container without HEALTHCHECK treated as healthy

- **WHEN** compose up completes and container "redis" has no HEALTHCHECK
- **AND** container "redis" is in "running" state
- **THEN** "redis" is considered healthy immediately
- **AND** it does not block verification

#### Scenario: Rollback NOT attempted on health failure

- **WHEN** health verification times out with unhealthy containers
- **THEN** the reconciler does NOT attempt rollback
- **AND** the circuit breaker attempt count is incremented
- **AND** the failure is reported via alert

#### Scenario: Legacy behavior when HealthCheckTimeout is zero

- **WHEN** `HealthCheckTimeout` is not configured (zero)
- **AND** `StartupGracePeriod` is set to 30s
- **THEN** the reconciler waits 30 seconds, performs a single drift check
- **AND** drift is logged as a warning but the reconciliation succeeds

#### Scenario: Unhealthy containers trigger alert via daemon

- **WHEN** the daemon runs reconciliation and health verification fails
- **THEN** the failure alert includes the names of unhealthy containers
- **AND** the alert is subject to the existing throttle schedule

#### Scenario: Dry run skips verification

- **WHEN** `DryRun` is true
- **THEN** post-deploy health verification is skipped entirely

#### Scenario: Context cancelled during polling

- **WHEN** the reconciliation context is cancelled during health polling
- **THEN** the polling loop exits immediately
- **AND** verification is treated as failed
