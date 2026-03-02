# Reconcile Spec Changes

## MODIFIED Requirements

### Requirement: Service Orchestration

The reconciler SHALL run `docker compose up -d --remove-orphans` with a
configured project name to bring services to their declared state.

The `--wait` flag SHALL NOT be used, because it exits non-zero when any container
is unhealthy, including pre-existing unhealthy containers unrelated to the current
deployment. Health inspection is handled separately by post-deploy verification.

Multiple compose files SHALL be supported via multiple `-f` flags.

When compose up exits non-zero, the reconciler SHALL inspect container state to
classify the failure before deciding whether to roll back. If all declared
containers are running (some may be unhealthy), the failure SHALL be treated as a
warning rather than triggering rollback. If any container failed to start (exited,
restarting, or not found), the failure SHALL trigger rollback as before.

On genuine compose up failure (containers failed to start), the reconciler SHALL
attempt rollback using the backup compose files. Rollback results are distinguished
via sentinel errors: `ErrRollbackSucceeded` (deploy failed, rollback worked) and
`ErrRollbackFailed` (both failed, critical state).

A configurable `ComposeUpTimeout` (default 10 minutes) SHALL apply to compose
operations.

#### Scenario: Compose up with project name

- **WHEN** compose up runs with `ProjectName` set to "bosun"
- **THEN** the command includes `-p bosun`
- **AND** `--remove-orphans` cleans up containers from removed services

#### Scenario: Deploy succeeds despite pre-existing unhealthy container

- **WHEN** container A is healthy and container B was already unhealthy
- **THEN** compose up exits 0 (no `--wait` flag)
- **AND** post-deploy verification logs a warning for container B
- **AND** the deployment is marked successful

#### Scenario: Compose up exits non-zero due to unhealthy dependency

- **WHEN** compose up exits non-zero because container B is unhealthy
- **AND** container A depends on container B with `condition: service_healthy`
- **AND** all containers are in running state (none exited or restarting)
- **THEN** the reconciler classifies the failure as unhealthy-only
- **AND** rollback is NOT triggered
- **AND** the deployment is marked successful with a warning
- **AND** an unhealthy container alert is sent for container B

#### Scenario: Compose up fails with rollback

- **WHEN** compose up fails because a container genuinely failed to start
- **AND** container state inspection shows exited or missing containers
- **AND** a backup exists with previous compose files
- **THEN** the reconciler runs compose up with the backup files
- **AND** returns `ErrRollbackSucceeded` if rollback works

#### Scenario: Both compose up and rollback fail

- **WHEN** compose up fails and rollback also fails
- **THEN** the reconciler returns `ErrRollbackFailed`
- **AND** logs at ERROR level indicating a critical state

### Requirement: Tolerant Compose Up

The reconciler SHALL run `docker compose up` without the `--wait` flag so that
pre-existing unhealthy containers do not cause deployment failures.

When compose up exits non-zero, the reconciler SHALL classify the failure by
inspecting container state via `docker compose ps`. The classification SHALL
distinguish between containers that are running-but-unhealthy and containers that
failed to start (exited, restarting, or absent).

Post-deploy health inspection SHALL categorize each container as:
- **Healthy or no healthcheck**: success (no action)
- **Unhealthy**: warning (alert sent, deployment not failed)
- **Exited or restarting**: error (included in drift report as missing)

When compose up exits non-zero and classification shows only unhealthy containers
(no start failures), the reconciler SHALL:
1. Log a warning with the names of unhealthy containers
2. Send an unhealthy container alert
3. Mark the deployment as successful
4. Continue with post-sync hooks and post-deploy verification

When compose up exits non-zero and classification shows containers that failed to
start, the reconciler SHALL treat it as a genuine deployment failure and proceed
with rollback.

#### Scenario: Deploy succeeds despite unhealthy container

- **WHEN** container A is healthy and container B is unhealthy (pre-existing)
- **THEN** compose up exits 0 (no `--wait` flag)
- **AND** post-deploy verification logs a warning for container B
- **AND** an unhealthy container alert is sent
- **AND** the deployment is marked successful

#### Scenario: Compose up exits non-zero but only unhealthy containers found

- **WHEN** compose up exits non-zero
- **AND** container state inspection shows all containers are running
- **AND** one or more containers have unhealthy health status
- **THEN** the reconciler logs a warning listing the unhealthy containers
- **AND** an unhealthy container alert is sent
- **AND** rollback is NOT triggered
- **AND** the deployment is marked successful

#### Scenario: Compose up exits non-zero with genuine start failure

- **WHEN** compose up exits non-zero
- **AND** container state inspection shows one or more containers exited, not found, or the compose error indicates an invalid image
- **THEN** the deployment is marked as failed
- **AND** rollback is triggered
- **AND** a failure alert is sent

## ADDED Requirements

### Requirement: Compose Exit Classification

When `docker compose up` exits non-zero, the reconciler SHALL inspect the resulting
container state to determine whether the failure is recoverable (unhealthy containers)
or requires rollback (start failures).

The classification SHALL use `docker compose ps --format json` with the same project
name and compose files to query container state. The query SHALL use the same
`ComposeUpTimeout` to prevent hangs.

Each container SHALL be classified as:
- **running-healthy**: running with passing healthcheck or no healthcheck defined
- **running-unhealthy**: running but healthcheck failing
- **failed**: exited, restarting, dead, or not present in the output

The classification result SHALL be one of:
- **unhealthy-only**: all declared containers are running, but one or more are unhealthy
- **start-failure**: one or more containers failed to start

When the classification query itself fails (e.g., Docker unreachable), the original
compose up error SHALL be returned unchanged (fail-safe: assume real failure).

#### Scenario: All containers running but some unhealthy

- **WHEN** compose up exits non-zero
- **AND** `docker compose ps` shows all containers in running state
- **AND** one container has health status unhealthy
- **THEN** the classification result is unhealthy-only
- **AND** the unhealthy container names are included in the result

#### Scenario: Some containers failed to start

- **WHEN** compose up exits non-zero
- **AND** `docker compose ps` shows one container with status exited
- **THEN** the classification result is start-failure

#### Scenario: Classification query fails

- **WHEN** compose up exits non-zero
- **AND** the subsequent `docker compose ps` command also fails
- **THEN** the original compose up error is returned
- **AND** the failure is treated as a start-failure (fail-safe)
