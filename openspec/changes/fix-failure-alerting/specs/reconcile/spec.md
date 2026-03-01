## MODIFIED Requirements

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

A failure at any stage after git sync (stages 3-13) SHALL send a throttled failure alert when `on_failure` is enabled. A failure at the git sync stage (stage 2) SHALL also send a throttled failure alert; because the deploy state file has not been loaded yet, the reconciler SHALL load the state file before sending the alert to ensure throttle state is available.

Lock acquisition failures (stage 1) SHALL NOT send alerts because the failure is transient (another reconciliation is running) and no state context is available.

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

#### Scenario: Git sync failure sends throttled failure alert

- **WHEN** git repository sync fails (network error, auth failure, etc.)
- **AND** `on_failure` is enabled
- **THEN** a throttled failure alert is sent with the sync error as the reason
- **AND** the deploy state file is loaded before alerting to provide throttle state
- **AND** the pipeline aborts without executing subsequent stages

#### Scenario: Lock acquisition failure does not alert

- **WHEN** lock acquisition fails (another reconciliation in progress)
- **THEN** no failure alert is sent
- **AND** the error is logged and returned to the caller
