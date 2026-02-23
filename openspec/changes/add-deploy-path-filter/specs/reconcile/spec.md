## ADDED Requirements

### Requirement: Path-Aware Deploy Skipping

The reconciler SHALL support a `deploy_paths` allowlist that filters commits by
changed file paths before executing the deployment pipeline. When configured and
no changed files match the allowlist, the reconciler SHALL skip the pipeline and
record the commit as deployed.

`deploy_paths` SHALL be an ordered list of glob patterns using the same matching
semantics as post-sync hook patterns (`matchGlob`: `**` for recursive directory
matching, `filepath.Match` for simple globs).

The path check SHALL execute after config reload and state load, but before the
circuit breaker. This ensures `deploy_paths` is fresh from the repo AND avoids
incrementing the circuit breaker attempt count for skipped commits.

When a commit is skipped via path filtering, the reconciler SHALL update the
deploy state file with the current commit hash, timestamp, and source, marking
it as "deployed" to prevent re-evaluation on the next poll cycle. The deploy
count SHALL NOT be incremented for skipped commits.

When `deploy_paths` is empty or not configured, the reconciler SHALL execute the
full pipeline unconditionally (existing behavior preserved).

`deploy_paths` SHALL be configurable via `bosun.yaml` and overridable via the
`BOSUN_DEPLOY_PATHS` environment variable (JSON string array). The env var
SHALL completely replace the config file value (same precedence model as
`BOSUN_POST_SYNC_HOOKS`). When set via env var, repo config reload SHALL NOT
update `deploy_paths`.

#### Scenario: Docs-only commit skipped

- **WHEN** `deploy_paths` is configured with `["unraid/**", "infrastructure/**"]`
- **AND** a commit only changes `docs/README.md` and `.beads/issues/task-1.jsonl`
- **THEN** the reconciler logs "No deploy-relevant files changed, skipping"
- **AND** the commit is recorded as deployed in the state file
- **AND** the deployment pipeline (decrypt, render, deploy, compose up) does not execute

#### Scenario: Infrastructure commit triggers full pipeline

- **WHEN** `deploy_paths` is configured with `["unraid/**"]`
- **AND** a commit changes `unraid/appdata/traefik/dynamic.yml`
- **THEN** the full pipeline executes normally

#### Scenario: Mixed commit triggers full pipeline

- **WHEN** `deploy_paths` is configured with `["unraid/**"]`
- **AND** a commit changes both `docs/README.md` and `unraid/compose/core.yml`
- **THEN** the full pipeline executes (at least one file matches)

#### Scenario: DiffFiles failure falls through to full deploy

- **WHEN** `deploy_paths` is configured
- **AND** `DiffFiles` fails (e.g., shallow clone lacks previous commit)
- **THEN** a warning is logged
- **AND** the full pipeline executes (safe fallback)

#### Scenario: Force flag bypasses path check

- **WHEN** `deploy_paths` is configured
- **AND** a trigger arrives with `force=true`
- **THEN** the path check is skipped entirely
- **AND** the full pipeline executes regardless of changed files

#### Scenario: Unconfigured deploy_paths preserves existing behavior

- **WHEN** `deploy_paths` is empty or not set
- **THEN** the reconciler executes the full pipeline for every commit (no path filtering)

#### Scenario: Env var override replaces config file

- **WHEN** `BOSUN_DEPLOY_PATHS` env var is set to `["infra/**"]`
- **AND** `bosun.yaml` contains `deploy_paths: ["unraid/**"]`
- **THEN** the reconciler uses `["infra/**"]` (env var wins)
- **AND** repo config reload does not update deploy_paths

#### Scenario: Repo config reload updates deploy_paths

- **WHEN** `deploy_paths` was loaded from `bosun.yaml` (not env var)
- **AND** the repo's `bosun.yaml` is updated with new patterns during git pull
- **THEN** the config reloader picks up the new patterns
- **AND** the new patterns take effect for the current reconciliation

## MODIFIED Requirements

### Requirement: Pipeline Orchestration

The reconciler SHALL execute stages in this fixed order:

1. Acquire lock
2. Git repository sync
3. Reload project config from repo
4. Load deploy state and evaluate skip/circuit-breaker logic
5. **Path-aware skip: if `deploy_paths` configured, diff changed files and skip if none match**
6. Decrypt secrets (SOPS)
7. Render templates (Go text/template + Sprig)
8. Extract declared state from rendered compose
9. Create configuration backup
10. Deploy files (local or remote)
11. Run `docker compose up`
12. Clean up staging directory
13. Record successful deployment in state file
14. Execute post-sync hooks
15. Post-deploy verification (drift check)
16. Release lock

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

#### Scenario: Path-aware skip bypasses pipeline

- **WHEN** `deploy_paths` is configured and no changed files match
- **THEN** the commit is recorded as deployed
- **AND** decrypt, render, deploy, compose, hooks, and verification stages are skipped
- **AND** the circuit breaker attempt count is not incremented
