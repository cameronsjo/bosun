## MODIFIED Requirements

### Requirement: Pipeline Orchestration

Before any target pipeline starts, Bosun SHALL canonicalize and validate every
effective target staging path, then harden-or-delete every pre-existing staging
slot. Equal or ancestor/descendant staging paths SHALL reject the complete target
set. If any pre-existing slot can be neither protected nor deleted, Bosun SHALL
fail the cycle before Git sync, secret decryption, or target execution.

For every valid target, the reconciler SHALL execute stages in this fixed order:

1. Acquire lock
2. Git repository sync
3. Load deploy state and evaluate skip/circuit-breaker logic
4. Decrypt secrets (SOPS)
5. Render templates (Go text/template + Sprig)
6. Extract declared state from rendered compose
7. Pull images (`docker compose pull`)
8. Create configuration backup
9. Deploy files (local or remote)
10. Deploy sync invariant check (see Deploy Sync Invariants)
11. Run `docker compose up`
12. Configured health gate (if enabled; see Critical Container Health Gate and Health Gate Scope)
13. Execute post-sync hooks
14. Post-deploy verification (drift check)
15. Clean up staging directory
16. Record successful deployment in state file
17. Release lock

A fatal failure at any stage SHALL abort the current target's remaining deployment
stages and release its lock, but while the shared cycle context remains live it
SHALL NOT prevent a valid sibling target from running. If the shared cycle context
is canceled or its deadline expires, target iteration SHALL stop as required by
the canonical `Non-Live Cycle Context Stops Target Iteration` requirement. The
configured health gate (stage 12) failing SHALL trigger rollback before aborting
that target. The invariant check (stage 10) failing SHALL abort that target before
compose up runs; no rollback is needed because no compose changes have been applied
at that point. After all eligible targets complete, the overall cycle SHALL report
failure if any target failed, without undoing successful siblings. Failure and
success alerts SHALL identify the applicable target, and the process-wide cycle
lock SHALL be released only after the target loop completes.

Before writing decrypted render output, Bosun SHALL prepare the current target's
private staging slot according to the Failed Staging Evidence Lifecycle
requirement. After preparation begins, every normal or panic exit before successful
staging cleanup SHALL run that lifecycle's harden-or-delete finalizer. A staging
cleanup failure after verification is non-fatal only when the retained tree is
successfully proven owner-only; if Bosun can neither harden nor delete that tree,
the security failure SHALL abort successful completion.

The lock SHALL always be released via defer, even on panic.

#### Scenario: Full pipeline succeeds

- **WHEN** a reconciliation is triggered and a new commit is available
- **THEN** all stages execute in order
- **AND** staging is removed after health checks and verification succeed and hook execution completes
- **AND** the deploy state file records the deployed commit
- **AND** a success alert is sent

#### Scenario: Pipeline aborts on stage failure

- **WHEN** secret decryption fails
- **THEN** template rendering, image pull, backup, deploy, invariant check, and compose stages are skipped
- **AND** any evidence retained from the preceding attempt is not replaced
- **AND** a throttled failure alert is sent
- **AND** the lock is released

#### Scenario: Invariant check aborts before compose

- **WHEN** stage 10 invariants fail
- **THEN** compose up, health gate, hooks, verification, and successful cleanup are skipped
- **AND** the current rendered staging tree is retained securely
- **AND** the lock is released
- **AND** a failure alert is sent
- **AND** the state file is NOT updated

#### Scenario: Dry run mode

- **WHEN** `DryRun` is true
- **THEN** image pull, backup, deploy, invariant check, compose up, health gate, post-sync hooks, and post-deploy verification are skipped
- **AND** template rendering still executes to validate templates
- **AND** normal staging cleanup is skipped
- **AND** Bosun hardens every rendered directory to `0700` and regular file to `0600` before returning
- **AND** the rendered staging tree is retained securely as the current evidence slot
- **AND** the replacement and retention outcome is reported without rendered content

#### Scenario: One target failure does not suppress siblings

- **GIVEN** a valid target set contains targets `unraid` and `pi`
- **WHEN** `unraid` fails after rendering and `pi` succeeds
- **AND** the shared cycle context remains live
- **THEN** `unraid` aborts its remaining stages and retains secured evidence
- **AND** `pi` completes and removes only its own staging slot
- **AND** the overall cycle reports failure after both targets finish
- **AND** alerts identify their respective target outcomes

#### Scenario: Configured health gate failure triggers rollback

- **WHEN** compose up succeeds but the configured health gate rejects the deployment
- **THEN** the reconciler triggers rollback to the backup compose files
- **AND** the failed rendered staging tree is retained securely regardless of the rollback outcome
- **AND** the deployment is NOT recorded as successful
- **AND** a failure alert is sent
- **AND** the lock is released

#### Scenario: Image pull runs before backup

- **WHEN** a reconciliation triggers with new images to pull
- **THEN** the image pull stage completes before the backup stage begins
- **AND** if the pull fails, no backup is created
- **AND** the rendered staging tree is retained securely

## ADDED Requirements

### Requirement: Image Pre-Pull

The reconciler SHALL run `docker compose pull` as an explicit pipeline stage before creating a backup and running `docker compose up`. This separates slow image downloads from container startup, providing independent timeouts and clearer error diagnostics.

The pre-pull stage SHALL use the same compose file list and project name as the subsequent `docker compose up` stage.

The pre-pull stage SHALL apply `ImagePullTimeout` (default 15 minutes) independently from the compose up timeout. This allows large or numerous images to pull without consuming the compose up timeout budget.

Pre-pull failures SHALL abort the pipeline without triggering rollback. Since no containers have been started with new configuration, the system remains in its previous state and rollback would be a no-op.

Pre-pull SHALL be skipped when `DryRun` is true, when no compose files are found, or for remote deployments (where compose commands execute over SSH).

#### Scenario: Successful image pull

- **WHEN** compose files reference images not yet cached locally
- **THEN** the pre-pull stage runs `docker compose pull` with the rendered compose files
- **AND** logs the pull duration and file count at INFO level
- **AND** the pipeline proceeds to the backup stage

#### Scenario: Image pull timeout

- **WHEN** image pulls exceed `ImagePullTimeout`
- **THEN** the pre-pull stage fails with a timeout error
- **AND** the error message includes the timeout duration
- **AND** the pipeline aborts without creating a backup or running compose up
- **AND** a throttled failure alert is sent

#### Scenario: Image pull failure (auth or missing image)

- **WHEN** `docker compose pull` fails due to authentication or a missing image
- **THEN** the pre-pull stage fails with the compose stderr output
- **AND** the error message preserves the original compose error for diagnosis
- **AND** the pipeline aborts without rollback

#### Scenario: All images already cached

- **WHEN** all referenced images are already present locally
- **THEN** the pre-pull stage completes quickly (seconds)
- **AND** logs success with a short duration
- **AND** the pipeline proceeds normally

#### Scenario: Pre-pull skipped in dry run

- **WHEN** `DryRun` is true
- **THEN** the pre-pull stage is skipped
- **AND** no `docker compose pull` command is executed

#### Scenario: Pre-pull skipped when no compose files

- **WHEN** no compose files are found in the rendered output
- **THEN** the pre-pull stage is skipped without error

#### Scenario: Pre-pull skipped for remote deploy

- **WHEN** the deployment target is a remote host
- **THEN** the pre-pull stage is skipped
- **AND** the remote `docker compose up` handles image pulling as before

### Requirement: Configurable Phase Timeouts

The reconciler SHALL support independent, configurable timeouts for the image pull phase and the compose up phase.

`BOSUN_IMAGE_PULL_TIMEOUT` SHALL configure the timeout for the `docker compose pull` stage. The default SHALL be 15 minutes. The value SHALL accept Go duration strings (e.g., `15m`, `30m`, `1h`) or plain seconds.

`BOSUN_COMPOSE_UP_TIMEOUT` SHALL configure the timeout for the `docker compose up` stage. The default SHALL be 5 minutes. The value SHALL accept Go duration strings or plain seconds.

These timeouts SHALL replace the existing single `ComposeUpTimeout` constant for their respective phases. When neither env var is set, the system SHALL use the defaults (15m pull, 5m up).

#### Scenario: Default timeouts applied

- **WHEN** neither `BOSUN_IMAGE_PULL_TIMEOUT` nor `BOSUN_COMPOSE_UP_TIMEOUT` is set
- **THEN** the image pull phase uses 15-minute timeout
- **AND** the compose up phase uses 5-minute timeout

#### Scenario: Custom pull timeout via env var

- **WHEN** `BOSUN_IMAGE_PULL_TIMEOUT` is set to `30m`
- **THEN** the image pull phase uses 30-minute timeout
- **AND** the compose up phase uses its default (5 minutes)

#### Scenario: Custom compose up timeout via env var

- **WHEN** `BOSUN_COMPOSE_UP_TIMEOUT` is set to `2m`
- **THEN** the compose up phase uses 2-minute timeout
- **AND** the image pull phase uses its default (15 minutes)

#### Scenario: Invalid timeout value logged and skipped

- **WHEN** `BOSUN_IMAGE_PULL_TIMEOUT` is set to an invalid value (e.g., `abc`)
- **THEN** a warning is logged with the invalid value
- **AND** the default timeout is used

#### Scenario: Daemon parses timeout env vars

- **WHEN** the daemon starts with `BOSUN_IMAGE_PULL_TIMEOUT=20m` and `BOSUN_COMPOSE_UP_TIMEOUT=3m`
- **THEN** the daemon config contains the parsed timeout values
- **AND** reconciliation runs use the configured timeouts

#### Scenario: CLI reconcile uses default timeouts

- **WHEN** `bosun reconcile` runs without timeout env vars
- **THEN** the image pull phase uses the 15-minute default
- **AND** the compose up phase uses the 5-minute default
