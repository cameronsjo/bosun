## ADDED Requirements

### Requirement: Changed-Only Compose Up

A local deploy SHALL run compose up only on the live compose files whose
effective input changed this run, and on files that declare a service with no
running or restarting container. A restarting (crash-looping) container counts
as present, matching the pipeline's missing-service check. A file's effective input changed when content-hash sync wrote
the file, or wrote or deleted a local file the compose file reads (`env_file`,
`label_file`, top-level `configs` or `secrets` `file:`). A file that reads an
input the reconciler cannot map to a deployed path (`include`, `extends.file`,
an absolute, out-of-tree, or interpolated path) SHALL always run.

The reconciler SHALL run compose up on every file whenever it cannot tell what
changed: force, drift self-heal, a previous deploy left `NeedsRedeploy` set, no
previous successful deploy, no prior deploy manifest, content-hash sync off,
compose not synced this run, a change in the compose directory other than a
top-level `*.yml` write, an unparseable compose file, a failed or skipped
pre-deploy container listing, a container in the project that belongs to no
declared service, or a service declared at the last successful deploy that no
file declares now.

The `--remove-orphans` pass SHALL run only when every file runs. The health-gate
rollback SHALL re-apply every live compose file. Remote deploys are unchanged.

#### Scenario: One compose file changed

- **WHEN** a commit changes the rendered content of one compose file and every declared service is running
- **THEN** compose up runs on that file only
- **AND** no `--remove-orphans` pass runs

#### Scenario: Nothing changed and everything is running

- **WHEN** no compose file or compose input was written and every declared service has a running or restarting container
- **THEN** compose up does not run

#### Scenario: A declared service is not running

- **WHEN** no compose file changed and one file's service has no running or restarting container
- **THEN** compose up runs on that file only

#### Scenario: Previous deploy did not complete

- **WHEN** the previous deploy left `NeedsRedeploy` set
- **THEN** compose up runs on every file
- **AND** the `--remove-orphans` pass runs

#### Scenario: Orphan container present

- **WHEN** a project container belongs to no service declared in any compose file
- **THEN** compose up runs on every file so the `--remove-orphans` pass removes it
