## ADDED Requirements

### Requirement: Deploy State Tracking

The reconciler SHALL persist the commit hash of the last successful deployment to
a state file after the full reconcile pipeline completes (git sync, decrypt,
render, backup, deploy, compose up, cleanup).

The state file SHALL be a JSON file containing at minimum: schema version, deployed
commit hash, deployment timestamp, trigger source, last attempted commit, and
attempt count.

The state file SHALL include a `schema_version` field (initially `1`) to support
future schema evolution without breaking existing deployments.

The state file SHALL be written atomically using the pattern: write temp file
(in same directory as target) → fsync temp → rename → fsync directory.

#### Scenario: State file written after successful deploy

- **WHEN** a reconcile pipeline completes all stages successfully
- **THEN** a state file is written with the current commit hash, timestamp, and source
- **AND** subsequent reconcile runs with the same commit skip the pipeline

#### Scenario: State file not written after failed deploy

- **WHEN** a reconcile pipeline fails at any stage (decrypt, render, deploy, compose up)
- **THEN** the state file is NOT updated
- **AND** the next reconcile run re-executes the full pipeline

#### Scenario: Missing state file treated as never deployed

- **WHEN** the state file does not exist (fresh install or lost volume)
- **THEN** the reconciler treats the system as never deployed
- **AND** the full pipeline executes regardless of git HEAD

#### Scenario: Corrupt state file treated as never deployed

- **WHEN** the state file exists but contains invalid JSON
- **THEN** the reconciler logs a warning and treats the system as never deployed
- **AND** the full pipeline executes

### Requirement: Attempt Tracking and Circuit Breaker

The reconciler SHALL track the last attempted commit and consecutive failure count
in the state file to prevent infinite retry loops on commits that break the pipeline
(bad templates, invalid compose files, etc.).

Before executing the pipeline, the reconciler SHALL update `last_attempted_commit`
and increment `attempt_count` in the state file. If the commit changes, the count
SHALL reset to 1.

After 3 consecutive failures on the same commit, the reconciler SHALL stop retrying
and require either a new commit or `--force` to resume. This SHALL be surfaced
through health checks as a "degraded" state.

#### Scenario: Bad commit triggers circuit breaker

- **WHEN** a commit causes the pipeline to fail 3 consecutive times
- **THEN** the reconciler stops retrying on subsequent triggers
- **AND** the health endpoint reports "degraded" status
- **AND** a log entry at ERROR level identifies the failing commit

#### Scenario: New commit resets circuit breaker

- **WHEN** a new commit is pushed after a circuit breaker trip
- **THEN** the attempt count resets to 1
- **AND** the pipeline executes normally

#### Scenario: Force flag overrides circuit breaker

- **WHEN** a trigger with `force=true` is received while circuit breaker is tripped
- **THEN** the pipeline executes regardless of attempt count

### Requirement: State-Based Skip Logic

The reconciler SHALL compare the current git HEAD commit to the last deployed
commit from the state file to determine whether to skip the pipeline, replacing
the previous git-diff-based skip logic.

The reconciler SHALL proceed with the full pipeline when the state file commit
does not match the current HEAD, even if `git.Sync()` reports no changes.

#### Scenario: Interrupted deployment detected via state mismatch

- **WHEN** a previous reconcile pulled commit X but was interrupted before completion
- **AND** the state file records the previous commit (or is absent)
- **AND** git HEAD is at commit X (no new changes)
- **THEN** the reconciler detects the mismatch and runs the full pipeline

#### Scenario: Self-restart during compose up

- **WHEN** `docker compose up` restarts bosun's own container mid-pipeline
- **AND** the new bosun instance starts and runs initial reconciliation
- **THEN** the state file does not reflect the current commit
- **AND** the full pipeline re-executes
- **AND** `docker compose up` is idempotent (no-op for already-running containers)
- **AND** the state file is written after success, breaking the retry loop

### Requirement: Force Trigger Override

All trigger paths (Unix socket, TCP, HTTP webhook, HTTP API, CLI) SHALL support a
`force` parameter that bypasses the state-based skip logic and forces a full
pipeline execution regardless of state file contents.

The force flag SHALL be per-invocation, not a daemon-wide configuration.

#### Scenario: Force flag via socket trigger

- **WHEN** a trigger request is sent to `/trigger` with `{"force": true}`
- **THEN** the reconciler runs the full pipeline even if state matches current HEAD

#### Scenario: Force flag via CLI

- **WHEN** `bosun trigger --force` is executed
- **THEN** the reconciler runs the full pipeline even if state matches current HEAD

#### Scenario: Normal trigger respects state

- **WHEN** a trigger request is sent without force flag
- **AND** the state file commit matches current HEAD
- **THEN** the reconciler skips the pipeline and returns success

### Requirement: State Directory Configuration

The daemon SHALL support a configurable state directory for the deploy state file,
defaulting to `/var/lib/bosun/` (FHS-compliant persistent application state).

The state directory SHALL be configurable via the `BOSUN_STATE_DIR` environment
variable.

The daemon SHALL create the state directory on startup if it does not exist.

The daemon SHALL log a startup warning if the state directory appears to be on a
tmpfs mount (`/var/run/`, `/tmp/`, or detected tmpfs filesystem), as this would
defeat the purpose of persistent state tracking.

#### Scenario: Default state directory

- **WHEN** `BOSUN_STATE_DIR` is not set
- **THEN** the state file is written to `/var/lib/bosun/deploy-state.json`

#### Scenario: Custom state directory

- **WHEN** `BOSUN_STATE_DIR` is set to `/data/bosun`
- **THEN** the state file is written to `/data/bosun/deploy-state.json`

#### Scenario: State directory created on startup

- **WHEN** the configured state directory does not exist
- **THEN** the daemon creates it with mode 0755 before starting reconciliation

#### Scenario: Tmpfs warning on startup

- **WHEN** the state directory is on a tmpfs mount
- **THEN** the daemon logs a WARNING indicating state may be lost on container recreation
- **AND** the daemon continues normally (does not block startup)
