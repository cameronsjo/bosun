## ADDED Requirements

### Requirement: Health-Gate Rollback Restores Prior State

When the critical-container health gate fails, the reconciler SHALL restore the backed-up prior compose configuration by re-deploying the backup compose files, and SHALL NOT re-run `docker compose up` against the new (just-deployed, now-unhealthy) rendered output.

The health-gate rollback path SHALL be distinct from the deploy-failure rollback helper (`ComposeUpMultipleWithRollback`). The deploy-failure helper runs the caller-supplied compose files first and only falls back to the backup on error; reusing it for the health-gate case is incorrect because the already-running-but-unhealthy containers cause the initial `up` to exit 0, so the backup is never consulted. The health-gate path SHALL instead deploy the backup files directly (the "restore from backup" pattern used by `ComposeUpIsolated`).

The restore SHALL operate on the recorded backup path (`r.lastBackupPath`), not on `r.lastComposeFiles` (the new render). The restore SHALL NOT short-circuit on the basis that the new containers already exist and are merely reporting unhealthy.

#### Scenario: Health-gate failure redeploys backup files

- **WHEN** a critical container is unhealthy after the health gate timeout
- **AND** a backup exists at `r.lastBackupPath` with the previous-good compose files
- **THEN** the reconciler re-deploys the backup compose files
- **AND** does NOT re-run compose up against the new rendered output (`r.lastComposeFiles`)
- **AND** the deployment is NOT recorded as successful

#### Scenario: Already-running unhealthy containers do not short-circuit rollback

- **WHEN** the new containers already exist and report "unhealthy"
- **THEN** rollback still re-deploys the backup files
- **AND** does not treat the existing-but-unhealthy containers as a successful no-op

### Requirement: Bounded Post-Deploy Health Polling

Post-deploy health verification polling SHALL terminate at the configured timeout and on context cancellation regardless of whether the Docker API call succeeds, and SHALL NOT loop indefinitely when the Docker API returns errors.

The polling loop SHALL evaluate its deadline on every iteration, not only when the underlying state-collection call (`CollectActualState`) succeeds. The wait loop SHALL carry a deadline/timeout case in addition to the context-cancellation and interval-tick cases.

The reconciler SHALL distinguish "container unhealthy" from "cannot query health". A container reporting unhealthy SHALL keep being polled until the timeout elapses. A Docker API error (daemon restart, socket permission, network blip) SHALL be bounded by the same timeout: polling SHALL continue across transient errors but SHALL return a failed `HealthCheckResult` once the deadline is reached, rather than blocking forever.

The bound SHALL be the configured health verification timeout (`HealthCheckTimeout`, default 60 seconds), with the configured poll interval (`HealthCheckInterval`, default 5 seconds).

#### Scenario: Persistent Docker API errors terminate at the deadline

- **WHEN** `CollectActualState` returns an error on every poll iteration for the entire `HealthCheckTimeout` window
- **THEN** polling logs a warning per failed query
- **AND** returns a failed `HealthCheckResult` once the deadline is reached
- **AND** does not loop indefinitely

#### Scenario: Context cancellation stops polling

- **WHEN** the context is cancelled mid-poll
- **THEN** polling returns promptly without waiting for the next interval tick

#### Scenario: Unhealthy container keeps polling within the timeout

- **WHEN** a container reports "unhealthy" but the deadline has not yet elapsed
- **THEN** polling continues at the configured interval
- **AND** returns as soon as the container becomes healthy or the deadline is reached

### Requirement: Rollback Failure Is Surfaced Distinctly

A rollback attempted after a failed health gate SHALL report its outcome distinctly: a successful restore and a failed restore MUST be distinguishable, and a failed rollback MUST NOT be swallowed or logged as a generic deploy success.

The health-gate rollback path SHALL preserve the existing sentinel contract: `ErrRollbackSucceeded` when the backup was restored successfully and `ErrRollbackFailed` when the restore itself failed (critical state). A failed rollback SHALL be logged at ERROR level indicating a critical state. A successful rollback SHALL NOT cause the deployment to be recorded as successful.

#### Scenario: Failed rollback surfaces critical state

- **WHEN** the health gate fails and the backup restore also fails
- **THEN** the reconciler returns `ErrRollbackFailed`
- **AND** logs at ERROR level indicating a critical state
- **AND** the deployment is NOT recorded as successful

#### Scenario: Successful rollback is reported, not masked as success

- **WHEN** the health gate fails and the backup restore succeeds
- **THEN** the reconciler returns `ErrRollbackSucceeded`
- **AND** the deployment is NOT recorded as successful
- **AND** the outcome is not logged as a generic deploy success
