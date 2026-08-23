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

Private HTTPS repository authentication SHALL use HTTP Basic authentication
with `BOSUN_GIT_USERNAME` as the username and `BOSUN_GIT_TOKEN` as the
password. Both variables MUST be configured together and MUST apply identically
to initial clone and subsequent fetch operations in standalone and daemon
reconciliation. When both variables are unset, HTTPS synchronization SHALL
remain anonymous. Bosun SHALL NOT recognize unprefixed aliases for these new
variables.

Bosun MUST send these credentials only to an `https://` repository URL. A
partial credential pair, credentials configured for another transport, or
userinfo embedded in a repository URL MUST fail before network I/O. Credential
values and URL userinfo MUST NOT appear in logs, returned errors, validation
diagnostics, or daemon status responses. Runtime synchronization and
`bosun validate` SHALL use the same credential validation rules.

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

#### Scenario: Authenticated HTTPS clone

- **WHEN** a private `https://` repository is configured
- **AND** both `BOSUN_GIT_USERNAME` and `BOSUN_GIT_TOKEN` are non-empty
- **THEN** the initial clone authenticates with that username and token using HTTP Basic authentication

#### Scenario: Authenticated HTTPS fetch

- **WHEN** an existing private HTTPS checkout pulls a new remote commit
- **AND** both HTTPS Git credential variables are non-empty
- **THEN** the fetch authenticates with the same username and token used by clone

#### Scenario: Standalone reconcile consumes HTTPS credentials

- **WHEN** `bosun reconcile` synchronizes a private HTTPS repository
- **THEN** it uses the configured HTTPS Git credential pair for clone and fetch

#### Scenario: Daemon reconcile consumes HTTPS credentials

- **WHEN** the daemon poll or webhook loop synchronizes a private HTTPS repository
- **THEN** it uses the same configured HTTPS Git credential pair as standalone reconcile

#### Scenario: Anonymous HTTPS remains supported

- **WHEN** an HTTPS repository is configured and both HTTPS Git credential variables are unset
- **THEN** clone and fetch proceed without an authentication method

#### Scenario: Partial HTTPS credential pair fails closed

- **WHEN** only one of `BOSUN_GIT_USERNAME` or `BOSUN_GIT_TOKEN` is non-empty
- **THEN** repository synchronization fails before network I/O
- **AND** the error identifies the missing environment variable by name without exposing the configured value

#### Scenario: HTTPS credentials reject other transports

- **WHEN** both HTTPS Git credential variables are configured
- **AND** the repository URL is HTTP, SSH, a local path, or another non-HTTPS transport
- **THEN** repository synchronization fails before network I/O
- **AND** the error explains that HTTPS Git credentials require an `https://` repository URL

#### Scenario: URL-embedded credentials are rejected

- **WHEN** the repository URL contains username, password, or token userinfo
- **THEN** repository synchronization fails before network I/O
- **AND** logs, errors, validation diagnostics, and status responses omit the userinfo
- **AND** the error directs the operator to the dedicated environment variables

#### Scenario: Validate reports unsafe HTTPS credential configuration

- **WHEN** `bosun validate` runs with a partial credential pair, credentials for a non-HTTPS URL, or URL-embedded userinfo
- **THEN** validation fails with the same actionable configuration error as runtime synchronization
- **AND** the diagnostic omits all credential and userinfo values

#### Scenario: Authentication failure is actionable and redacted

- **WHEN** a private HTTPS server rejects the configured Basic credentials
- **THEN** clone or fetch returns an actionable authentication error
- **AND** neither the configured username nor token appears in the error or logs

#### Scenario: New HTTPS credential variables have no legacy aliases

- **WHEN** `GIT_USERNAME` or `GIT_TOKEN` is set without its `BOSUN_` counterpart
- **THEN** Bosun does not use that value for repository authentication

#### Scenario: HTTPS credential variables do not alter SSH resolution

- **WHEN** an SSH repository URL is configured and HTTPS Git credential variables are unset
- **THEN** authentication continues to resolve through the SSH agent and key-file chain

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
