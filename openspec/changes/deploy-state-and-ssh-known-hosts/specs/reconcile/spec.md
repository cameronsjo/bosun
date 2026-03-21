# Reconcile spec delta: deploy state tracking and SSH known_hosts resolution

## MODIFIED Requirements

### Requirement: Git Repository Sync

The reconciler SHALL clone the repository on first run (when no local repo
exists) and pull on subsequent runs. Clones SHALL use depth 1 (shallow) and
track a single branch.

Pulls SHALL fetch the configured branch from origin, verify the remote branch
exists, and hard-reset the working tree to the remote HEAD.

The reconciler SHALL warn when the local repository has uncommitted changes
and proceed with the pull. The hard reset discards local changes, which are
typically stale artifacts from previous reconciliation runs (template renders,
FUSE symlink diffs). This prevents dirty working trees from blocking
automated deployments.

SSH authentication SHALL be resolved in order: SSH agent (via `SSH_AUTH_SOCK`),
then key files (`BOSUN_SSH_KEY`, `/config/deploy-key`, `/config/ssh-key`,
`~/.ssh/id_ed25519`, `~/.ssh/id_rsa`).

SSH host key verification SHALL use config-controlled known_hosts files,
checked in order: `BOSUN_SSH_KNOWN_HOSTS` environment variable (explicit
override), then `/config/known_hosts` (container convention). The user-profile
path `~/.ssh/known_hosts` SHALL NOT be consulted, because it is an ephemeral
location in container environments that can be polluted by manual `ssh`
commands, causing key mismatches. When no known_hosts file is found, the
reconciler SHALL fall back to insecure mode with a warning. The
`BOSUN_SSH_INSECURE_HOST_KEY` environment variable SHALL disable verification
entirely.

#### Scenario: Fresh clone on first run

- **WHEN** the repository directory does not exist or is not a git repo
- **THEN** the reconciler performs a shallow clone (depth 1)
- **AND** reports changed=true with the cloned commit hash

#### Scenario: Pull detects new commit

- **WHEN** the remote branch has a new commit
- **THEN** the reconciler fetches and hard-resets to the remote HEAD
- **AND** returns changed=true with before and after commit hashes

#### Scenario: Pull with dirty working tree

- **WHEN** the local repository has uncommitted changes
- **THEN** the reconciler logs a warning about the dirty state
- **AND** proceeds with fetch and hard reset (discarding local changes)
- **AND** the pull succeeds normally

#### Scenario: Branch validation rejects injection

- **WHEN** a branch name starts with `-` or contains shell metacharacters
- **THEN** the operation fails with a validation error before any git command executes

#### Scenario: DiffFiles with unavailable previous commit

- **WHEN** a shallow clone does not contain the previous commit referenced in the deploy state file
- **THEN** `DiffFiles` SHALL return a sentinel error indicating the commit is unavailable
- **AND** `executePostSyncHooks` SHALL treat all files as changed (run hooks against the full file set)
- **AND** a warning SHALL be logged indicating that full hook execution is triggered due to insufficient git history

#### Scenario: Configurable fetch depth

- **WHEN** `BOSUN_GIT_FETCH_DEPTH` is set to a value greater than 1
- **THEN** git clone and fetch operations SHALL use the specified depth
- **AND** the default depth SHALL remain 1 when unset

#### Scenario: known_hosts resolved from BOSUN_SSH_KNOWN_HOSTS

- **WHEN** `BOSUN_SSH_KNOWN_HOSTS` is set to a valid path
- **THEN** host key verification uses that file exclusively
- **AND** `/config/known_hosts` is not consulted

#### Scenario: known_hosts resolved from container convention path

- **WHEN** `BOSUN_SSH_KNOWN_HOSTS` is not set
- **AND** `/config/known_hosts` exists
- **THEN** host key verification uses `/config/known_hosts`

#### Scenario: No known_hosts found falls back to insecure mode

- **WHEN** `BOSUN_SSH_KNOWN_HOSTS` is not set
- **AND** `/config/known_hosts` does not exist
- **THEN** the reconciler falls back to insecure host key mode
- **AND** logs a warning that host key verification is disabled

#### Scenario: User-profile known_hosts not consulted

- **WHEN** `BOSUN_SSH_KNOWN_HOSTS` is not set
- **AND** `/config/known_hosts` does not exist
- **AND** `~/.ssh/known_hosts` exists with valid host keys
- **THEN** the reconciler does NOT use `~/.ssh/known_hosts`
- **AND** falls back to insecure mode with a warning

#### Scenario: BOSUN_SSH_INSECURE_HOST_KEY disables verification entirely

- **WHEN** `BOSUN_SSH_INSECURE_HOST_KEY=true`
- **THEN** no known_hosts file is consulted
- **AND** all host keys are accepted without verification

### Requirement: Post-Sync Container Restart Hooks

The reconciler SHALL support configurable post-sync hooks that restart containers
when specific file paths change during deployment.

Hooks SHALL be configured via `PostSyncHooks` with fields: `Paths` (glob patterns
matched against changed files relative to repo root), `Action` (the action to
perform, currently only `restart`), and `Container` (the container name to act on).

After a successful deployment, the reconciler SHALL diff the last successfully
deployed commit (from `state.CommitHash`) against the current HEAD to determine
which files changed. Hooks SHALL then be matched against the changed file set
and matching actions executed. Each container SHALL be restarted at most once
per deployment, even if multiple patterns match.

Hooks SHALL only execute when a Docker client is available, dry run is false,
hooks are configured, and `state.CommitHash` is non-empty (i.e., a previous
successful deployment exists). When `state.CommitHash` is empty (first deploy
or no prior state), hooks SHALL NOT execute.

Glob patterns SHALL support `**` for recursive directory matching.

#### Scenario: Container restarted after config change

- **WHEN** a hook is configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **AND** a deployment changes `traefik/conf.d/dynamic.yml`
- **THEN** the reconciler restarts the `traefik` container after compose up
- **AND** logs the hook execution

#### Scenario: No restart when unrelated files change

- **WHEN** a hook is configured with paths `["traefik/conf.d/**"]` and container `traefik`
- **AND** a deployment only changes `docker-compose.yml`
- **THEN** the reconciler does not restart the `traefik` container

#### Scenario: Unsupported hook action skipped

- **WHEN** a hook has an action other than `restart`
- **THEN** the hook is skipped with a warning log

#### Scenario: First deploy skips hooks

- **WHEN** there is no previous commit (first deployment)
- **THEN** post-sync hooks are not evaluated

#### Scenario: Failed pipeline does not advance hook diff base

- **WHEN** a reconciliation pulls commit B but fails at template rendering
- **AND** the previous successful deploy was at commit A (recorded in `state.CommitHash`)
- **THEN** on the next successful reconciliation (commit B or later commit C)
- **AND** the hook diff is computed from commit A, not from commit B
- **AND** files changed between A and the new commit are evaluated for hook patterns

#### Scenario: No prior deploy skips hooks

- **WHEN** `state.CommitHash` is empty (no prior successful deployment recorded)
- **AND** post-sync hooks are configured
- **THEN** the reconciler does not evaluate or execute post-sync hooks
