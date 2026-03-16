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

### Requirement: Per-Target Locking

Each target SHALL have an independent lock file: `<LockDir>/reconcile-<target.Name>.lock`. The implicit default target SHALL use the existing `reconcile.lock` path. Locking SHALL use the same `flock(2)` / `LockFileEx` mechanism as today.

Per-target locking SHALL allow a CLI `bosun reconcile --target=pi` to execute concurrently with a daemon reconciling `unraid`, while preventing two concurrent reconciliations of the same target.

#### Scenario: Per-target lock prevents concurrent reconciliation

- **WHEN** the daemon is reconciling target `unraid`
- **AND** a CLI `bosun reconcile --target=unraid` is run
- **THEN** the CLI fails with "lock already held" for `unraid`
- **AND** a concurrent `bosun reconcile --target=pi` would succeed

#### Scenario: Default target uses legacy lock file

- **WHEN** no `targets:` section is configured
- **THEN** the lock file path is `reconcile.lock` (unchanged from today)

### Requirement: Sequential Target Reconciliation

The daemon SHALL reconcile targets sequentially in the order they appear in the `targets:` list. For each target, the full pipeline executes: lock, git sync (shared), secrets, render, backup, deploy, compose up, health gate, hooks, drift check, state save, unlock.

Git sync SHALL run once per reconciliation cycle (shared across all targets). The git clone/pull result (commit hash, changed files) SHALL be reused for all targets in the cycle.

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

The reconciler SHALL execute stages in this fixed order:

1. Acquire lock (per-target)
2. Git repository sync (shared — runs once per cycle, before target iteration)
3. Load deploy state and evaluate skip/circuit-breaker logic (per-target)
4. Decrypt secrets (shared) and apply per-target scoping
5. Render templates (per-target, to isolated staging directory)
6. Extract declared state from rendered compose (per-target)
7. Create configuration backup (per-target)
8. Deploy files (per-target — local or remote)
9. Run `docker compose up` (per-target)
10. Clean up staging directory (per-target)
11. Critical container health gate (per-target, if configured)
12. Execute post-sync hooks (per-target)
13. Post-deploy verification (per-target — drift check)
14. Record successful deployment in state file (per-target)
15. Release lock (per-target)

A failure at any stage SHALL abort the remaining stages for that target and release its lock. The health gate (stage 11) failing SHALL trigger rollback for that target before aborting. Other targets in the cycle SHALL continue unaffected.

The lock SHALL always be released via defer, even on panic.

When only one target is configured (implicit default), behavior SHALL be identical to the pre-multi-target pipeline.

#### Scenario: Full pipeline succeeds for all targets

- **WHEN** a reconciliation cycle triggers with a new commit and two targets
- **THEN** git sync runs once
- **AND** both targets execute stages 1, 3-15 in order
- **AND** both targets' state files record the deployed commit

#### Scenario: Pipeline aborts on stage failure for one target

- **WHEN** target `unraid` fails during secret decryption
- **THEN** remaining stages for `unraid` are skipped
- **AND** a throttled failure alert is sent for `unraid`
- **AND** `unraid`'s lock is released
- **AND** target `pi` proceeds with its full pipeline

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
