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
remain anonymous. Bosun SHALL read these values only from their `BOSUN_` names:
it SHALL NOT recognize unprefixed aliases or project-configuration keys. The
pair SHALL apply to the effective repository URL after the existing
`BOSUN_REPO_URL`-over-`REPO_URL` precedence rule.

Bosun MUST send these credentials only to an absolute `https://` repository URL
with a non-empty host, comparing the scheme case-insensitively. A partial
credential pair, credentials configured for another transport, or
userinfo embedded in a repository URL MUST fail before network I/O. Credential
validation SHALL parse standard URLs and reject any userinfo component,
including username-only, password-bearing, and percent-encoded forms; SCP-like
SSH syntax SHALL retain its existing meaning and SHALL NOT be treated as URL
userinfo.

For authenticated Git traffic, every redirect hop MUST remain HTTPS and MUST
retain the configured origin's case-insensitive hostname and effective port;
an omitted HTTPS port and explicit `:443` SHALL be equivalent. Bosun MUST reject
HTTPS-to-HTTP downgrade and cross-origin redirects without forwarding the Basic
Authorization header. Same-origin HTTPS redirects MAY proceed.

Standalone reconcile SHALL validate this contract before entering the
reconciliation pipeline. Daemon startup SHALL validate it before starting any
listener or background reconcile loop. Clone, fetch, and `bosun validate` SHALL
use the same validation rules so no consumer can bypass the pre-network guard.

The credential pair SHALL remain process-environment state. Bosun MUST NOT copy
it into `reconcile.Config`, project YAML, deploy state, metrics, trace
attributes, logs, returned errors, validation diagnostics, daemon `/config`, or
daemon health/status responses. Project config hot reload SHALL NOT define,
replace, or rotate the pair; an operator-supplied rotation takes effect after
the Bosun process is restarted with the new environment.

Before presentation, Bosun SHALL remove `URL.User` from a parseable repository
URL. If an invalid repository URL cannot be safely parsed, Bosun SHALL display a
fixed redacted placeholder rather than echo the raw URL. Transport errors SHALL
be wrapped or sanitized so observable output contains neither raw nor escaped
username/token/userinfo values nor the derived Basic Authorization value, while
still giving stable guidance for authentication failures.

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

#### Scenario: Same-origin HTTPS redirect preserves authentication

- **WHEN** an authenticated HTTPS clone or fetch receives a redirect whose destination remains HTTPS with the same hostname and effective port
- **THEN** Bosun follows the redirect
- **AND** the redirected Git request carries the configured Basic credentials

#### Scenario: HTTPS downgrade redirect is rejected

- **WHEN** an authenticated HTTPS clone or fetch is redirected to an `http://` destination
- **THEN** synchronization fails before requesting the downgrade destination
- **AND** no Authorization header is forwarded

#### Scenario: Cross-origin redirect is rejected

- **WHEN** an authenticated HTTPS clone or fetch is redirected to a different hostname or effective port
- **THEN** synchronization fails before requesting the cross-origin destination
- **AND** no Authorization header is forwarded

#### Scenario: Standalone reconcile consumes HTTPS credentials

- **WHEN** `bosun reconcile` synchronizes a private HTTPS repository
- **THEN** it uses the configured HTTPS Git credential pair for clone and fetch

#### Scenario: Standalone reconcile rejects unsafe authentication before pipeline execution

- **WHEN** `bosun reconcile` starts with a partial pair, a credential-bearing non-HTTPS URL, or URL userinfo
- **THEN** command validation fails before the reconciliation pipeline or any Git network request starts
- **AND** the error is actionable and redacted

#### Scenario: Daemon reconcile consumes HTTPS credentials

- **WHEN** the daemon poll or webhook loop synchronizes a private HTTPS repository
- **THEN** it uses the same configured HTTPS Git credential pair as standalone reconcile

#### Scenario: Daemon startup rejects unsafe Git authentication

- **WHEN** daemon configuration contains a partial pair, a credential-bearing non-HTTPS URL, or URL userinfo
- **THEN** daemon validation fails before socket, TCP, or HTTP listeners and background loops start
- **AND** the startup error is actionable and redacted

#### Scenario: Anonymous HTTPS remains supported

- **WHEN** an HTTPS repository is configured and both HTTPS Git credential variables are unset
- **THEN** clone and fetch proceed without an authentication method

#### Scenario: Partial HTTPS credential pair fails closed

- **WHEN** only one of `BOSUN_GIT_USERNAME` or `BOSUN_GIT_TOKEN` is non-empty
- **THEN** repository synchronization fails before network I/O
- **AND** the error identifies the missing environment variable by name without exposing the configured value

#### Scenario: HTTPS credentials use the effective repository URL

- **WHEN** both `BOSUN_REPO_URL` and legacy `REPO_URL` are configured with different URLs
- **AND** the HTTPS Git credential pair is configured
- **THEN** authentication validation and synchronization use `BOSUN_REPO_URL`
- **AND** credentials are never evaluated against or sent to the shadowed legacy URL

#### Scenario: HTTPS credentials reject other transports

- **WHEN** both HTTPS Git credential variables are configured
- **AND** the repository URL is HTTP, SSH, a local path, or another non-HTTPS transport
- **THEN** repository synchronization fails before network I/O
- **AND** the error explains that HTTPS Git credentials require an `https://` repository URL

#### Scenario: HTTPS credentials reject malformed or hostless URLs

- **WHEN** both HTTPS Git credential variables are configured
- **AND** the effective repository URL is malformed or has an HTTPS scheme without a host
- **THEN** repository synchronization fails before network I/O
- **AND** the error does not echo the unsafe raw URL

#### Scenario: URL-embedded credentials are rejected

- **WHEN** a standard repository URL contains username-only, password-bearing, or percent-encoded userinfo
- **THEN** repository synchronization fails before network I/O
- **AND** logs, errors, validation diagnostics, and status responses omit the userinfo
- **AND** the error directs the operator to the dedicated environment variables

#### Scenario: SCP-like SSH URL is not userinfo

- **WHEN** the repository URL uses SCP-like SSH syntax such as `git@example.com:owner/repo.git`
- **AND** HTTPS Git credential variables are unset
- **THEN** Bosun does not reject the `git@` portion as URL userinfo
- **AND** existing SSH authentication resolution proceeds

#### Scenario: Validate reports unsafe HTTPS credential configuration

- **WHEN** `bosun validate` runs with a partial credential pair, credentials for a non-HTTPS URL, or URL-embedded userinfo
- **THEN** validation fails with the same actionable configuration error as runtime synchronization
- **AND** the diagnostic omits all credential and userinfo values

#### Scenario: Authentication failure is actionable and redacted

- **WHEN** a private HTTPS server rejects the configured Basic credentials
- **THEN** clone or fetch returns an actionable authentication error
- **AND** neither raw/escaped credentials nor the derived Basic Authorization value appears in the error, logs, or traces

#### Scenario: New HTTPS credential variables have no legacy aliases

- **WHEN** `GIT_USERNAME` or `GIT_TOKEN` is set without its `BOSUN_` counterpart
- **THEN** Bosun does not use that value for repository authentication

#### Scenario: BOSUN credential names cannot be completed by aliases

- **WHEN** only one `BOSUN_` credential variable is configured
- **AND** the corresponding unprefixed alias is also configured
- **THEN** Bosun reports the `BOSUN_` pair as partial before network I/O
- **AND** the unprefixed value is ignored

#### Scenario: Project config reload cannot rotate Git credentials

- **WHEN** `bosun.yaml` is reloaded during daemon reconciliation
- **THEN** no YAML field can define or replace the HTTPS Git username or token
- **AND** the daemon continues using the process environment received at startup

#### Scenario: Credential rotation requires process restart

- **WHEN** an operator changes the configured HTTPS Git credentials outside the running process
- **THEN** Bosun does not claim hot-reload support for the pair
- **AND** the new pair takes effect after the standalone command or daemon process is restarted

#### Scenario: Git credentials are not persisted

- **WHEN** Bosun constructs reconcile config, saves deploy state, emits metrics/traces, or serves daemon responses
- **THEN** neither HTTPS Git credential value nor a reusable Basic Authorization value is serialized or emitted

#### Scenario: Reconcile presentation redacts repository authentication

- **WHEN** a repository URL or Git transport failure is logged or returned by clone, fetch, or the reconciliation pipeline
- **THEN** parseable URL userinfo is removed and unsafe unparseable URLs use a fixed redacted placeholder
- **AND** raw/escaped credentials and the derived Basic Authorization value are absent

#### Scenario: Daemon config response redacts repository userinfo

- **WHEN** the daemon `/config` response includes the configured repository URL
- **THEN** the response includes only the sanitized URL without userinfo
- **AND** it includes no HTTPS Git credential field

#### Scenario: Daemon status and health responses redact authentication material

- **WHEN** `/status`, `/api/status`, or `/health` presents a repository URL or reconciliation error
- **THEN** raw/escaped credentials, URL userinfo, and the derived Basic Authorization value are absent

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
