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
9. Run `docker compose up`
10. Clean up staging directory
11. Record successful deployment in state file
12. Execute post-sync hooks
13. Post-deploy verification (drift check)
14. Release lock

A failure at any stage SHALL abort the remaining stages and release the lock.
The lock SHALL always be released via defer, even on panic.

#### Scenario: Full pipeline succeeds

- **WHEN** a reconciliation is triggered and a new commit is available
- **THEN** all stages execute in order
- **AND** the deploy state file records the deployed commit
- **AND** a success alert is sent

#### Scenario: Pipeline aborts on stage failure

- **WHEN** secret decryption fails
- **THEN** template rendering, backup, deploy, and compose stages are skipped
- **AND** a throttled failure alert is sent
- **AND** the lock is released

#### Scenario: Dry run mode

- **WHEN** `DryRun` is true
- **THEN** backup, deploy, compose up, post-sync hooks, and post-deploy verification are skipped
- **AND** template rendering still executes to validate templates

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

SSH host key verification SHALL use known_hosts files (`BOSUN_SSH_KNOWN_HOSTS`,
`~/.ssh/known_hosts`, `/config/known_hosts`), falling back to insecure mode
with a warning when no known_hosts file is found. The `BOSUN_SSH_INSECURE_HOST_KEY`
environment variable SHALL disable verification entirely.

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

Old backups SHALL be pruned to retain only the most recent N backups
(configurable via `BackupsToKeep`, default 5).

Backup failures SHALL log a warning but SHALL NOT abort the deployment pipeline.

The last backup path SHALL be stored for potential rollback during compose up.

#### Scenario: Local backup created and verified

- **WHEN** a local deployment runs with existing config files
- **THEN** a timestamped tar.gz is created containing the config paths
- **AND** the archive is verified to be non-empty and readable

#### Scenario: Remote backup via SSH

- **WHEN** a remote deployment runs
- **THEN** a tar command runs over SSH to create the archive
- **AND** transient SSH errors are retried with exponential backoff

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

A configurable `ComposeUpTimeout` (default 10 minutes) SHALL apply to compose
operations.

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
the drift status in the state file and logs.

The drift check interval SHALL be configurable via `BOSUN_DRIFT_INTERVAL`
environment variable. Setting it to 0 SHALL disable periodic drift checks.

#### Scenario: Periodic drift check detects crash

- **WHEN** a container crashes between reconciliations
- **AND** the next periodic drift check runs
- **THEN** the drift is detected and logged at WARN level
- **AND** the deploy state file is updated with the drift

#### Scenario: Periodic drift checks disabled

- **WHEN** `BOSUN_DRIFT_INTERVAL` is set to 0
- **THEN** no periodic drift checks run
- **AND** drift is only checked after deployments

### Requirement: Drift Status Persistence

The deploy state file SHALL include drift check results: `drift_checked_at`
timestamp and `drift_items` list (service name, drift type, declared value,
actual value).

The drift fields SHALL be updated after every drift check (post-deploy and
periodic), not only after deployments.

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

After a successful deployment, the reconciler SHALL diff the previous and current
commits, match changed files against hook glob patterns, and execute matching
actions. Each container SHALL be restarted at most once per deployment, even if
multiple patterns match.

Hooks SHALL only execute when a Docker client is available, dry run is false, hooks
are configured, and a previous commit exists (not on first deploy).

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
