## ADDED Requirements

### Requirement: Orphan Container Cleanup Configuration

The reconciler SHALL support a configurable `remove_orphans` option (default: `true`)
that controls whether `--remove-orphans` is passed to `docker compose up` commands.

When `remove_orphans` is `true`, the reconciler SHALL append `--remove-orphans` to
all compose-up invocations (local, rollback, and remote). This removes containers
belonging to services that have been deleted from the compose file.

When `remove_orphans` is `false`, the reconciler SHALL omit `--remove-orphans` from
compose-up invocations, leaving orphan containers running.

The option SHALL be configurable via `bosun.yaml` (`remove_orphans` field) and the
`BOSUN_REMOVE_ORPHANS` environment variable. The environment variable SHALL take
precedence over the config file value.

#### Scenario: Default behavior removes orphans

- **WHEN** no `remove_orphans` config is set
- **AND** a service is removed from the compose template
- **AND** the reconciler runs `docker compose up`
- **THEN** the command includes `--remove-orphans`
- **AND** the container from the removed service is stopped and removed

#### Scenario: Orphan removal disabled via config file

- **WHEN** `remove_orphans: false` is set in `bosun.yaml`
- **AND** a service is removed from the compose template
- **AND** the reconciler runs `docker compose up`
- **THEN** the command does NOT include `--remove-orphans`
- **AND** the container from the removed service continues running

#### Scenario: Environment variable overrides config file

- **WHEN** `remove_orphans: true` is set in `bosun.yaml`
- **AND** `BOSUN_REMOVE_ORPHANS=false` is set in the environment
- **THEN** the reconciler omits `--remove-orphans` from compose-up commands

#### Scenario: Rollback respects orphan cleanup config

- **WHEN** `remove_orphans` is `false`
- **AND** a compose-up fails and rollback executes
- **THEN** the rollback compose-up also omits `--remove-orphans`

#### Scenario: Remote deploy respects orphan cleanup config

- **WHEN** `remove_orphans` is `false`
- **AND** a remote deployment runs compose-up over SSH
- **THEN** the SSH command omits `--remove-orphans`

## MODIFIED Requirements

### Requirement: Service Orchestration

The reconciler SHALL run `docker compose up -d` with a configured project name to
bring services to their declared state. When orphan cleanup is enabled (default),
`--remove-orphans` SHALL be appended to remove containers from deleted services.

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

#### Scenario: Compose up with project name and orphan cleanup enabled

- **WHEN** compose up runs with `ProjectName` set to "bosun"
- **AND** `remove_orphans` is `true` (default)
- **THEN** the command includes `-p bosun`
- **AND** `--remove-orphans` cleans up containers from removed services

#### Scenario: Compose up with project name and orphan cleanup disabled

- **WHEN** compose up runs with `ProjectName` set to "bosun"
- **AND** `remove_orphans` is `false`
- **THEN** the command includes `-p bosun`
- **AND** `--remove-orphans` is NOT included in the command

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
