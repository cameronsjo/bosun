## ADDED Requirements

### Requirement: Deploy State Tracking

The reconciler SHALL persist the commit hash of the last successful deployment to
a state file after the full reconcile pipeline completes (git sync, decrypt,
render, backup, deploy, compose up, cleanup).

The state file SHALL be a JSON file containing at minimum the deployed commit hash,
deployment timestamp, and trigger source.

The state file SHALL be written atomically (temp file + rename) to prevent
corruption from interrupted writes.

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
defaulting to the same directory as the lock file (`/var/run/bosun/`).

The state directory SHALL be configurable via the `BOSUN_STATE_DIR` environment
variable.

The daemon SHALL create the state directory on startup if it does not exist.

#### Scenario: Default state directory

- **WHEN** `BOSUN_STATE_DIR` is not set
- **THEN** the state file is written to the lock file's parent directory

#### Scenario: Custom state directory

- **WHEN** `BOSUN_STATE_DIR` is set to `/data/bosun`
- **THEN** the state file is written to `/data/bosun/deploy-state.json`

#### Scenario: State directory created on startup

- **WHEN** the configured state directory does not exist
- **THEN** the daemon creates it with mode 0755 before starting reconciliation
