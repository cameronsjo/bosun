## MODIFIED Requirements

### Requirement: Post-Sync Container Restart Hooks

The reconciler SHALL support configurable post-sync hooks that perform container
actions when matching deploy paths change, including when matching paths are
deleted.

Hooks SHALL contain Paths (doublestar glob patterns matched against canonical
staging-relative paths such as appdata/authelia/**), Action (restart or exec),
Container, optional Command, and
optional per-hook Delay. An exec hook with an empty or absent Command SHALL be
rejected before deployment. An unsupported action SHALL be skipped with a
warning.

Hook input SHALL use this priority:

1. A remote deploy SHALL conservatively make all configured hooks eligible
   because it has no per-path result.
2. A local content-hash deploy result SHALL be authoritative even when its
   written and deleted path lists are both empty.
3. A local standard-copy result SHALL supply direct evidence when either list is
   non-empty; otherwise the reconciler SHALL fall back to git diff.
4. A git-diff fallback SHALL compare DeployState.LastDeployedCommit (the last
   successful deploy) with the current commit and normalize repo-relative paths
   into the staging-relative namespace. If the diff base is unavailable, every
   configured hook SHALL conservatively be eligible.

For local direct evidence, the evaluated set SHALL include both written and
deleted paths. Glob matching SHALL honor the complete recursive pattern,
including suffixes after **. Matching hooks SHALL be deduplicated by effective
action: restart hooks are unique per container, and exec hooks are unique per
container and command. Thus multiple matching patterns SHALL NOT restart the
same container more than once per deployment, while distinct actions or exec
commands MAY each run.

On the ordinary path, hooks SHALL be evaluated after deployment work and only
when a previous successful commit exists, dry run is false, hooks are configured,
and Docker is available. A first deploy with empty
DeployState.LastDeployedCommit SHALL skip hooks. A typed post-write verification
failure is the narrow exception: the successfully renamed path SHALL remain
eligible for best-effort matching hooks, but hook execution SHALL NOT convert the
failed reconciliation to success or advance the successful diff base. Hook
action failures remain best-effort and are reported by the hook span; this
requirement does not convert them into a deployment failure.

#### Scenario: Container restarted after config change

- **WHEN** a restart hook is configured with paths ["appdata/traefik/conf.d/**"] and container traefik
- **AND** a deployment changes appdata/traefik/conf.d/dynamic.yml
- **THEN** the reconciler restarts the traefik container after deployment work
- **AND** logs the hook execution

#### Scenario: No restart when unrelated files change

- **WHEN** a restart hook is configured with paths ["appdata/traefik/conf.d/**"] and container traefik
- **AND** a deployment only changes docker-compose.yml
- **THEN** the reconciler does not restart the traefik container

#### Scenario: Deletion-only commit fires the hook

- **WHEN** a hook is configured with paths ["appdata/traefik/conf.d/**"]
- **AND** a local deployment only deletes appdata/traefik/conf.d/legacy.yml
- **THEN** the deleted path is included in the evaluated set
- **AND** the matching hook executes

#### Scenario: Mixed write and deletion retains the matching deletion

- **WHEN** a deployment writes an unrelated path and deletes a hook-matched path
- **THEN** the reconciler evaluates the union of written and deleted paths
- **AND** the deletion's matching hook executes

#### Scenario: Content-hash no-change is authoritative

- **WHEN** content-hash sync returns no written or deleted paths
- **THEN** the reconciler does not fall back to git diff
- **AND** no hooks execute for that no-change result

#### Scenario: Standard-copy empty result falls back to successful diff base

- **WHEN** standard-copy mode returns no direct written or deleted paths
- **AND** the previous successful deploy was commit A
- **THEN** the hook diff compares commit A with the current commit
- **AND** normalizes matching paths into the staging-relative namespace

#### Scenario: Remote deploy conservatively fires hooks

- **WHEN** a remote deploy has no path-level change result
- **THEN** each distinct configured hook is eligible without path matching

#### Scenario: Unsupported hook action skipped

- **WHEN** a hook has an action other than restart or exec
- **THEN** the hook is skipped with a warning log

#### Scenario: Exec hook with empty command rejected at load

- **WHEN** a hook is configured with action exec and an empty or absent command
- **THEN** configuration validation fails before deployment
- **AND** the invalid hook is not silently skipped at execution time

#### Scenario: First deploy skips hooks

- **WHEN** DeployState.LastDeployedCommit is empty
- **THEN** post-sync hooks are not evaluated

#### Scenario: Failed pipeline does not advance hook diff base

- **WHEN** a reconciliation pulls commit B but fails before success
- **AND** the previous successful deploy was commit A
- **THEN** the next fallback hook diff starts from commit A, not commit B
- **AND** changes since A remain eligible

#### Scenario: Post-write verification failure preserves remediation and failure

- **WHEN** a file is renamed successfully and post-write verification returns ErrPostWriteVerification
- **AND** the preserved path matches a configured hook
- **THEN** that hook remains eligible for best-effort execution
- **AND** the reconciliation remains failed with its redeploy marker set
- **AND** the successful diff base does not advance
