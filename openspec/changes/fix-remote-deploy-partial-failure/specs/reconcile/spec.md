## MODIFIED Requirements

### Requirement: Pipeline Orchestration

The reconciler SHALL execute stages in this fixed order:

1. Acquire lock
2. Git repository sync
3. Load deploy state and evaluate skip/circuit-breaker logic
4. Decrypt secrets (SOPS)
5. Render templates (Go text/template + Sprig)
6. Extract declared state from rendered compose
7. Create configuration backup
8. Deploy files (local or remote)
9. Run `docker compose up`
10. Clean up staging directory
11. Record successful deployment in state file
12. Execute post-sync hooks
13. Post-deploy verification (drift check)
14. Release lock

A failure at any stage SHALL abort the remaining stages and release the lock.
The lock SHALL always be released via defer, even on panic.

For remote deployments, a `docker compose up` failure after successful config
sync SHALL be treated as a pipeline failure. The reconciler SHALL NOT record
the deployment as successful when service orchestration fails, regardless of
whether file deployment succeeded.

#### Scenario: Full pipeline succeeds

- **WHEN** a reconciliation is triggered and a new commit is available
- **THEN** all stages execute in order
- **AND** the deploy state file records the deployed commit
- **AND** a success alert is sent

#### Scenario: Pipeline aborts on stage failure

- **WHEN** secret decryption fails
- **THEN** template rendering, backup, deploy, and compose stages are skipped
- **AND** a throttled failure alert is sent
- **AND** the lock is released

#### Scenario: Dry run mode

- **WHEN** `DryRun` is true
- **THEN** backup, deploy, compose up, post-sync hooks, and post-deploy verification are skipped
- **AND** template rendering still executes to validate templates

#### Scenario: Remote compose up failure aborts pipeline

- **WHEN** config files are successfully synced to a remote host via SSH
- **AND** `ComposeUpRemote` fails (SSH error, compose error, timeout)
- **THEN** the reconciler returns an error from the deploy stage
- **AND** `LastDeployedCommit` is NOT updated in the state file
- **AND** a throttled failure alert is sent
- **AND** the circuit breaker tracks the failed attempt

#### Scenario: Remote compose up failure triggers retry on next reconcile

- **WHEN** a remote deploy fails during compose up (config sync succeeded)
- **AND** the next reconcile cycle runs with the same git HEAD
- **THEN** the reconciler detects that `LastDeployedCommit` does not match current HEAD
- **AND** the full pipeline re-executes (including config sync and compose up)

### Requirement: Service Orchestration

The reconciler SHALL run `docker compose up -d --remove-orphans` with a
configured project name to bring services to their declared state.

The `--wait` flag SHALL NOT be used, because it exits non-zero when any container
is unhealthy, including pre-existing unhealthy containers unrelated to the current
deployment. Health inspection is handled separately by post-deploy verification.

Multiple compose files SHALL be supported via multiple `-f` flags.

On compose up failure, the reconciler SHALL attempt rollback using the backup
compose files. Rollback results are distinguished via sentinel errors:
`ErrRollbackSucceeded` (deploy failed, rollback worked) and `ErrRollbackFailed`
(both failed, critical state).

A configurable `ComposeUpTimeout` (default 10 minutes) SHALL apply to compose
operations.

For remote deployments, compose up failures SHALL be propagated as errors to the
reconciliation pipeline. The `deployRemote()` method SHALL NOT swallow
`ComposeUpRemote` errors as warnings. Best-effort operations (e.g., SIGHUP
signals to containers) SHALL remain warnings and SHALL NOT cause the pipeline
to fail.

#### Scenario: Compose up with project name

- **WHEN** compose up runs with `ProjectName` set to "bosun"
- **THEN** the command includes `-p bosun`
- **AND** `--remove-orphans` cleans up containers from removed services

#### Scenario: Deploy succeeds despite pre-existing unhealthy container

- **WHEN** container A is healthy and container B was already unhealthy
- **THEN** compose up exits 0 (no `--wait` flag)
- **AND** post-deploy verification logs a warning for container B
- **AND** the deployment is marked successful

#### Scenario: Compose up fails with rollback

- **WHEN** compose up fails
- **AND** a backup exists with previous compose files
- **THEN** the reconciler runs compose up with the backup files
- **AND** returns `ErrRollbackSucceeded` if rollback works

#### Scenario: Both compose up and rollback fail

- **WHEN** compose up fails and rollback also fails
- **THEN** the reconciler returns `ErrRollbackFailed`
- **AND** logs at ERROR level indicating a critical state

#### Scenario: Remote compose up failure propagated

- **WHEN** `ComposeUpRemote` fails on a remote host
- **THEN** `deployRemote()` returns the error (not nil)
- **AND** the reconciler treats it as a deploy failure
- **AND** state file records the attempt but not the deployment

#### Scenario: Remote signal failure does not fail pipeline

- **WHEN** `SignalContainerRemote` (SIGHUP) fails on a remote host
- **AND** `ComposeUpRemote` succeeded
- **THEN** the failure is logged as a warning
- **AND** the deployment is still marked successful
