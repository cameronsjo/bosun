## MODIFIED Requirements

### Requirement: Pipeline Orchestration

The reconciler SHALL execute stages in this fixed order:

1. Acquire lock
2. Git repository sync
3. Reload project config from repo
4. Load deploy state and evaluate skip/circuit-breaker logic
5. Decrypt secrets (SOPS)
6. Render templates (Go text/template + Sprig)
7. Extract declared state from rendered compose
8. Create configuration backup
9. Deploy files (local or remote)
10. Run `docker compose up`
11. Clean up staging directory
12. Record successful deployment in state file
13. Execute post-sync hooks
14. Post-deploy verification (drift check)
15. Release lock

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

## ADDED Requirements

### Requirement: Project Config Reload

After git repository sync, the reconciler SHALL re-read `bosun.yaml` from the
repo working directory and update `PostSyncHooks` and `HookSettleDelay` if the
file has changed. This ensures config changes pushed to the repo take effect
without a daemon restart.

The reload SHALL only update fields not overridden by environment variables.
When `BOSUN_POST_SYNC_HOOKS` is set, the hooks from `bosun.yaml` are ignored.
When `BOSUN_HOOK_SETTLE_DELAY` is set, the settle delay from `bosun.yaml` is
ignored.

If `bosun.yaml` is absent from the repo or fails to parse, the reconciler
SHALL log a warning and keep the existing config values. This preserves the
graceful degradation behavior where reconciliation works without a project
config file.

#### Scenario: Hooks updated from repo config

- **WHEN** a commit adds a new post-sync hook to `bosun.yaml`
- **AND** `BOSUN_POST_SYNC_HOOKS` is NOT set
- **THEN** the new hook is active for this reconciliation cycle
- **AND** a log message indicates the config was reloaded

#### Scenario: Env var override prevents reload

- **WHEN** a commit changes hooks in `bosun.yaml`
- **AND** `BOSUN_POST_SYNC_HOOKS` is set
- **THEN** the hooks from the env var are used (not the file)

#### Scenario: Malformed bosun.yaml keeps existing config

- **WHEN** a commit introduces a YAML syntax error in `bosun.yaml`
- **THEN** a warning is logged
- **AND** the hooks and settle delay from the previous config remain active
- **AND** reconciliation continues normally

#### Scenario: No bosun.yaml in repo

- **WHEN** the repo does not contain `bosun.yaml` or `bosun.yml`
- **THEN** the existing config values are retained
- **AND** no error or warning is logged (absence is expected for repos without project config)
