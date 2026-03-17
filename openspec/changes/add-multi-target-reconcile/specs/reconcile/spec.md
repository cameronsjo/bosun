## ADDED Requirements

### Requirement: Multi-Target Configuration

The reconciler SHALL support deploying to multiple targets from a single daemon instance. Each target SHALL be described by a named target descriptor containing: target name, host (empty for local), appdata paths, project name, state file path, staging directory, secrets scope, critical containers, post-sync hooks, and deploy sync filters.

Targets SHALL be configured via the `targets:` section in `bosun.yaml`, where each entry is a named target descriptor. When the `targets:` section is absent or empty, the reconciler SHALL synthesize a single implicit target named `default` from the flat config fields, preserving full backwards compatibility.

When both `targets:` and flat target fields (`target_host`, `project_name`, etc.) are present, the reconciler SHALL use `targets:` and log a deprecation warning for the flat fields.

`BOSUN_TARGETS` (JSON array of target descriptors) SHALL override the `targets:` section from `bosun.yaml` when set.

#### Scenario: Multi-target config parsed from YAML

- **WHEN** `bosun.yaml` contains a `targets:` section with entries `unraid` and `pi`
- **THEN** the reconciler creates two target descriptors with the specified per-target configuration
- **AND** each target has independent paths for state, staging, and locking

#### Scenario: Implicit default target from flat config

- **WHEN** `bosun.yaml` has no `targets:` section
- **AND** flat fields `target_host`, `project_name`, etc. are set
- **THEN** the reconciler synthesizes one target named `default` from the flat fields
- **AND** behavior is identical to pre-multi-target versions

#### Scenario: Deprecation warning for mixed config

- **WHEN** `bosun.yaml` has both `targets:` and flat `target_host`
- **THEN** the reconciler logs a deprecation warning
- **AND** uses only the `targets:` section

#### Scenario: Env var overrides targets

- **WHEN** `BOSUN_TARGETS` is set to a JSON array with one target
- **THEN** the reconciler uses the env var targets, ignoring `bosun.yaml` targets

### Requirement: Per-Target Staging Isolation

Each target SHALL have an isolated staging directory derived from its name: `<StagingDir>/<target.Name>/`. Template rendering for a target SHALL write exclusively to that target's staging directory. The staging directory SHALL be created before rendering and cleaned up after deployment completes (success or failure).

This isolation SHALL prevent file collisions when multiple targets are reconciled, even if future versions support parallel execution.

#### Scenario: Isolated staging per target

- **WHEN** targets `unraid` and `pi` are configured
- **AND** reconciliation runs for both sequentially
- **THEN** `unraid` renders to `<StagingDir>/unraid/`
- **AND** `pi` renders to `<StagingDir>/pi/`
- **AND** neither target's staging directory contains the other's files

#### Scenario: Staging cleanup after failure

- **WHEN** target `unraid` fails during template rendering
- **THEN** `<StagingDir>/unraid/` is cleaned up
- **AND** target `pi`'s staging directory is unaffected

### Requirement: Per-Target State Persistence

Each target SHALL have an independent state file tracking its deploy state. The state file path SHALL be derived from the target name: `<StateDir>/deploy-state-<target.Name>.json`. The implicit default target SHALL use the existing `deploy-state.json` path for backwards compatibility.

Per-target state files SHALL track the same fields as today (schema version, last deployed commit, deployment timestamp, deploy count, trigger source, attempt tracking, declared services, drift results) independently per target. A successful deploy on target A SHALL NOT affect target B's state.

#### Scenario: Independent deploy tracking per target

- **WHEN** target `unraid` deploys commit `abc123` successfully
- **AND** target `pi` fails on the same commit
- **THEN** `deploy-state-unraid.json` records `abc123` as last deployed
- **AND** `deploy-state-pi.json` does NOT record `abc123` as last deployed
- **AND** `deploy-state-pi.json` increments the attempt count

#### Scenario: Default target uses legacy state file

- **WHEN** no `targets:` section is configured (implicit default target)
- **THEN** the state file path is `<StateDir>/deploy-state.json` (unchanged from today)

#### Scenario: Circuit breaker independent per target

- **WHEN** target `pi` trips its circuit breaker after 3 failures
- **AND** target `unraid` has been deploying successfully
- **THEN** subsequent reconciliation cycles skip `pi` (circuit breaker)
- **AND** `unraid` continues to deploy normally

### Requirement: Two-Layer Reconciliation Locking

Reconciliation SHALL use a two-layer locking model to protect both the shared git worktree and per-target state.

**Layer 1: Process-wide single-flight gate.** An in-memory mutex SHALL serialize all reconciliation entry points within a process (daemon loop, CLI, webhook triggers). Only one reconciliation cycle SHALL run at a time. Incoming triggers while a cycle is running SHALL set a dirty flag to coalesce a follow-up run after the current cycle completes, rather than queuing or starting concurrently.

**Layer 2: Per-target file locks.** Each target SHALL have an independent lock file: `<LockDir>/reconcile-<target.Name>.lock`. The implicit default target SHALL use the existing `reconcile.lock` path. File locking SHALL use `flock(2)` / `LockFileEx` as today. Per-target file locks SHALL be acquired inside the single-flight gate, after the process-wide gate is held.

The single-flight gate protects the shared git worktree from concurrent access. Per-target file locks protect against cross-process overlap (e.g., two separate bosun processes sharing a lock directory).

#### Scenario: Single-flight gate serializes daemon and CLI

- **WHEN** the daemon is mid-cycle reconciling target `unraid`
- **AND** a CLI `bosun reconcile --target=pi` is invoked
- **THEN** the CLI blocks on the single-flight gate until the daemon cycle completes
- **AND** then the CLI acquires the gate and the per-target lock for `pi`

#### Scenario: Dirty-flag coalesces concurrent triggers

- **WHEN** the daemon is mid-cycle
- **AND** a webhook trigger arrives
- **THEN** the trigger sets the dirty flag and returns immediately
- **AND** after the current cycle completes, the daemon runs another cycle

#### Scenario: Per-target file lock prevents cross-process overlap

- **WHEN** process A holds the per-target lock for `unraid`
- **AND** process B attempts to reconcile `unraid`
- **THEN** process B fails with "lock already held" for `unraid`

#### Scenario: Default target uses legacy lock file

- **WHEN** no `targets:` section is configured
- **THEN** the lock file path is `reconcile.lock` (unchanged from today)

### Requirement: Sequential Target Reconciliation

The daemon SHALL reconcile targets sequentially in the order they appear in the `targets:` list. Git sync and secret decryption are cycle-level stages that run once before target iteration (see Pipeline Orchestration). For each target, the per-target pipeline executes: acquire lock, apply secrets scoping, render, backup, deploy, compose up, health gate, hooks, drift check, state save, release lock.

The git clone/pull result (commit hash, changed files) SHALL be reused for all targets in the cycle.

A failure on one target SHALL be logged and alerted, then the next target proceeds. The daemon SHALL NOT abort the cycle on a per-target failure.

#### Scenario: Two targets, one fails, second proceeds

- **WHEN** targets `unraid` and `pi` are configured in that order
- **AND** `unraid` fails during compose up
- **THEN** a failure alert is sent for `unraid`
- **AND** reconciliation proceeds to `pi`
- **AND** `pi` completes successfully with its own state file updated

#### Scenario: Git sync shared across targets

- **WHEN** targets `unraid` and `pi` are configured
- **AND** a new commit is available
- **THEN** git clone/pull runs once at the start of the cycle
- **AND** both targets see the same commit hash and changed files

#### Scenario: All targets skipped when no new commit

- **WHEN** all targets' state files record the current HEAD commit
- **AND** `force` is false
- **THEN** all targets are skipped
- **AND** no deploy activity occurs

### Requirement: Per-Target Secrets Scoping

The reconciler SHALL support per-target secrets via a naming convention in the decrypted secrets map. Top-level keys are shared across all targets. Keys under `targets.<target-name>.*` are scoped to the named target and SHALL override same-named top-level keys when rendering templates for that target.

When a target has a `SecretsScope` configured, the reconciler SHALL merge `targets.<SecretsScope>.*` over the top-level keys. When `SecretsScope` is empty, the target receives only shared (top-level) secrets.

Overridden keys SHALL be logged at debug level to aid troubleshooting.

#### Scenario: Target-scoped secret overrides shared secret

- **WHEN** secrets contain `db_password: shared` and `targets.unraid.db_password: secret1`
- **AND** target `unraid` has `SecretsScope: "unraid"`
- **THEN** templates for `unraid` resolve `{{ .db_password }}` to `secret1`

#### Scenario: Target without scope gets shared secrets only

- **WHEN** secrets contain `db_password: shared` and `targets.pi.db_password: secret2`
- **AND** target `unraid` has no `SecretsScope`
- **THEN** templates for `unraid` resolve `{{ .db_password }}` to `shared`

#### Scenario: No target-scoped secrets in map

- **WHEN** secrets contain only top-level keys (no `targets:` section)
- **THEN** all targets receive the same secrets
- **AND** behavior is identical to pre-multi-target versions

## MODIFIED Requirements

### Requirement: Pipeline Orchestration

The reconciler SHALL execute a two-phase pipeline: cycle-level stages run once, then per-target stages run for each target sequentially.

**Cycle-level stages** (run once per reconciliation cycle):

1. Acquire process-wide single-flight gate
2. Git repository sync (clone or pull)
3. Decrypt secrets (shared SOPS files)

**Per-target stages** (run for each target in list order):

4. Acquire per-target file lock
5. Load deploy state and evaluate skip/circuit-breaker logic
6. Apply per-target secrets scoping
7. Render templates (to isolated staging directory)
8. Extract declared state from rendered compose
9. Create configuration backup
10. Deploy files (local or remote)
11. Run `docker compose up`
12. Clean up staging directory
13. Critical container health gate (if configured)
14. Execute post-sync hooks
15. Post-deploy verification (drift check)
16. Record successful deployment in state file
17. Release per-target file lock

**Post-cycle:** Release process-wide single-flight gate. If dirty flag is set, start another cycle.

A failure at any per-target stage SHALL abort the remaining stages for that target and release its per-target lock. The health gate (stage 13) failing SHALL trigger rollback for that target before aborting. Other targets in the cycle SHALL continue unaffected.

Per-target locks SHALL always be released via defer, even on panic. The single-flight gate SHALL always be released after all targets complete.

When only one target is configured (implicit default), behavior SHALL be identical to the pre-multi-target pipeline.

#### Scenario: Full pipeline succeeds for all targets

- **WHEN** a reconciliation cycle triggers with a new commit and two targets
- **THEN** the single-flight gate is acquired and git sync runs once
- **AND** secrets are decrypted once
- **AND** both targets execute per-target stages 4–17 in order
- **AND** both targets' state files record the deployed commit
- **AND** the single-flight gate is released

#### Scenario: Pipeline aborts on stage failure for one target

- **WHEN** target `unraid` fails during template rendering
- **THEN** remaining per-target stages for `unraid` are skipped
- **AND** a throttled failure alert is sent for `unraid`
- **AND** `unraid`'s per-target lock is released
- **AND** target `pi` proceeds with its full per-target pipeline

#### Scenario: Dry run mode with multiple targets

- **WHEN** `DryRun` is true and two targets are configured
- **THEN** both targets skip backup, deploy, compose up, health gate, hooks, and verification
- **AND** template rendering executes for both targets to validate templates

#### Scenario: Health gate failure triggers rollback for one target only

- **WHEN** target `pi` passes compose up but fails the health gate
- **THEN** `pi` triggers rollback to its backup compose files
- **AND** `pi`'s deployment is NOT recorded as successful
- **AND** other targets are unaffected

### Requirement: Deploy State Persistence

The reconciler SHALL persist deployment state to a JSON file per target containing: schema version, last deployed commit hash, deployment timestamp, deploy count, trigger source, last attempted commit, attempt count, last alerted attempt, declared services snapshot, drift check timestamp, and drift items.

The state file path SHALL be per-target: `<StateDir>/deploy-state-<target.Name>.json`. The implicit default target SHALL use `deploy-state.json` for backwards compatibility.

The state file SHALL include a `schema_version` field (currently `2`) to support future schema evolution without breaking existing deployments.

The state file SHALL be written atomically using the pattern: write temp file (in same directory as target) -> fsync temp -> rename -> fsync directory.

#### Scenario: State file written after successful deploy

- **WHEN** a reconcile pipeline completes all stages successfully for target `unraid`
- **THEN** `deploy-state-unraid.json` is written with the current commit hash, timestamp, source, and declared services
- **AND** the attempt count is reset to 0

#### Scenario: State file not updated on failure

- **WHEN** a reconcile pipeline fails for target `pi`
- **THEN** `deploy-state-pi.json`'s `last_deployed_commit` is NOT updated
- **AND** the attempt tracking fields ARE updated

#### Scenario: Missing state file treated as never deployed

- **WHEN** the state file for target `unraid` does not exist
- **THEN** the reconciler returns a zero-value state with the current schema version
- **AND** the full pipeline executes for `unraid` regardless of git HEAD

#### Scenario: Corrupt state file treated as never deployed

- **WHEN** the state file for target `pi` exists but contains invalid JSON
- **THEN** the reconciler logs a warning and returns a zero-value state
- **AND** the full pipeline executes for `pi`

### Requirement: Drift CLI

A `bosun drift` command SHALL display the current drift status. Without flags, it SHALL read the cached drift results from the deploy state file(s).

When `--target` is specified, only the named target's drift is shown. When no `--target` is specified and multiple targets are configured, drift for all targets SHALL be displayed with target name headers.

The `--live` flag SHALL perform a fresh drift check. For local targets, it queries Docker. For remote targets, live drift is not supported (logged as a warning).

The `--json` flag SHALL output machine-readable JSON. For multiple targets, the output SHALL be a JSON object keyed by target name.

When no deploy state file exists for a target, the command SHALL indicate that no deployments have been recorded for that target.

#### Scenario: Cached drift status for all targets

- **WHEN** `bosun drift` is run without `--target` and two targets are configured
- **THEN** it displays drift results for both targets with target name headers
- **AND** shows the timestamp of each target's last drift check

#### Scenario: Cached drift for single target

- **WHEN** `bosun drift --target=unraid` is run
- **THEN** it reads only `deploy-state-unraid.json` and displays its drift results

#### Scenario: Live drift check

- **WHEN** `bosun drift --live` is run for a local target
- **THEN** it reads declared state from the target's state file
- **AND** queries Docker for actual state
- **AND** displays the comparison result

#### Scenario: No previous deployment for one target

- **WHEN** `bosun drift` is run and target `pi` has no state file
- **THEN** it prints a message indicating no deployments for `pi`
- **AND** still shows drift for other targets that have state
