## MODIFIED Requirements

### Requirement: Post-Sync Container Restart Hooks

The reconciler SHALL support configurable post-sync hooks that act on containers when specific file paths change during deployment, including paths that are deleted.

Hooks SHALL be configured via `PostSyncHooks` with fields: `Paths` (glob patterns
matched against changed files relative to repo root), `Action` (the action to
perform, `restart` or `exec`), `Container` (the container name to act on), and
`Command` (required for `exec`). A hook whose action is `exec` with an empty or
absent `Command` SHALL be rejected as a config-load error, not silently skipped.

After a successful deployment, the reconciler SHALL determine the set of changed
files, match them against hook glob patterns, and execute matching actions. The
changed-file set SHALL include files that were added or modified AND files that
were deleted (e.g. by `removeStaleFiles`), so that a deletion-only commit still
fires matching hooks. Each container SHALL be restarted at most once per
deployment, even if multiple patterns match.

Hooks SHALL only execute when a Docker client is available, dry run is false, hooks
are configured, and a previous commit exists (not on first deploy).

Glob patterns SHALL support `**` for recursive directory matching, honoring any
literal suffix that follows the `**` segment.

#### Scenario: Container restarted after config change

- **WHEN** a hook is configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **AND** a deployment changes `traefik/conf.d/dynamic.yml`
- **THEN** the reconciler restarts the `traefik` container after compose up
- **AND** logs the hook execution

#### Scenario: No restart when unrelated files change

- **WHEN** a hook is configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **AND** a deployment only changes `docker-compose.yml`
- **THEN** the reconciler does not restart the `traefik` container

#### Scenario: Deletion-only commit fires the hook

- **WHEN** a hook is configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **AND** a deployment only deletes `traefik/conf.d/legacy.yml` (no added or modified files)
- **THEN** the deleted path is included in the changed-file set
- **AND** the reconciler restarts the `traefik` container

#### Scenario: Unsupported hook action skipped

- **WHEN** a hook has an action other than `restart` or `exec`
- **THEN** the hook is skipped with a warning log

#### Scenario: Exec hook with empty command rejected at load

- **WHEN** a hook is configured with `action: exec` and an empty or absent `command`
- **THEN** config loading fails with an error identifying the invalid hook
- **AND** the daemon does not run the deployment with the silently-skipped hook

#### Scenario: First deploy skips hooks

- **WHEN** there is no previous commit (first deployment)
- **THEN** post-sync hooks are not evaluated

#### Scenario: Failed pipeline does not advance hook diff base

- **WHEN** a reconciliation pulls commit B but fails at template rendering
- **AND** the previous successful deploy was at commit A (recorded in `state.CommitHash`)
- **THEN** on the next successful reconciliation (commit B or later commit C)
- **AND** the hook diff is computed from commit A, not from commit B
- **AND** files changed between A and the new commit are evaluated for hook patterns
