# Reconcile Specification

## Purpose

The reconcile capability implements the GitOps engine that drives bosun's core
workflow: sync a git repository, decrypt secrets, render templates, back up
existing configs, deploy files, and bring services up via Docker Compose. It
includes persistent deploy state tracking, circuit breaker logic, drift
detection, and post-deploy verification.
## Requirements
### Requirement: Pipeline Orchestration

The reconciler SHALL execute stages in this fixed order:

1. Acquire lock
2. Git repository sync
3. Load deploy state and evaluate skip/circuit-breaker logic
4. Decrypt secrets (SOPS)
5. Render templates (Go text/template + Sprig)
6. Extract declared state from rendered compose
7. Create configuration backup
8. Deploy files (local or remote)
9. Deploy sync invariant check (see Deploy Sync Invariants)
10. Run `docker compose up`
11. Clean up staging directory
12. Critical container health gate (if configured)
13. Execute post-sync hooks
14. Post-deploy verification (drift check)
15. Record successful deployment in state file
16. Release lock

A failure at any stage SHALL abort the remaining stages and release the lock. The health gate (stage 12) failing SHALL trigger rollback before aborting. The invariant check (stage 9) failing SHALL abort before compose up runs; no rollback is needed because no compose changes have been applied at that point.

The lock SHALL always be released via defer, even on panic.

#### Scenario: Full pipeline succeeds

- **WHEN** a reconciliation is triggered and a new commit is available
- **THEN** all stages execute in order
- **AND** the deploy state file records the deployed commit
- **AND** a success alert is sent

#### Scenario: Pipeline aborts on stage failure

- **WHEN** secret decryption fails
- **THEN** template rendering, backup, deploy, invariant check, and compose stages are skipped
- **AND** a throttled failure alert is sent
- **AND** the lock is released

#### Scenario: Invariant check aborts before compose

- **WHEN** stage 9 invariants fail
- **THEN** compose up, cleanup, health gate, hooks, and verification are skipped
- **AND** the lock is released
- **AND** a failure alert is sent
- **AND** the state file is NOT updated

#### Scenario: Dry run mode

- **WHEN** `DryRun` is true
- **THEN** backup, deploy, invariant check, compose up, health gate, post-sync hooks, and post-deploy verification are skipped
- **AND** template rendering still executes to validate templates

#### Scenario: Health gate failure triggers rollback

- **WHEN** compose up succeeds but a critical container fails the health gate
- **THEN** the reconciler triggers rollback to the backup compose files
- **AND** the deployment is NOT recorded as successful
- **AND** a failure alert is sent
- **AND** the lock is released

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

### Requirement: Secret Decryption

The reconciler SHALL decrypt SOPS-encrypted YAML files using the Age encryption
backend. Decryption SHALL use the go-sops library for in-process decryption
without requiring an external `sops` binary.

Age key discovery SHALL follow this order: `SOPS_AGE_KEY` environment variable,
`SOPS_AGE_KEY_FILE` environment variable, default path
`~/.config/sops/age/keys.txt`.

When multiple secret files are configured, the reconciler SHALL decrypt each file
independently and deep-merge the results. Later files SHALL override earlier
files for duplicate keys, with recursive merging for nested maps.

The reconciler SHALL validate that each file contains the `sops` metadata key
before attempting decryption, and SHALL sanitize decryption error messages to
prevent leaking sensitive information (partial keys, decrypted content).

#### Scenario: Single secret file decrypted

- **WHEN** one SOPS-encrypted YAML file is configured
- **THEN** it is decrypted and returned as a map of key-value pairs

#### Scenario: Multiple secret files merged

- **WHEN** two secret files are configured with overlapping keys
- **THEN** the second file's values override the first
- **AND** nested maps are merged recursively

#### Scenario: Missing age key

- **WHEN** no age key is found in any discovery location
- **THEN** decryption fails with an actionable error listing setup instructions

#### Scenario: Invalid SOPS file

- **WHEN** a configured secrets file lacks the `sops` metadata key
- **THEN** decryption fails with a message indicating the file is not SOPS-encrypted
- **AND** includes the `sops --encrypt` command to fix it

#### Scenario: No secrets files configured

- **WHEN** the secrets file list is empty
- **THEN** decryption returns an empty map without error

### Requirement: Template Rendering

The reconciler SHALL render Go `text/template` files (`.tmpl` extension) from
the infrastructure subdirectory to a staging directory. Non-template files SHALL
be copied as-is. Rendered output SHALL strip the `.tmpl` extension.

Template rendering SHALL use Sprig function library plus custom bosun functions:
`include` (reads file contents) and `fromJsonFile` (reads and parses JSON file).

Template data SHALL be the merged secrets map, accessible via `{{ .key }}` syntax.

Rendered files SHALL be written atomically: write to temp file, set permissions
(0644), then rename to final path. This prevents malformed output from partial
writes.

The staging directory SHALL be cleared before rendering to prevent stale files
from previous runs.

#### Scenario: Template rendered with secrets

- **WHEN** a `.tmpl` file references `{{ .network.unraid_ip }}`
- **AND** the secrets contain a `network.unraid_ip` value
- **THEN** the rendered output contains the interpolated value

#### Scenario: Non-template file copied verbatim

- **WHEN** a file without `.tmpl` extension exists in the source directory
- **THEN** it is copied to the staging directory without modification

#### Scenario: Atomic write prevents partial output

- **WHEN** template execution fails mid-render
- **THEN** no output file is created (temp file is cleaned up)
- **AND** previously rendered files from other templates are unaffected

### Requirement: Configuration Backup

The reconciler SHALL create a timestamped tar.gz backup of existing
configuration files before deploying new ones. Backups SHALL be named
`backup-YYYYMMDD-HHMMSS`.

The reconciler SHALL verify backup integrity after creation by listing the
archive contents. Empty or corrupted archives SHALL cause the backup to fail.
Verification SHALL run under the same cancellation context as creation, so a
caller deadline or cancellation aborts verification rather than blocking
indefinitely.

The backup archive SHALL NOT include the backup destination directory or any
prior backup it contains. When the configured backup destination is nested
within a backed-up path (for example, the reconciler's own appdata directory),
the reconciler SHALL exclude the destination so the archive cannot recursively
include its own growing output.

Backup creation and verification SHALL run under a configurable timeout
(`BackupTimeout`, default 5 minutes, overridable via `BOSUN_BACKUP_TIMEOUT`
accepting a Go duration or a plain number of seconds). When the timeout
elapses, the backup SHALL be treated as a failure.

Old backups SHALL be pruned to retain only the most recent N backups
(configurable via `BackupsToKeep`, default 5).

Backup failures, including timeouts, SHALL log a warning but SHALL NOT abort the
deployment pipeline.

The last backup path SHALL be stored for potential rollback during compose up.

#### Scenario: Local backup created and verified

- **WHEN** a local deployment runs with existing config files
- **THEN** a timestamped tar.gz is created containing the config paths
- **AND** the archive is verified to be non-empty and readable

#### Scenario: Remote backup via SSH

- **WHEN** a remote deployment runs
- **THEN** a tar command runs over SSH to create the archive
- **AND** transient SSH errors are retried with exponential backoff

#### Scenario: Backup destination excluded from the archive

- **WHEN** the configured backup destination is nested within a backed-up path
- **THEN** the created archive does not contain the backup destination directory or any prior backup
- **AND** the archive size does not grow with the number of prior backups present

#### Scenario: Backup exceeds its timeout

- **WHEN** backup creation or verification runs longer than `BackupTimeout`
- **THEN** the backup is aborted and treated as a failure
- **AND** a warning is logged
- **AND** the deployment continues

#### Scenario: Old backups pruned

- **WHEN** more than `BackupsToKeep` backups exist after a new backup
- **THEN** the oldest backups are removed, keeping only the most recent N

#### Scenario: Backup failure does not block deploy

- **WHEN** backup creation fails (e.g., no existing configs to backup)
- **THEN** a warning is logged
- **AND** the deployment continues

### Requirement: File Deployment

The reconciler SHALL support two deployment modes: local (direct file
operations) and remote (SSH+tar or SCP).

Local deployment SHALL use atomic-like operations: copy source to a temp directory in
the same parent, then rename to the target path. This provides atomic directory
replacement with `--delete` semantics (files in target not in source are removed).

Remote deployment SHALL use tar-over-SSH for directories (tar source, pipe to SSH
for extraction in a temp dir, atomic move to target) and SCP for individual files
(SCP to temp file, then atomic move).

All remote operations SHALL retry on transient SSH errors (connection refused,
timeout, network unreachable) with exponential backoff (1s, 2s, 4s, max 3
attempts).

All remote operations SHALL validate the host string against an allowlist pattern
and reject strings starting with `-` to prevent SSH option injection.

#### Scenario: Local atomic-like directory deployment

- **WHEN** deploying a directory locally
- **THEN** files are copied to a temp directory in the target's parent
- **AND** the old target is renamed aside (e.g., `target.old`)
- **AND** the temp directory is renamed to the target path
- **AND** the aside directory is removed after successful rename
- **AND** if the final rename fails, the aside directory is renamed back to restore the previous state

#### Scenario: Remote deployment with SSH retry

- **WHEN** a remote SSH operation fails with "connection refused"
- **THEN** it retries up to 3 times with exponential backoff
- **AND** succeeds on a subsequent attempt

#### Scenario: SSH host validation rejects injection

- **WHEN** a target host contains shell metacharacters or starts with `-`
- **THEN** the operation fails with a validation error before any SSH command runs

### Requirement: Service Orchestration

The reconciler SHALL run `docker compose up -d --remove-orphans` with a
configured project name to bring services to their declared state.

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
- **AND** `--remove-orphans` cleans up containers from removed services

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

### Requirement: Reconciliation Locking

The reconciler SHALL acquire an exclusive file-based lock before executing the
pipeline to prevent concurrent reconciliation runs.

On Unix systems, locking SHALL use `flock(2)` with `LOCK_EX|LOCK_NB` for
non-blocking exclusive lock. On Windows, locking SHALL use `LockFileEx` with
`LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY`.

The lock SHALL be released via defer to guarantee release even on errors or
panics.

The default lock file path SHALL be `/var/run/bosun/reconcile.lock`, configurable
via `LockFile`.

#### Scenario: Concurrent reconciliation prevented

- **WHEN** a reconciliation is running and another trigger arrives
- **THEN** the second run fails immediately with a "lock already held" error
- **AND** does not queue or wait

#### Scenario: Lock released after failure

- **WHEN** a reconciliation fails at any stage
- **THEN** the lock is released
- **AND** subsequent reconciliation runs can acquire it

### Requirement: Deploy State Persistence

The reconciler SHALL persist deployment state to a JSON file containing: schema
version, last deployed commit hash, deployment timestamp, deploy count, trigger
source, last attempted commit, attempt count, last alerted attempt, declared
services snapshot, drift check timestamp, and drift items.

The state file SHALL include a `schema_version` field (currently `2`) to support
future schema evolution without breaking existing deployments.

The state file SHALL be written atomically using the pattern: write temp file (in
same directory as target) -> fsync temp -> rename -> fsync directory.

The default state file path SHALL be `/var/lib/bosun/deploy-state.json`,
configurable via `StateFile` and `BOSUN_STATE_DIR`.

#### Scenario: State file written after successful deploy

- **WHEN** a reconcile pipeline completes all stages successfully
- **THEN** a state file is written with the current commit hash, timestamp, source, and declared services
- **AND** the attempt count is reset to 0
- **AND** the last alerted attempt is reset to 0

#### Scenario: State file not updated on failure

- **WHEN** a reconcile pipeline fails at any stage
- **THEN** the `last_deployed_commit` is NOT updated
- **AND** the attempt tracking fields ARE updated (attempted commit, attempt count)

#### Scenario: Missing state file treated as never deployed

- **WHEN** the state file does not exist
- **THEN** the reconciler returns a zero-value state with the current schema version
- **AND** the full pipeline executes regardless of git HEAD

#### Scenario: Corrupt state file treated as never deployed

- **WHEN** the state file exists but contains invalid JSON
- **THEN** the reconciler logs a warning and returns a zero-value state
- **AND** the full pipeline executes

### Requirement: State-Based Skip Logic

The reconciler SHALL compare the current git HEAD commit to the
`last_deployed_commit` from the state file to determine whether to skip the
pipeline.

When the state file commit matches git HEAD and `force` is false, the reconciler
SHALL skip the pipeline and return success.

When the state file commit does not match git HEAD (even if `git.Sync()` reports
no changes), the reconciler SHALL proceed with the full pipeline.

#### Scenario: Same commit skipped

- **WHEN** git HEAD matches `last_deployed_commit` in the state file
- **AND** `force` is false
- **THEN** the reconciler logs "Already deployed commit X, skipping" and returns nil

#### Scenario: Interrupted deployment detected via state mismatch

- **WHEN** a previous reconcile pulled a commit but was interrupted before completion
- **AND** the state file records a different commit (or is absent)
- **AND** git HEAD has not changed
- **THEN** the reconciler detects the mismatch and runs the full pipeline

#### Scenario: Self-restart during compose up

- **WHEN** `docker compose up` restarts bosun's own container mid-pipeline
- **AND** the new instance starts and runs initial reconciliation
- **THEN** the state file does not reflect the current commit
- **AND** the full pipeline re-executes
- **AND** `docker compose up` is idempotent for already-running containers

### Requirement: Circuit Breaker

The reconciler SHALL track consecutive failure count per commit in the state file.
After `MaxAttempts` (3) consecutive failures on the same commit, the reconciler
SHALL stop retrying and return a circuit breaker error.

Before executing the pipeline, the reconciler SHALL increment `attempt_count`
for the same commit or reset to 1 for a new commit.

When an attempt is classified as interrupted because the caller context is
cancelled and the returned pipeline error wraps `context.Canceled`, the
reconciler SHALL restore the failure count that existed before that attempt.
`context.DeadlineExceeded`, an independently returned cancellation error while
the caller context remains live, and a non-cancellation error that races with
caller cancellation SHALL retain normal failure accounting.

The circuit breaker SHALL be overridable via the `force` flag.

A circuit breaker activation SHALL always trigger a failure alert, regardless of
the alert throttling schedule.

#### Scenario: Bad commit triggers circuit breaker

- **WHEN** a commit causes the pipeline to fail 3 consecutive times
- **THEN** the reconciler stops retrying on subsequent triggers
- **AND** logs an ERROR with the failing commit and attempt count
- **AND** includes "use --force to override" in the error message

#### Scenario: New commit resets circuit breaker

- **WHEN** a new commit is pushed after a circuit breaker trip
- **THEN** the attempt count resets to 1
- **AND** the pipeline executes normally

#### Scenario: Force flag overrides circuit breaker

- **WHEN** a trigger with `force=true` is received while circuit breaker is tripped
- **THEN** the pipeline executes regardless of attempt count

#### Scenario: Shutdown interruption preserves prior failure budget

- **GIVEN** the current commit has one previously counted pipeline failure
- **WHEN** a later attempt returns an error wrapping `context.Canceled`
- **AND** the caller context is cancelled by daemon shutdown
- **THEN** the persisted attempt count remains 1
- **AND** the interruption does not move the commit closer to the circuit breaker

#### Scenario: Reconcile deadline remains a failure

- **WHEN** an attempt returns `context.DeadlineExceeded`
- **THEN** the attempt remains counted as a failure for the current commit
- **AND** repeated deadline failures can activate the circuit breaker

#### Scenario: Real error racing with shutdown remains a failure

- **WHEN** the caller context is cancelled during shutdown
- **AND** the pipeline returns an error that does not wrap `context.Canceled`
- **THEN** the attempt remains counted as a failure for the current commit

### Requirement: Force Override

All trigger paths SHALL support a `force` parameter that bypasses the state-based
skip logic and circuit breaker, forcing a full pipeline execution.

The force flag SHALL be per-invocation (set via `SetRunOptions`), not a daemon-wide
configuration.

#### Scenario: Force flag bypasses skip logic

- **WHEN** a trigger with `force=true` is received
- **AND** the state file commit matches current HEAD
- **THEN** the reconciler runs the full pipeline

#### Scenario: Normal trigger respects state

- **WHEN** a trigger without force flag is received
- **AND** the state file commit matches current HEAD
- **THEN** the reconciler skips the pipeline and returns success

### Requirement: Declared vs Actual State Comparison

The reconciler SHALL compare declared state (from rendered compose files) against
actual state (from Docker containers) and produce a drift report.

Declared state SHALL be extracted by parsing rendered compose YAML files and
collecting each service's name and image from the `services` map.

Actual state SHALL be collected by querying Docker for all containers and matching
them to the compose project via labels (`com.docker.compose.project` and
`com.docker.compose.service`). A name-based fallback SHALL be used for containers
lacking compose labels.

Drift types SHALL include: `missing` (declared but not running or not in running
state), `image_mismatch` (declared image differs from running image after tag
normalization), and `unhealthy` (running but Docker health status is "unhealthy").

Image comparison SHALL normalize tags: images without a tag are treated as
`:latest`. Digest references (`image@sha256:...`) SHALL be compared verbatim.

#### Scenario: Missing service detected

- **WHEN** a service is declared in compose but no matching container exists
- **THEN** the drift report includes it as a `missing` drift item

#### Scenario: Non-running service detected as missing

- **WHEN** a declared service has a container that is not in "running" state
- **THEN** the drift report includes it as `missing` with the actual state noted

#### Scenario: Image mismatch detected

- **WHEN** a service declares image `nginx` but the container runs `nginx:1.25`
- **THEN** the drift report includes it as `image_mismatch`
- **AND** both declared and actual images are included

#### Scenario: Unhealthy service detected

- **WHEN** a declared service is running but Docker reports it as "unhealthy"
- **THEN** the drift report includes it as `unhealthy`

#### Scenario: No drift

- **WHEN** all declared services are running, healthy (or no healthcheck), and match images
- **THEN** the drift report is empty

### Requirement: Post-Deploy Verification

After `docker compose up` succeeds, the reconciler SHALL perform a drift check
to verify that all declared services are running.

The post-deploy verification SHALL wait for a configurable startup grace period
(default 30 seconds) before checking, to allow time for containers to start and
health checks to pass.

If drift is detected after deployment, the reconciler SHALL log a warning and
record drift in the state file, but SHALL NOT fail the reconciliation.

If unhealthy containers are found, the reconciler SHALL send an unhealthy
container alert.

Post-deploy verification SHALL only run when a Docker client is available, dry
run is false, and declared services were extracted.

#### Scenario: Post-deploy verification passes

- **WHEN** compose up completes and all declared services are running after the grace period
- **THEN** the reconciler logs success with the count of declared services
- **AND** records zero drift items in the state file

#### Scenario: Post-deploy verification finds drift

- **WHEN** compose up completes but a declared service fails to start
- **THEN** the reconciler logs a warning for each drift item
- **AND** records the drift in the state file
- **AND** the reconciliation still reports success

#### Scenario: Unhealthy containers trigger alert

- **WHEN** post-deploy verification finds unhealthy containers
- **THEN** an unhealthy container alert is sent via the configured alert sender
- **AND** the deployment is still marked successful

### Requirement: Drift CLI

A `bosun drift` command SHALL display the current drift status. Without flags, it
SHALL read the cached drift results from the deploy state file.

The `--live` flag SHALL perform a fresh drift check by reading declared state from
the state file and querying Docker for actual state.

The `--json` flag SHALL output machine-readable JSON.

When no deploy state file exists, the command SHALL indicate that no deployments
have been recorded.

#### Scenario: Cached drift status

- **WHEN** `bosun drift` is run without flags
- **THEN** it reads the deploy state file and displays the last drift check result
- **AND** shows the timestamp of the last check

#### Scenario: Live drift check

- **WHEN** `bosun drift --live` is run
- **THEN** it reads declared state from the state file
- **AND** queries Docker for actual state
- **AND** displays the comparison result

#### Scenario: No previous deployment

- **WHEN** `bosun drift` is run but no deploy state file exists or has no declared services
- **THEN** it prints a message indicating no deployments have been recorded

### Requirement: Periodic Drift Checks

The daemon SHALL perform periodic drift checks on a configurable interval,
independent of the reconciliation poll interval.

Periodic drift checks SHALL NOT trigger reconciliation. They SHALL only update
the drift status in the state file, send deduplicated alerts, and log results.

The drift check interval SHALL be configurable via `BOSUN_DRIFT_INTERVAL`
environment variable. Setting it to 0 SHALL disable periodic drift checks.

Critical drift items (missing or unhealthy) SHALL be subject to per-item alert
deduplication with a configurable cooldown (see Alerting spec: Drift Alert
Deduplication). The state file SHALL be saved after alert dedup updates so that
alert timestamps are persisted atomically with drift results.

#### Scenario: Periodic drift check detects crash

- **WHEN** a container crashes between reconciliations
- **AND** the next periodic drift check runs
- **THEN** the drift is detected and logged at WARN level
- **AND** the deploy state file is updated with the drift
- **AND** a deduplicated alert is sent for the critical drift item

#### Scenario: Periodic drift checks disabled

- **WHEN** `BOSUN_DRIFT_INTERVAL` is set to 0
- **THEN** no periodic drift checks run
- **AND** drift is only checked after deployments

#### Scenario: Persistent drift does not spam alerts

- **WHEN** a drift item persists across multiple check cycles
- **THEN** only the first check sends an alert
- **AND** subsequent checks suppress the alert until the cooldown expires

### Requirement: Drift Status Persistence

The deploy state file SHALL include drift check results: `drift_checked_at`
timestamp, `drift_items` list (service name, drift type, declared value,
actual value), `drift_alerted_at` timestamp of last drift alert sent, and
`drift_alerted_items` map of `"service:type"` keys to last-alerted timestamps
for per-item deduplication.

The drift fields SHALL be updated after every drift check (post-deploy and
periodic), not only after deployments. The alert dedup fields SHALL be updated
atomically with the drift results in the same state file save.

Drift state updates SHALL use the same atomic write pattern as deploy state
updates.

#### Scenario: Drift recorded in state file

- **WHEN** a drift check completes and finds issues
- **THEN** the state file is updated with the check timestamp and drift items
- **AND** the write uses the atomic temp-fsync-rename pattern

#### Scenario: Clean state after no drift

- **WHEN** a drift check finds no issues
- **THEN** the state file records an empty drift items list with the check timestamp

### Requirement: Tolerant Compose Up

The reconciler SHALL run `docker compose up` without the `--wait` flag so that
pre-existing unhealthy containers do not cause deployment failures.

Post-deploy health inspection SHALL categorize each container as:
- **Healthy or no healthcheck**: success (no action)
- **Unhealthy**: warning (alert sent, deployment not failed)
- **Exited or restarting**: error (included in drift report as missing)

#### Scenario: Deploy succeeds despite unhealthy container

- **WHEN** container A is healthy and container B is unhealthy (pre-existing)
- **THEN** compose up exits 0 (no `--wait` flag)
- **AND** post-deploy verification logs a warning for container B
- **AND** an unhealthy container alert is sent
- **AND** the deployment is marked successful

#### Scenario: Container fails to start

- **WHEN** compose up exits non-zero due to an invalid image
- **THEN** the deployment is marked as failed
- **AND** a failure alert is sent

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

- **WHEN** `state.CommitHash` is empty (first deployment or no prior successful deploy recorded)
- **THEN** post-sync hooks are not evaluated

#### Scenario: Failed pipeline does not advance hook diff base

- **WHEN** a reconciliation pulls commit B but fails at template rendering
- **AND** the previous successful deploy was at commit A (recorded in `state.CommitHash`)
- **THEN** on the next successful reconciliation (commit B or later commit C)
- **AND** the hook diff is computed from commit A, not from commit B
- **AND** files changed between A and the new commit are evaluated for hook patterns

### Requirement: Alert Throttling

The reconciler SHALL throttle failure alerts using a progressive schedule to
prevent notification storms from repeated failures on the same commit.

Alerts SHALL be sent on attempt 1, 3, 10, and 30, then every 30th attempt
thereafter. Circuit breaker activation (attempt == `MaxAttempts`) SHALL always
trigger an alert.

The `last_alerted_attempt` SHALL be tracked in the deploy state file to
persist throttle state across restarts.

Recovery alerts SHALL be sent when a deployment succeeds after previous
failures, including the count of prior failures.

#### Scenario: First failure sends alert

- **WHEN** a deployment fails for the first time on a commit
- **THEN** a failure alert is sent
- **AND** `last_alerted_attempt` is updated to 1

#### Scenario: Second failure suppressed

- **WHEN** a deployment fails for the second time on the same commit
- **THEN** no alert is sent (next alert at attempt 3)

#### Scenario: Recovery alert after failures

- **WHEN** a deployment succeeds after 2 previous failures
- **THEN** a recovery alert is sent indicating 2 prior failures

### Requirement: Critical Container Health Gate

The reconciler SHALL support a configurable list of critical containers that MUST be healthy after `docker compose up` for the deployment to succeed. When any critical container is unhealthy or missing after the health gate timeout, the reconciler SHALL trigger rollback and fail the deployment.

Critical containers SHALL be configured via `critical_containers` in `bosun.yaml` (a list of container names) and overridable via `BOSUN_CRITICAL_CONTAINERS` environment variable (JSON string array). When the env var is set, it completely replaces the config file value.

The health gate SHALL run after the startup grace period has elapsed but before recording the deployment as successful. The gate SHALL poll critical container health via Docker API `ContainerInspect` every 5 seconds for up to `HealthGateTimeout` (default 60 seconds, configurable via `BOSUN_HEALTH_GATE_TIMEOUT`).

Health status classification for critical containers:
- **healthy**: pass (container is running and Docker healthcheck reports healthy)
- **no healthcheck defined**: pass (cannot gate on undefined checks)
- **unhealthy**: fail (Docker healthcheck reports unhealthy after timeout)
- **starting**: fail if still starting at timeout (treated as not-yet-healthy)
- **missing or not running**: fail (container does not exist or is not in running state)

When the critical container list is empty (default), the health gate SHALL be skipped entirely, preserving backwards compatibility.

The `critical_containers` config SHALL be reloaded from the repo's `bosun.yaml` after each git pull, unless the `BOSUN_CRITICAL_CONTAINERS` env var override is set.

#### Scenario: All critical containers healthy

- **WHEN** `critical_containers` is configured with `["traefik", "authelia"]`
- **AND** both containers are running and healthy after compose up
- **THEN** the health gate passes
- **AND** the deployment is recorded as successful
- **AND** post-deploy verification proceeds normally

#### Scenario: Critical container unhealthy triggers rollback

- **WHEN** `critical_containers` is configured with `["traefik", "authelia"]`
- **AND** traefik is healthy but authelia reports "unhealthy" after the health gate timeout
- **THEN** the health gate fails
- **AND** the reconciler triggers rollback to the backup compose files
- **AND** a failure alert is sent identifying authelia as the failing container
- **AND** the deployment is NOT recorded as successful

#### Scenario: Critical container missing triggers rollback

- **WHEN** `critical_containers` is configured with `["traefik", "authelia"]`
- **AND** traefik is running but authelia's container does not exist after compose up
- **THEN** the health gate fails
- **AND** the reconciler triggers rollback

#### Scenario: Critical container without healthcheck passes

- **WHEN** `critical_containers` is configured with `["traefik"]`
- **AND** traefik is running but has no Docker healthcheck defined
- **THEN** the health gate passes (no healthcheck defined = pass)
- **AND** the deployment is recorded as successful

#### Scenario: Health gate timeout with eventual success

- **WHEN** `critical_containers` is configured with `["authelia"]`
- **AND** authelia initially reports "starting" but becomes "healthy" within the timeout
- **THEN** the health gate polls every 5 seconds
- **AND** passes as soon as authelia reports healthy
- **AND** the deployment is recorded as successful

#### Scenario: Empty critical containers list skips gate

- **WHEN** `critical_containers` is empty or not configured
- **THEN** the health gate is skipped entirely
- **AND** the deployment proceeds as before (backwards compatible)

#### Scenario: Env var overrides config file

- **WHEN** `bosun.yaml` sets `critical_containers: ["traefik"]`
- **AND** `BOSUN_CRITICAL_CONTAINERS` is set to `["traefik", "authelia"]`
- **THEN** the health gate uses `["traefik", "authelia"]` from the env var
- **AND** config reload from the repo does not update the critical containers list

#### Scenario: Health gate skipped in dry run

- **WHEN** `DryRun` is true
- **AND** `critical_containers` is configured
- **THEN** the health gate is skipped
- **AND** no Docker API calls are made for health inspection

#### Scenario: Health gate skipped for remote deploys

- **WHEN** `TargetHost` is set (remote deployment)
- **AND** `critical_containers` is configured
- **THEN** the health gate is skipped (Docker API is local-only)
- **AND** a warning is logged indicating the health gate cannot run for remote deploys

### Requirement: Infra Directory Misconfiguration Hint

The reconciler SHALL diagnose a likely `BOSUN_INFRA_DIR` misconfiguration when the configured infra/staging directory has no `compose/` child (the condition that produces `ErrComposeDirMissing`), before surfacing the failure.

To do so, the reconciler SHALL scan the immediate child directories of the
infra/staging directory and identify any whose own contents include a
`compose/` subdirectory. Dot-prefixed children (e.g. `.beads`, `.git`) SHALL be excluded,
and a `compose` entry that is a file rather than a directory SHALL NOT count as
a candidate.

- When one or more candidates are found, the surfaced `ErrComposeDirMissing`
  failure SHALL name them and SHALL include a suggested `BOSUN_INFRA_DIR` value
  formed by joining the current `InfraSubDir` with the candidate name.
- When no candidate is found, the failure SHALL retain its existing bare message
  naming the missing compose directory path, with no suggestion.

The hint SHALL be diagnostic only. It SHALL NOT auto-correct `InfraSubDir`,
SHALL NOT change which paths are deployed, and SHALL NOT alter the unconditional
failure semantics of `ErrComposeDirMissing` defined by the Deploy Sync
Invariants requirement. The scan SHALL run only on the failing path (compose
directory absent), never on a successful reconcile.

#### Scenario: Single candidate names the infra dir to set

- **WHEN** `ExtractDeclaredState` finds no `compose/` under the configured infra dir
- **AND** exactly one child directory (e.g. `unraid`) contains a `compose/` subdirectory
- **THEN** the reconcile fails with `ErrComposeDirMissing`
- **AND** the error names `unraid` as the candidate infra directory
- **AND** the surfaced error includes a suggested `BOSUN_INFRA_DIR` value formed from the current `InfraSubDir` joined with `unraid`

#### Scenario: Multiple candidates are all listed

- **WHEN** `ExtractDeclaredState` finds no `compose/` under the configured infra dir
- **AND** more than one child directory contains a `compose/` subdirectory
- **THEN** the error lists every candidate directory
- **AND** directs the operator to set `BOSUN_INFRA_DIR` to one of them

#### Scenario: No candidate keeps the bare error

- **WHEN** `ExtractDeclaredState` finds no `compose/` under the configured infra dir
- **AND** no child directory contains a `compose/` subdirectory
- **THEN** the reconcile fails with `ErrComposeDirMissing` naming the missing path
- **AND** no `BOSUN_INFRA_DIR` suggestion is appended

#### Scenario: Dot-directories and compose files are not candidates

- **WHEN** scanning for candidate infra directories
- **AND** a child is dot-prefixed (e.g. `.beads`) or its `compose` entry is a file
- **THEN** that child is not offered as a candidate

#### Scenario: Hint does not change failure semantics

- **WHEN** a candidate infra directory is identified
- **THEN** the reconcile still fails unconditionally on `ErrComposeDirMissing`
- **AND** `BOSUN_ALLOW_EMPTY_DECLARED_STATE` does not suppress the failure
- **AND** no deploy or `InfraSubDir` value is changed automatically

### Requirement: Deploy Sync Invariants

The reconciler SHALL enforce three invariants between deploy sync (stage 8) and `docker compose up` (stage 10) to prevent silent-success failures where rendered templates fail to overwrite the destination files.

**Invariant 1 — Declared services present.** When template rendering completes and `ExtractDeclaredState` runs, the reconciler SHALL distinguish two failure modes:

- `ErrComposeDirMissing` — the configured staging compose directory does not exist on disk
- `ErrNoDeclaredServices` — the compose directory exists but contains no parseable services

`ErrComposeDirMissing` SHALL fail the reconcile run unconditionally; the override does not apply because a missing compose directory indicates a misconfigured staging path, not a genuinely empty repo.

`ErrNoDeclaredServices` SHALL fail the reconcile run unless the operator opts in via `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true`. When the override is set, the reconciler SHALL log at `Warn` level (not `Info`) and continue.

**Invariant 2 — Written files exist with fresh mtime.** After deploy sync completes, for each path in `WrittenFiles` across all targets, the reconciler SHALL stat the destination path and assert `mtime >= reconcileStartTime`. If any destination is missing or stale, the reconciler SHALL fail before compose-up runs.

**Invariant 3 — Non-empty source must be reflected at the destination.** For each deploy target whose source staging directory contains at least one regular file but whose `WrittenFiles` slice is empty, the reconciler SHALL:

- Inspect the destination directly rather than inferring corruption from the empty write list.
- Assert that every regular file in the source is present at its corresponding destination path.
- Assert that every such file is byte-identical to the source using SHA-256 content equality — the same comparison `CopyFileIfChanged` uses to decide a write is skippable. No mtime assertion is performed, since a content-hash match means the files were written on a prior run.
- Skip symlinks in the source using Lstat semantics, matching the copy path, which never deploys them.
- Pass the invariant when every source file is present and content-equal — a legitimate no-op, since the destination already byte-matches the source.
- Fail the run when any source file is absent from the destination or differs in content, naming the first mismatching destination path.

A symlink-only source therefore imposes no requirement on the destination.

This refines the original formulation, which failed *any* zero-write target against a non-empty source. That was too aggressive: with content-hash sync a target legitimately records zero writes when the destination already matches, so a single byte-identical config could abort an entire reconcile (see GH#330). Asserting the real post-condition — files present *and content-equal* at the destination — preserves protection against silent-sync failures while permitting genuine no-ops. Existence alone is insufficient: a stale destination file occupying the right path would pass an existence check yet serve outdated config, so content equality closes that gap.

The invariant check (invariants 2 and 3) MAY be skipped via `BOSUN_SKIP_DEPLOY_INVARIANT=true` for diagnostic or development scenarios. When skipped, the reconciler SHALL log at `Warn` level noting that invariants are disabled.

Per-file write decisions SHALL be observable: `CopyDirIfChanged` and `CopyFileIfChanged` SHALL emit a `Debug` log on every file write (formatted `wrote src=<src> dst=<dst> bytes=<n>`) and every skip (formatted `skipped src=<src> dst=<dst> reason=hash_match`). This gives operators a way to confirm sync behavior from the log stream without inspecting destination mtimes externally.

#### Scenario: Reconcile fails when declared services is zero

- **WHEN** `ExtractDeclaredState` returns `ErrNoDeclaredServices`
- **AND** `BOSUN_ALLOW_EMPTY_DECLARED_STATE` is unset or `false`
- **THEN** the reconciler fails the pipeline at stage 6
- **AND** the error message names the staging compose directory path
- **AND** compose up does not run

#### Scenario: Override allows empty declared services

- **WHEN** `ExtractDeclaredState` returns `ErrNoDeclaredServices`
- **AND** `BOSUN_ALLOW_EMPTY_DECLARED_STATE=true`
- **THEN** the reconciler logs at `Warn` level and continues
- **AND** post-deploy verification still respects the existing "declared services were extracted" precondition

#### Scenario: Missing compose directory always fails

- **WHEN** `ExtractDeclaredState` returns `ErrComposeDirMissing`
- **THEN** the reconciler fails the pipeline regardless of `BOSUN_ALLOW_EMPTY_DECLARED_STATE`
- **AND** the error message names the expected directory path
- **AND** the operator is directed to verify the staging path configuration

#### Scenario: Stale destination mtime blocks compose-up

- **WHEN** deploy sync completes
- **AND** a destination file in `WrittenFiles` has `mtime < reconcileStartTime`
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the stale destination path

#### Scenario: No-op sync against a content-matched destination passes

- **WHEN** a deploy target's source staging directory contains regular files
- **AND** the target's `WrittenFiles` returned by `CopyDirIfChanged` is empty
- **AND** every source file already exists at its corresponding destination path and is byte-identical to the source
- **THEN** the invariant check passes at stage 9 (legitimate no-op — destination already byte-matches)
- **AND** compose up proceeds normally

#### Scenario: Empty WrittenFiles with a missing destination file blocks compose-up

- **WHEN** a deploy target's source staging directory contains regular files
- **AND** the target's `WrittenFiles` returned by `CopyDirIfChanged` is empty
- **AND** at least one source file is absent from the destination
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the first mismatching destination path

#### Scenario: Empty WrittenFiles with a stale-content destination file blocks compose-up

- **WHEN** a deploy target's source staging directory contains regular files
- **AND** the target's `WrittenFiles` returned by `CopyDirIfChanged` is empty
- **AND** a destination file exists at the corresponding path but its content differs from the source (a stale write the content-hash sync failed to replace)
- **THEN** the invariant check fails at stage 9
- **AND** compose up does not run
- **AND** the error message names the first mismatching destination path

#### Scenario: Symlinks in the source impose no destination requirement

- **WHEN** a deploy target's source staging directory contains only symlinks (no regular files)
- **AND** the target's `WrittenFiles` is empty
- **THEN** the invariant check passes at stage 9 (symlinks are never deployed, so nothing is required at the destination)
- **AND** compose up proceeds normally

#### Scenario: Healthy deploy passes invariant check

- **WHEN** deploy sync writes at least one file per non-empty target
- **AND** every destination has `mtime >= reconcileStartTime`
- **THEN** the invariant check passes silently
- **AND** compose up proceeds normally

#### Scenario: Operator skips invariants for diagnostics

- **WHEN** `BOSUN_SKIP_DEPLOY_INVARIANT=true`
- **THEN** invariants 2 and 3 are bypassed
- **AND** the reconciler logs at `Warn` level noting that invariants are disabled
- **AND** invariant 1 (declared services) is still enforced

#### Scenario: Per-file write logs emitted on Debug level

- **WHEN** `CopyDirIfChanged` writes 3 files and skips 2 files
- **AND** the log level is `Debug` or finer
- **THEN** five log lines are emitted total
- **AND** each write line includes `src`, `dst`, and `bytes`
- **AND** each skip line includes `src`, `dst`, and `reason=hash_match`

### Requirement: Interrupted Reconciliation Outcome

The reconciler SHALL classify an attempt as interrupted only when the caller
context reports `context.Canceled` and the returned pipeline error wraps
`context.Canceled`.

For a classified interruption, the reconciler SHALL persist an optional
`last_attempt_outcome` object containing the canonical outcome `interrupted`,
the affected commit when known, and an interruption timestamp. The object SHALL
not contain arbitrary pipeline error text. State files that omit the object
SHALL remain valid and SHALL mean that no interruption outcome is recorded.

The reconciler SHALL preserve the `last_attempted_commit`, `attempt_count`, and
`last_alerted_attempt` failure budgets that existed before the interrupted
attempt. It SHALL NOT clear an existing `needs_redeploy` marker during
interruption finalization, so a later trigger retries possibly partial deploy
work even when Git HEAD is unchanged. Cancellation before deploy mutation SHALL
NOT set `needs_redeploy` solely because the run was interrupted. A later attempt
that reaches a terminal success or ordinary failure SHALL clear or replace the
interruption outcome.

#### Scenario: Mid-deploy shutdown is persisted as interrupted

- **GIVEN** a reconcile has begun deploying the current commit
- **WHEN** daemon shutdown cancels the caller context
- **AND** deployment returns an error wrapping `context.Canceled`
- **THEN** `last_attempt_outcome.outcome` is persisted as `interrupted`
- **AND** the outcome identifies the current commit and interruption time
- **AND** `needs_redeploy` remains true
- **AND** a later trigger retries the pipeline even when Git HEAD is unchanged

#### Scenario: First interrupted run of a new commit consumes no failure budget

- **GIVEN** a commit has no counted pipeline failures
- **WHEN** its first attempt is classified as interrupted
- **THEN** its counted failure total remains zero
- **AND** the next ordinary failure on that commit is counted as failure 1

#### Scenario: Legacy state has no interruption outcome

- **WHEN** a state file written by an older Bosun version omits
  `last_attempt_outcome`
- **THEN** the state file loads successfully
- **AND** no interruption outcome is inferred

#### Scenario: Later completed attempt supersedes interruption outcome

- **GIVEN** state records an interrupted attempt
- **WHEN** a later attempt reaches an ordinary failure or success
- **THEN** the stale interrupted outcome is no longer reported as the last
  attempt outcome

#### Scenario: Best-effort hook cancellation is not a terminal interruption

- **GIVEN** a post-sync hook returns an error wrapping `context.Canceled`
- **AND** `runPostSyncHooksWithSpan` records and swallows that best-effort error
- **WHEN** the remaining pipeline completes successfully
- **THEN** the reconcile is not classified as interrupted on the basis of the
  swallowed hook error
- **AND** post-sync hook cancellation does not consume or restore the deploy
  failure budget

### Requirement: Non-Live Cycle Context Stops Target Iteration

Ordinary per-target failures SHALL retain the existing behavior of logging and
alerting the failure before proceeding to the next configured target while the
cycle context remains live. When the shared cycle context reports either
`context.Canceled` or `context.DeadlineExceeded`, the daemon SHALL stop the
reconciliation cycle before constructing or running any later target, regardless
of the in-flight target's returned error.

Only propagated caller cancellation SHALL receive interruption accounting. A
shared reconcile deadline that expires during a target SHALL remain an ordinary
counted failure with the active target's existing alert behavior, but the daemon
SHALL NOT pass the already-expired context to later targets.

Only an in-flight target that satisfies interruption classification SHALL
finalize an interrupted outcome and alert. State files and alert streams for
later targets SHALL remain unchanged. If a terminal cycle context is observed
between targets, the daemon SHALL stop before starting the next target without
creating an interrupted outcome, failure, or alert for a target that did not
begin.

#### Scenario: Shutdown during first target stops later targets

- **GIVEN** targets `unraid`, `pi`, and `nas` are configured in that order
- **AND** `unraid` is currently reconciling
- **WHEN** daemon shutdown cancels the cycle context
- **AND** `unraid` returns an error wrapping `context.Canceled`
- **THEN** `unraid` finalizes one interrupted outcome
- **AND** the daemon does not construct or run `pi` or `nas`
- **AND** the state files and alert streams for `pi` and `nas` remain unchanged

#### Scenario: Cancellation between targets invents no interrupted attempt

- **GIVEN** one target has completed and another target has not started
- **WHEN** daemon shutdown cancels the cycle context between those targets
- **THEN** target iteration stops before the next target begins
- **AND** no interruption outcome or alert is created for the untouched target

#### Scenario: Ordinary target failure still continues

- **WHEN** a target returns an error that does not satisfy interruption
  classification
- **AND** the cycle context remains live
- **THEN** the target failure retains its existing state and alert behavior
- **AND** reconciliation proceeds to the next configured target

#### Scenario: Real target error racing with shutdown stops iteration

- **WHEN** a target returns an error that does not wrap `context.Canceled`
- **AND** the cycle context reports `context.Canceled`
- **THEN** that target retains ordinary failure accounting and alert behavior
- **AND** reconciliation does not start the next configured target

#### Scenario: Shared reconcile deadline charges only the active target

- **GIVEN** multiple targets share one cycle-level reconcile deadline
- **WHEN** the deadline expires while one target is active
- **AND** that target returns `context.DeadlineExceeded`
- **THEN** the active target records an ordinary counted failure and retains its
  existing failure-alert behavior
- **AND** reconciliation does not start any later target
- **AND** later target state and alert streams remain unchanged

### Requirement: Local Rollback Archive Extraction Confinement

Every local backup-consuming rollback path SHALL extract
`<backupPath>/configs.tar.gz` with the same in-process, single-reader extraction
policy used by remote compose rollback. `RollbackFromBackupSet` (the current
full-managed-tree successor to `RollbackFromBackup`) and `ComposeUpIsolated`
SHALL NOT invoke an external tar extractor for backup restore.

The extractor SHALL map valid Bosun archive members into a fresh temporary root
in the layout expected by `resolveBackupFile`. For each member, it SHALL validate
the realized destination before writing. Member-name traversal, absolute or
escaping symlink targets, and escaping hardlink targets SHALL be rejected before
they can create or redirect content outside that root. Relative symlinks and
archive-root-relative hardlinks whose realized targets remain within the root
SHALL remain supported.

Each local caller SHALL pass `safeExtractBackup` its existing background-derived,
independently bounded rollback/extraction context, preserving the outer
failed-deployment context's logging metadata without inheriting its
cancellation. Cancellation of the outer method or deployment context SHALL NOT
suppress a local rollback extraction attempt. The extractor SHALL honor
cancellation or deadline expiry of the independent context it receives and the
existing total decompressed size bound. It SHALL return a usable root only after
the complete archive passes. On any validation, corruption, I/O, size-bound, or
independent-context cancellation error, the extractor SHALL remove the partial
temporary tree and return no usable root before a local caller copies to live
state, removes a live path, invokes compose with a backup file, or includes a
backup file in an orphan-reconciliation pass.

`RollbackFromBackupSet` SHALL preserve its rollback-not-attempted outward
contract on extraction failure while returning an actionable error that
keeps the extraction cause discoverable via `errors.Is`/`errors.As`.
`ComposeUpIsolated` SHALL preserve the original compose failure in its per-file
result and aggregate outcome, log the extraction cause, report no successful
rollback for that file, and exclude the unrolled failed file from the
orphan-reconciliation pass.

#### Scenario: Full-tree local rollback accepts a valid archive

- **WHEN** `RollbackFromBackupSet` receives a valid Bosun backup archive whose members and any link targets remain within the extraction root
- **THEN** the archive is extracted in-process and the requested managed files are resolved from the completed temporary tree
- **AND** live managed files are restored before the restored compose files are re-applied

#### Scenario: Per-file local rollback accepts a valid archive

- **WHEN** `ComposeUpIsolated` needs to roll back a failed compose file and the backup archive is valid
- **THEN** the archive is extracted in-process at most once for that operation
- **AND** compose rollback uses the matching file from the completed temporary tree
- **AND** only a successfully rolled-back backup file can be included in the orphan-reconciliation pass

#### Scenario: Archive member traversal is rejected

- **WHEN** a backup archive contains a member name that traverses outside the extraction root
- **THEN** extraction fails before either local rollback consumer can use any extracted content
- **AND** no path outside the temporary root is created or modified

#### Scenario: Absolute symlink target is rejected

- **WHEN** a backup archive contains a symlink with an absolute target
- **THEN** extraction fails before the symlink or a later write through it can escape the temporary root
- **AND** neither local rollback consumer uses the partially extracted archive

#### Scenario: Relative symlink target escaping the root is rejected

- **WHEN** a backup archive contains a symlink whose relative target resolves outside the extraction root
- **THEN** extraction fails before the symlink or a later write through it is admitted
- **AND** neither local rollback consumer uses the partially extracted archive

#### Scenario: Hardlink target escaping the root is rejected

- **WHEN** a backup archive contains a hardlink whose archive-relative target resolves outside the extraction root
- **THEN** extraction fails before the hardlink is created
- **AND** neither local rollback consumer uses the partially extracted archive

#### Scenario: Full-tree rollback survives outer cancellation but honors its independent deadline

- **WHEN** `RollbackFromBackupSet` is invoked with an already-cancelled outer method context after a failed deployment
- **THEN** it still attempts extraction with its background-derived, independently bounded rollback context
- **AND** when that independent context is cancelled or reaches its deadline before or during archive entry processing, extraction returns promptly with its context cause discoverable
- **AND** the partial temporary tree is cleaned before any live managed-tree restore or compose invocation

#### Scenario: Per-file rollback survives outer cancellation but honors its independent deadline

- **WHEN** `ComposeUpIsolated` reaches backup extraction while its outer deployment context is cancelled
- **THEN** it still attempts extraction with its background-derived, independently bounded extraction context
- **AND** when that independent context is cancelled or reaches its deadline before or during archive entry processing, extraction returns promptly and logs its context cause
- **AND** the partial temporary tree is cleaned before any backup-based compose invocation or orphan-pass use

#### Scenario: Failed extraction cleans partial content before live use

- **WHEN** a valid early archive entry is extracted and a later entry fails validation or extraction
- **THEN** the extractor removes the entire partial temporary tree and returns no usable root
- **AND** no live managed file is copied or removed and no compose or orphan-pass command receives a path from that tree

#### Scenario: Full-tree extraction error preserves rollback outcome

- **WHEN** archive extraction fails for `RollbackFromBackupSet`
- **THEN** the method returns its rollback-not-attempted outcome with an actionable extraction cause discoverable via `errors.Is`/`errors.As`
- **AND** it performs no managed-tree restore, deletion, or restored compose invocation

#### Scenario: Per-file extraction error preserves the original compose failure

- **WHEN** archive extraction fails after a compose file fails in `ComposeUpIsolated`
- **THEN** the extraction cause is logged and the original compose failure remains on the per-file result and aggregate outcome
- **AND** the file is not marked rolled back and its failed new path or partial backup path is excluded from the orphan-reconciliation pass

### Requirement: Health Gate Scope

The reconciler SHALL support a configurable `health_gate_scope` that selects which containers the post-compose-up health gate polls and rolls back on. The scope SHALL be one of `critical`, `declared`, or `off`, configured via `health_gate_scope` in `bosun.yaml` and overridable via the `BOSUN_HEALTH_GATE_SCOPE` environment variable. An empty or unset value SHALL resolve to `critical`.

- **critical** (default): the gate polls only the configured `critical_containers` members, exactly as the Critical Container Health Gate requirement describes. An empty `critical_containers` list skips the gate. A declared-but-non-critical service coming up unhealthy SHALL NOT trigger rollback.
- **declared**: the gate polls all declared services (those extracted from the staging compose files). A service that was already unhealthy BEFORE this deploy (a pre-existing casualty) SHALL be exempt and SHALL NOT trigger rollback; only a service this deploy made unhealthy triggers the gate. An empty declared set skips the gate.
- **off**: the health gate SHALL be skipped entirely.

When an unknown config-file `health_gate_scope` value reaches the gate at deploy
time, the gate SHALL fall back to `critical` and log an error that names the
three valid values rather than failing the deployment.

Regardless of scope, the gate SHALL be skipped when the deploy is a dry run, the deploy is remote (the Docker API is local-only and cannot observe the remote host's containers), or no Docker client is available.

On a gate failure under any scope, the reconciler SHALL trigger rollback to the backup compose files before post-sync hooks run, SHALL skip post-sync hooks when a rollback ran (the working tree is a hybrid of old compose and new config), and SHALL NOT record the deployment as successful.

Alerting on a gate failure differs by scope, so a flapping healthcheck under `declared` does not spam:

- **critical**: SHALL send only the existing throttled failure alert on the attempt-count schedule — byte-for-byte the prior behavior, with NO rollback-specific alert.
- **declared**: SHALL send the throttled failure alert AND, when a rollback ran, a rollback alert (success or failure) — both on the SAME attempt-count throttle window, so they fire on the established cadence rather than once per cycle.

`BOSUN_HEALTH_GATE_SCOPE` SHALL take precedence over the config file value. An invalid env value SHALL be ignored with a warning, leaving the config-file (or default) scope in effect.

#### Scenario: Declared scope rolls back on a service this deploy broke

- **WHEN** `health_gate_scope` is `declared`
- **AND** a declared service is healthy before the deploy but reports unhealthy after compose up within the health gate timeout
- **THEN** the health gate fails
- **AND** the reconciler triggers rollback to the backup compose files
- **AND** post-sync hooks are skipped
- **AND** the deployment is NOT recorded as successful
- **AND** a throttled failure alert AND a throttled rollback alert are sent on the same attempt-count window

#### Scenario: Declared scope exempts a pre-existing unhealthy service

- **WHEN** `health_gate_scope` is `declared`
- **AND** a declared service was already unhealthy before the deploy and remains unhealthy after compose up
- **THEN** the health gate does NOT fail on that service
- **AND** no rollback is triggered
- **AND** the deployment is recorded as successful

#### Scenario: Critical scope ignores a non-critical declared service

- **WHEN** `health_gate_scope` is `critical` (the default) with no `critical_containers` configured
- **AND** a declared-but-non-critical service reports unhealthy after compose up
- **THEN** the health gate is skipped
- **AND** no rollback is triggered
- **AND** the deployment is recorded as successful

#### Scenario: Off scope runs no gate

- **WHEN** `health_gate_scope` is `off`
- **AND** a declared service reports unhealthy after compose up
- **THEN** the health gate does not run
- **AND** no rollback is triggered
- **AND** the deployment is recorded as successful

#### Scenario: Unknown config-file scope falls back at deploy time

- **WHEN** config-file `health_gate_scope` is set to a value other than `critical`, `declared`, or `off`
- **THEN** the invalid value may reach the gate at deploy time
- **AND** the gate falls back to `critical`
- **AND** the gate logs an error naming the invalid value and the three valid values

#### Scenario: Unknown environment override is ignored

- **GIVEN** the config file selects a valid `health_gate_scope`, or leaves it unset
- **WHEN** `BOSUN_HEALTH_GATE_SCOPE` is set to a value other than `critical`, `declared`, or `off`
- **THEN** the environment override is ignored with a warning
- **AND** the config-file scope, or the `critical` default, remains in effect

### Requirement: Drift Self-Heal Attempt Bounding

Drift self-heal SHALL track reconciliation attempts per drift signature and SHALL stop attempting after a bounded number of attempts when reconciling does not resolve the drift, rather than looping indefinitely.

When `BOSUN_DRIFT_SELF_HEAL` is enabled, a periodic drift check that finds drift
MAY trigger a reconciliation. The daemon SHALL maintain, in the deploy state
file, a per-drift-signature attempt counter, where a drift signature is the
stable set of currently-drifted `service:type` items. The attempt bound SHALL be
configurable via `BOSUN_DRIFT_SELF_HEAL_MAX_ATTEMPTS` with a small positive
default.

Each self-heal trigger for the current signature SHALL increment the counter.
When the counter reaches the bound, the daemon SHALL mark the signature exhausted,
SHALL stop triggering self-heal for that signature, and SHALL emit a
`self-heal-exhausted` alert at most once per exhausted signature. When the drift
signature changes, or the drifted items clear, the counter SHALL reset and an
exhausted signature SHALL re-arm.

#### Scenario: Out-of-band drift exhausts the self-heal bound
- **WHEN** `BOSUN_DRIFT_SELF_HEAL` is enabled and a drift caused by out-of-band container state persists across self-heal attempts
- **THEN** self-heal triggers up to the configured maximum number of attempts for that signature
- **AND** after the bound is reached the daemon stops triggering self-heal for that signature
- **AND** a `self-heal-exhausted` alert is emitted once

#### Scenario: New drift signature resets the attempt counter
- **WHEN** a different set of services enters drift after a prior signature was exhausted
- **THEN** the attempt counter for the new signature starts at zero
- **AND** self-heal may attempt reconciliation again for the new signature

#### Scenario: Resolved drift re-arms an exhausted signature
- **WHEN** a previously exhausted drift signature later clears (drift items become empty)
- **THEN** the exhausted state for that signature is cleared
- **AND** a subsequent recurrence of the same signature is eligible for self-heal again

#### Scenario: Self-heal disabled performs no attempts
- **WHEN** `BOSUN_DRIFT_SELF_HEAL` is disabled and a periodic drift check finds drift
- **THEN** no self-heal reconciliation is triggered
- **AND** no attempt counter is incremented

### Requirement: Restart Breaker Baseline Integrity

The restart circuit breaker SHALL NOT silently reset its restart-count baseline merely because the evaluation window elapsed while restarts are still accumulating, so that a sustained slow restart loop still trips.

When evaluating a tracked service whose current restart count exceeds its
baseline (`delta > 0`), the breaker SHALL preserve the earliest unresolved-restart
baseline (its count and timestamp) when the elapsed time exceeds the configured
window, rather than resetting the baseline to the current observation. The breaker
SHALL advance the baseline normally only when no new restarts occurred since the
last check (`delta <= 0`). A service that restarts repeatedly across intervals
longer than `BOSUN_RESTART_WINDOW` SHALL still accumulate toward the threshold and
trip.

At configuration load, the daemon SHALL warn when `BOSUN_DRIFT_INTERVAL` is
greater than `BOSUN_RESTART_WINDOW`, because the breaker observes restart counts
on the drift-check cadence and a window-bounded delta would otherwise be
unobservable.

#### Scenario: Slow restart loop trips despite long drift interval
- **WHEN** `BOSUN_DRIFT_INTERVAL` is greater than `BOSUN_RESTART_WINDOW` and a container restarts repeatedly across successive drift checks
- **THEN** the breaker preserves the accumulating baseline rather than resetting it each interval
- **AND** the service eventually trips the restart breaker

#### Scenario: Clean check advances the baseline
- **WHEN** a tracked service shows no new restarts since the last check (`delta <= 0`)
- **THEN** the breaker advances the baseline to the current count and timestamp

#### Scenario: Misconfigured intervals warn at load
- **WHEN** the daemon loads configuration with `BOSUN_DRIFT_INTERVAL` greater than `BOSUN_RESTART_WINDOW`
- **THEN** a warning is logged identifying the interval/window mismatch

### Requirement: Restart Breaker Resolution Attribution

The restart circuit breaker SHALL distinguish a container recreation caused by bosun's own deploy from external operator recovery and SHALL NOT record a deploy-driven recreation as resolution of a tripped service.

The breaker SHALL track a container-identity field (such as the container ID)
alongside the restart count for each tracked service. Because Docker resets a
container's restart count to zero on recreation, a lower-or-equal restart count
SHALL NOT by itself be treated as operator recovery. When the tracked
container identity changes, the breaker SHALL treat the event as recreation and
SHALL require a post-recreate stability grace period — at least one check cycle
with no further restarts — before marking the service `Resolved`. A service that
resumes restart-looping after recreation SHALL remain tripped.

#### Scenario: Deploy-driven recreation does not falsely resolve
- **WHEN** a tripped service is recreated by a reconcile and immediately resumes restart-looping
- **THEN** the breaker recognizes the changed container identity as recreation
- **AND** does not emit a `Resolved` event
- **AND** the service remains tripped

#### Scenario: Operator recovery resolves after stability
- **WHEN** a tripped service is recreated or recovered and then runs with no further restarts across at least one check cycle
- **THEN** the breaker marks the service `Resolved`

#### Scenario: Same-container recovery requires stability
- **WHEN** a tripped service shows a restart count that is lower or equal but the container identity is unchanged
- **THEN** the breaker does not mark the service `Resolved` until a clean check cycle confirms stability
