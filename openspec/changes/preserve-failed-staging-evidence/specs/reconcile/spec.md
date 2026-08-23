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
9. Deploy sync invariant check (see Deploy Sync Invariants)
10. Run `docker compose up`
11. Critical container health gate (if configured)
12. Execute post-sync hooks
13. Post-deploy verification (drift check)
14. Clean up staging directory
15. Record successful deployment in state file
16. Release lock

A failure at any stage SHALL abort the remaining deployment stages and release the
lock. The health gate (stage 11) failing SHALL trigger rollback before aborting.
The invariant check (stage 9) failing SHALL abort before compose up runs; no
rollback is needed because no compose changes have been applied at that point.

After rendering begins, a failure before successful staging cleanup SHALL retain
the current target's rendered staging tree according to the Failed Staging Evidence
Lifecycle requirement. A staging cleanup failure after verification is non-fatal
only when the retained tree is successfully restricted to owner-only access; if
Bosun can neither harden nor delete that tree, the security failure SHALL abort
successful completion.

The lock SHALL always be released via defer, even on panic.

#### Scenario: Full pipeline succeeds

- **WHEN** a reconciliation is triggered and a new commit is available
- **THEN** all stages execute in order
- **AND** staging is removed after health checks and verification succeed and hook execution completes
- **AND** the deploy state file records the deployed commit
- **AND** a success alert is sent

#### Scenario: Pipeline aborts on stage failure

- **WHEN** secret decryption fails
- **THEN** template rendering, backup, deploy, invariant check, and compose stages are skipped
- **AND** any evidence retained from the preceding attempt is not replaced
- **AND** a throttled failure alert is sent
- **AND** the lock is released

#### Scenario: Invariant check aborts before compose

- **WHEN** stage 9 invariants fail
- **THEN** compose up, health gate, hooks, verification, and successful cleanup are skipped
- **AND** the current rendered staging tree is retained securely
- **AND** the lock is released
- **AND** a failure alert is sent
- **AND** the state file is NOT updated

#### Scenario: Dry run mode

- **WHEN** `DryRun` is true
- **THEN** backup, deploy, invariant check, compose up, health gate, post-sync hooks, and post-deploy verification are skipped
- **AND** template rendering still executes to validate templates
- **AND** the rendered staging tree is retained securely as the current evidence slot

#### Scenario: Health gate failure triggers rollback

- **WHEN** compose up succeeds but a critical container fails the health gate
- **THEN** the reconciler triggers rollback to the backup compose files
- **AND** the failed rendered staging tree is retained securely regardless of the rollback outcome
- **AND** the deployment is NOT recorded as successful
- **AND** a failure alert is sent
- **AND** the lock is released

## ADDED Requirements

### Requirement: Failed Staging Evidence Lifecycle

The reconciler SHALL retain the current rendered staging tree in the effective
per-target `StagingDir` when a reconciliation fails after rendering begins. This
includes template-render failure with partial output, declared-state or invariant
failure, deployment failure, health-gate failure, rollback success, rollback
failure, and local post-deploy verification failure.

The retained tree SHALL be treated as secret-bearing plaintext because rendered
templates can interpolate arbitrary values from decrypted SOPS data. Bosun SHALL
restrict retained directories to owner-only access and regular files to owner
read/write, SHALL use `Lstat`-style traversal that does not follow symlinks, and
SHALL NOT log file contents. If permission hardening fails, Bosun SHALL delete the
staging tree instead of knowingly retaining it with broader access. If both
hardening and deletion fail, Bosun SHALL surface a security error and SHALL NOT
report the reconciliation as successful.

Retention SHALL use the existing effective staging path rather than copying the
tree into `BackupDir` or creating timestamped archives. Each target SHALL therefore
have at most one evidence slot. A subsequent attempt SHALL preserve the prior slot
through sync, config reload, secret decryption, and deploy-mode resolution; when
the subsequent render phase begins, its normal clear-before-render operation SHALL
replace only that target's prior slot. No target SHALL clean, harden, replace, or
otherwise mutate another target's effective staging directory.

After the health gate and applicable post-deploy verification succeed and post-sync
hook execution completes, Bosun SHALL remove the current target's staging tree
before reporting successful completion. Post-sync hook errors retain their existing
non-fatal warning semantics. If removal fails, Bosun SHALL apply the same owner-only
hardening fallback before continuing; failure to either harden or delete SHALL be a
security error.

#### Scenario: Health failure preserves the rejected render

- **WHEN** compose up succeeds but the health gate rejects the deployment
- **THEN** the exact rendered staging tree remains at the target's effective `StagingDir`
- **AND** retained directories are owner-only and regular files are owner read/write
- **AND** the retained path, but no file content, is reported for operator diagnosis

#### Scenario: Successful rollback does not erase evidence

- **WHEN** the health gate fails and rollback restores the previous managed tree successfully
- **THEN** the failed render remains in the secured staging evidence slot
- **AND** it can be compared with the restored prior state

#### Scenario: Failed or partial rollback retains evidence

- **WHEN** the health gate fails and rollback also fails or restores only part of the managed tree
- **THEN** the failed render remains in the secured staging evidence slot
- **AND** the rollback error is returned without deleting the evidence

#### Scenario: Post-deploy verification failure preserves evidence

- **WHEN** the health gate passes but local post-deploy verification rejects the deployment
- **THEN** successful staging cleanup has not run
- **AND** the rendered staging tree remains secured for diagnosis

#### Scenario: Next render replaces one bounded slot

- **GIVEN** a target has retained staging evidence from a failed attempt
- **WHEN** a later attempt reaches template rendering
- **THEN** the prior staging tree is cleared before the new render starts
- **AND** no timestamped or sibling evidence directory is accumulated

#### Scenario: Early failure preserves prior evidence

- **GIVEN** a target has retained staging evidence from a failed attempt
- **WHEN** the next attempt fails during sync, config reload, secret decryption, or deploy-mode resolution before rendering begins
- **THEN** the prior staging evidence remains unchanged

#### Scenario: Hardening failure deletes unsafe evidence

- **WHEN** Bosun cannot restrict a retained staging entry to the required owner-only access
- **THEN** Bosun deletes that target's staging tree
- **AND** reports that evidence was discarded without logging its contents
- **AND** if deletion also fails, the reconciliation returns a security error

#### Scenario: Verified success removes staging

- **WHEN** the health gate and applicable post-deploy verification succeed and post-sync hook execution completes
- **THEN** Bosun removes the current target's staging tree
- **AND** only then reports successful completion and records the deployment

#### Scenario: Multi-target evidence remains isolated

- **GIVEN** two named targets have distinct effective staging directories
- **WHEN** the first target fails verification and the second target succeeds
- **THEN** the first target's secured evidence remains available
- **AND** the second target removes only its own staging tree
