## ADDED Requirements

### Requirement: Compose Directory Ignore List

The `compose_ignore` configuration field (root level, overridable per target)
SHALL list exact names of entries directly in a local target's live compose
directory that the reconciler leaves alone. A listed entry that is not in this
run's deploy manifest SHALL NOT make the deploy run compose up on every file,
SHALL NOT appear in the unmanaged compose-directory entries recorded in deploy
state, and SHALL be removed from the last deploy's record before that record
is compared with the current listing. The reconciler SHALL NOT read a listed
entry as compose input: a listed `*.yml` file SHALL NOT be inspected, passed
to compose up, or included in the health-gate rollback scope.

An entry in this run's deploy manifest or the last successful deploy's
manifest SHALL NOT be ignored even when listed, and the reconciler SHALL log a
warning naming it. An unmanaged entry that is not
listed SHALL keep the fail-closed behavior of Changed-Only Compose Up.

Configuration loading SHALL reject a `compose_ignore` entry that is empty, is
`.` or `..`, contains a path separator, a glob character (`*`, `?`, `[`,
`]`), leading or trailing whitespace, or a NUL byte, or is `.env`, which
compose reads from the compose directory on its own. If an invalid list
reaches the reconciler by another path, the reconciler SHALL log an error and
ignore no entries.

Each local deploy with a non-empty list SHALL log the configured names and the
entries it skipped, and SHALL log a warning for each listed name ending in
`.yml` or `.yaml`. A hot reload of a configuration file without the field
SHALL clear the list. A list set on a target through `BOSUN_TARGETS`
(including an empty list) is environment-owned, and a reload of the
repository configuration SHALL NOT replace it.

The orphan pass is not affected: it acts on containers in the reconciler's
compose project, not on compose-directory entries.

#### Scenario: Ignored unmanaged directory

- **WHEN** the compose directory holds an unmanaged directory named `handmade`
- **AND** `compose_ignore` lists `handmade`
- **AND** one compose file's rendered content changed
- **THEN** compose up runs on that file only
- **AND** no `--remove-orphans` pass runs
- **AND** deploy state records an empty unmanaged list

#### Scenario: Unlisted unmanaged entry

- **WHEN** the compose directory holds an unmanaged entry that `compose_ignore` does not list
- **THEN** compose up runs on every file
- **AND** the `--remove-orphans` pass runs

#### Scenario: Name removed from the list

- **WHEN** the last successful deploy ignored `handmade` and recorded no unmanaged entries
- **AND** the operator removes `handmade` from `compose_ignore`
- **THEN** the next deploy runs compose up on every file
- **AND** deploy state records `handmade` as unmanaged

#### Scenario: Name added to the list

- **WHEN** the last successful deploy recorded `handmade` as unmanaged
- **AND** the operator adds `handmade` to `compose_ignore`
- **THEN** the next deploy narrows compose up to the changed files

#### Scenario: Ignored compose file

- **WHEN** the compose directory holds an unmanaged `extra.yml` listed in `compose_ignore`
- **THEN** `extra.yml` is not parsed and not passed to compose up

#### Scenario: Ignored compose file whose stack runs under the bosun project

- **WHEN** an unmanaged `extra.yml` that earlier deploys ran with bosun's project name is added to `compose_ignore`
- **AND** its containers still run under bosun's project name
- **THEN** the reconciler logs a warning that an ignored compose file must run under its own project name
- **AND** those containers belong to no declared service, so compose up runs on every file and the `--remove-orphans` pass removes them

#### Scenario: List set through BOSUN_TARGETS

- **WHEN** `BOSUN_TARGETS` sets `compose_ignore` on a target
- **AND** the repository `bosun.yaml` sets a different root list, or none
- **THEN** the target keeps the `BOSUN_TARGETS` list across reloads

#### Scenario: Listed name that bosun deploys

- **WHEN** `compose_ignore` lists `b.yml` and bosun deploys `b.yml`
- **THEN** `b.yml` is treated as managed and runs when it changes
- **AND** the reconciler logs a warning

#### Scenario: Invalid entry

- **WHEN** `compose_ignore` contains `../x`, `*.yml`, or `.env`
- **THEN** configuration loading fails with an error naming `compose_ignore`
