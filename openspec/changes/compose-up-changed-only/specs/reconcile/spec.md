## MODIFIED Requirements

### Requirement: Service Orchestration

The reconciler SHALL run `docker compose up -d` with a configured project name to
bring services to their declared state. A local deploy SHALL run it on the
compose files the Changed-Only Compose Up requirement selects, and SHALL pass
`--remove-orphans` (when orphan removal is enabled) only in a pass that covers
every compose file. Over a subset, `--remove-orphans` would remove the omitted
files' services from the shared project.

The `--wait` flag SHALL NOT be used, because it exits non-zero when any container
is unhealthy, including pre-existing unhealthy containers unrelated to the current
deployment. Health inspection is handled separately by post-deploy verification.

Multiple compose files SHALL be supported via multiple `-f` flags.

On compose up failure, the reconciler SHALL attempt rollback using the backup
compose files. Rollback results are distinguished via sentinel errors:
`ErrRollbackSucceeded` (deploy failed, rollback worked) and `ErrRollbackFailed`
(both failed, critical state).

A configurable compose up timeout (default 10 minutes, overridable via
`BOSUN_COMPOSE_UP_TIMEOUT` env var or `DeployOps.ComposeUpTimeout` field) SHALL
apply to compose operations.

#### Scenario: Compose up with project name

- **WHEN** compose up runs with `ProjectName` set to "bosun"
- **THEN** the command includes `-p bosun`
- **AND** a pass over every compose file includes `--remove-orphans`, which cleans up containers from removed services

#### Scenario: Subset run omits orphan removal

- **WHEN** a local deploy runs compose up on a subset of the compose files
- **THEN** no command in that deploy passes `--remove-orphans`

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

## ADDED Requirements

### Requirement: Changed-Only Compose Up

A local deploy SHALL run compose up on a subset of the compose files only when
every entry in the live compose directory (file, symlink, or subdirectory) is
in this run's deploy manifest; otherwise every file SHALL run. Compose reads
more of that directory than the files passed with `-f` (a `.env` beside them,
for one), and the reconciler cannot see changes to entries it does not deploy,
so it fails closed on any of them.

A local deploy SHALL run compose up only on the live compose files whose
effective input changed this run, and on files that declare a service with no
present container. Presence SHALL follow the pipeline's missing-service rule
(`ServiceIsPresent`): a running, restarting (crash-looping), or
restart-breaker-tripped container is present. A file's effective input changed
when content-hash sync wrote the file, or wrote or deleted a local file the
compose file reads (`env_file`, `label_file`, top-level `configs` or `secrets`
`file:`). A file SHALL always run when it reads an input the reconciler cannot
map to a deployed path (`include`, `extends.file`, an absolute, out-of-tree, or
interpolated path), reads an in-tree input that is not in this run's deploy
manifest (content-hash sync does not track it), declares a service whose
`pull_policy` is anything other than `missing`, `if_not_present`, or `never`,
or declares a service with more than one replica (presence is tracked per
service, so a lost replica would look present).
When these rules select every file, the run is a full run.

Two changes SHALL NOT trigger compose up on their own, and operators apply them
with a forced reconcile (`bosun trigger -f`): an image pulled under a floating
tag (for example `:latest`) outside bosun, and a `${VAR}` value in bosun's own
process environment that compose interpolates. A service that never runs by
design (an inactive profile, `scale: 0`, an exited one-shot job) is never
present, so its file runs on every deploy, as every file did before.

The reconciler SHALL run compose up on every file whenever it cannot tell what
changed: force, drift self-heal, a previous deploy left `NeedsRedeploy` set, no
previous successful deploy, no prior deploy manifest, content-hash sync off,
compose not synced this run, a change in the compose directory other than a
top-level `*.yml` write, an unparseable compose file, a failed or skipped
pre-deploy container listing, a container in the project (in any state) that
belongs to no declared service, or a service declared at the last successful deploy that no
file declares now.

The `--remove-orphans` pass SHALL run only when every file runs. The health-gate
rollback SHALL re-apply every live compose file. Remote deploys are unchanged.

#### Scenario: One compose file changed

- **WHEN** a commit changes the rendered content of one compose file and every declared service is running
- **THEN** compose up runs on that file only
- **AND** no `--remove-orphans` pass runs

#### Scenario: Nothing changed and everything is running

- **WHEN** no compose file or compose input was written and every declared service has a present container
- **THEN** compose up does not run

#### Scenario: A declared service is not running

- **WHEN** no compose file changed and one file's service has no present container
- **THEN** compose up runs on that file only

#### Scenario: Previous deploy did not complete

- **WHEN** the previous deploy left `NeedsRedeploy` set
- **THEN** compose up runs on every file
- **AND** the `--remove-orphans` pass runs

#### Scenario: Orphan container present

- **WHEN** a project container belongs to no service declared in any compose file
- **THEN** compose up runs on every file so the `--remove-orphans` pass removes it

#### Scenario: Changed env_file

- **WHEN** content-hash sync writes a managed file that one compose file reads as `env_file`
- **THEN** compose up runs on that compose file only

#### Scenario: Crash-looping service with unchanged file

- **WHEN** no compose input changed and a service's container is restarting
- **THEN** compose up does not run on its file

#### Scenario: Host-created entry in the compose directory

- **WHEN** the compose directory holds an entry that bosun did not deploy this run (for example a `.env` or an extra `*.yml` created on the host)
- **THEN** compose up runs on every file
- **AND** the `--remove-orphans` pass runs
