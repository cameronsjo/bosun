## ADDED Requirements

### Requirement: Compose Up Tolerates Unhealthy Containers

The reconciler SHALL run `docker compose up` without the `--wait` flag so that pre-existing unhealthy containers do not cause deployment failures.

After compose up succeeds, the reconciler SHALL inspect container health status via the Docker SDK and categorize each container as:
- **Healthy / No healthcheck**: success
- **Unhealthy**: warning (alert, do not fail deployment)
- **Exited / Restarting**: error (include in deploy result)

#### Scenario: Deploy succeeds despite unhealthy container

- **GIVEN** a stack with container A (healthy) and container B (unhealthy, pre-existing)
- **WHEN** reconciliation runs `docker compose up`
- **THEN** compose up exits 0 (no `--wait` flag)
- **AND** post-deploy health inspection logs a warning for container B
- **AND** an unhealthy container alert is sent
- **AND** the deployment is marked successful

#### Scenario: Deploy fails when container cannot start

- **GIVEN** a stack with container C that has an invalid image reference
- **WHEN** reconciliation runs `docker compose up`
- **THEN** compose up exits non-zero
- **AND** the deployment is marked as failed
- **AND** a deployment failure alert is sent

### Requirement: Post-Sync Container Restart Hooks

The reconciler SHALL support configurable post-sync hooks that restart containers when specific file paths change during deployment.

Hooks SHALL be configured via `PostSyncHooks` with fields:
- `Paths`: glob patterns to match against changed files
- `Action`: the action to perform (initially `restart` only)
- `Container`: the container name to act on

After a successful deployment, the reconciler SHALL evaluate which files changed, match them against configured hooks, and execute matching actions.

#### Scenario: Traefik restarted after route config change

- **GIVEN** a post-sync hook configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **WHEN** a deployment changes `traefik/conf.d/dynamic.yml`
- **THEN** the reconciler restarts the `traefik` container after compose up completes
- **AND** logs the hook execution

#### Scenario: No restart when unrelated files change

- **GIVEN** a post-sync hook configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **WHEN** a deployment only changes `docker-compose.yml`
- **THEN** the reconciler does not restart the `traefik` container
