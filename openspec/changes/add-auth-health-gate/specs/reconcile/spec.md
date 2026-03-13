## ADDED Requirements

### Requirement: Critical Container Health Gate

The reconciler SHALL support a configurable list of critical containers that MUST be healthy after `docker compose up` for the deployment to succeed. When any critical container is unhealthy or missing after the health gate timeout, the reconciler SHALL trigger rollback and fail the deployment.

Critical containers SHALL be configured via `critical_containers` in `bosun.yaml` (a list of container names) and overridable via `BOSUN_CRITICAL_CONTAINERS` environment variable (JSON string array). When the env var is set, it completely replaces the config file value.

The health gate SHALL run after the startup grace period has elapsed but before recording the deployment as successful. The gate SHALL poll critical container health via Docker API `ContainerInspect` every 5 seconds for up to `HealthGateTimeout` (default 60 seconds, configurable via `BOSUN_HEALTH_GATE_TIMEOUT`).

Health status classification for critical containers:
- **healthy**: pass (container is running and Docker healthcheck reports healthy)
- **no healthcheck defined**: pass (cannot gate on undefined checks)
- **unhealthy**: fail (Docker healthcheck reports unhealthy after timeout)
- **starting**: fail if still starting at timeout (treated as not-yet-healthy)
- **missing or not running**: fail (container does not exist or is not in running state)

When the critical container list is empty (default), the health gate SHALL be skipped entirely, preserving backwards compatibility.

The `critical_containers` config SHALL be reloaded from the repo's `bosun.yaml` after each git pull, unless the `BOSUN_CRITICAL_CONTAINERS` env var override is set.

#### Scenario: All critical containers healthy

- **WHEN** `critical_containers` is configured with `["traefik", "authelia"]`
- **AND** both containers are running and healthy after compose up
- **THEN** the health gate passes
- **AND** the deployment is recorded as successful
- **AND** post-deploy verification proceeds normally

#### Scenario: Critical container unhealthy triggers rollback

- **WHEN** `critical_containers` is configured with `["traefik", "authelia"]`
- **AND** traefik is healthy but authelia reports "unhealthy" after the health gate timeout
- **THEN** the health gate fails
- **AND** the reconciler triggers rollback to the backup compose files
- **AND** a failure alert is sent identifying authelia as the failing container
- **AND** the deployment is NOT recorded as successful

#### Scenario: Critical container missing triggers rollback

- **WHEN** `critical_containers` is configured with `["traefik", "authelia"]`
- **AND** traefik is running but authelia's container does not exist after compose up
- **THEN** the health gate fails
- **AND** the reconciler triggers rollback

#### Scenario: Critical container without healthcheck passes

- **WHEN** `critical_containers` is configured with `["traefik"]`
- **AND** traefik is running but has no Docker healthcheck defined
- **THEN** the health gate passes (no healthcheck defined = pass)
- **AND** the deployment is recorded as successful

#### Scenario: Health gate timeout with eventual success

- **WHEN** `critical_containers` is configured with `["authelia"]`
- **AND** authelia initially reports "starting" but becomes "healthy" within the timeout
- **THEN** the health gate polls every 5 seconds
- **AND** passes as soon as authelia reports healthy
- **AND** the deployment is recorded as successful

#### Scenario: Empty critical containers list skips gate

- **WHEN** `critical_containers` is empty or not configured
- **THEN** the health gate is skipped entirely
- **AND** the deployment proceeds as before (backwards compatible)

#### Scenario: Env var overrides config file

- **WHEN** `bosun.yaml` sets `critical_containers: ["traefik"]`
- **AND** `BOSUN_CRITICAL_CONTAINERS` is set to `["traefik", "authelia"]`
- **THEN** the health gate uses `["traefik", "authelia"]` from the env var
- **AND** config reload from the repo does not update the critical containers list

#### Scenario: Health gate skipped in dry run

- **WHEN** `DryRun` is true
- **AND** `critical_containers` is configured
- **THEN** the health gate is skipped
- **AND** no Docker API calls are made for health inspection

#### Scenario: Health gate skipped for remote deploys

- **WHEN** `TargetHost` is set (remote deployment)
- **AND** `critical_containers` is configured
- **THEN** the health gate is skipped (Docker API is local-only)
- **AND** a warning is logged indicating the health gate cannot run for remote deploys

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
11. Critical container health gate (if configured)
12. Execute post-sync hooks
13. Post-deploy verification (drift check)
14. Record successful deployment in state file
15. Release lock

A failure at any stage SHALL abort the remaining stages and release the lock. The health gate (stage 11) failing SHALL trigger rollback before aborting.
The lock SHALL always be released via defer, even on panic.

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
- **THEN** backup, deploy, compose up, health gate, post-sync hooks, and post-deploy verification are skipped
- **AND** template rendering still executes to validate templates

#### Scenario: Health gate failure triggers rollback

- **WHEN** compose up succeeds but a critical container fails the health gate
- **THEN** the reconciler triggers rollback to the backup compose files
- **AND** the deployment is NOT recorded as successful
- **AND** a failure alert is sent
- **AND** the lock is released
