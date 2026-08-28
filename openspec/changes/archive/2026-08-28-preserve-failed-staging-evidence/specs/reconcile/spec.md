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
7. Create configuration backup
8. Deploy files (local or remote)
9. Deploy sync invariant check (see Deploy Sync Invariants)
10. Run `docker compose up`
11. Configured health gate (if enabled; see Critical Container Health Gate and Health Gate Scope)
12. Execute post-sync hooks
13. Post-deploy verification (drift check)
14. Clean up staging directory
15. Record successful deployment in state file
16. Release lock

A fatal failure at any stage SHALL abort the current target's remaining deployment
stages and release its lock, but while the shared cycle context remains live it
SHALL NOT prevent a valid sibling target from running. If the shared cycle context
is canceled or its deadline expires, target iteration SHALL stop as required by
the canonical `Non-Live Cycle Context Stops Target Iteration` requirement. The
configured health gate (stage 11) failing SHALL trigger rollback before aborting
that target. The invariant check (stage 9) failing SHALL abort that target before
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

## ADDED Requirements

### Requirement: Failed Staging Evidence Lifecycle

The reconciler SHALL retain the current rendered staging tree in the effective
per-target `StagingDir` when a reconciliation fails after rendering begins. This
includes template-render failure with partial output, declared-state or invariant
failure, backup or rollback-anchor failure, pre-deploy state-persistence failure,
file-deployment or compose-up failure, health-gate failure, rollback success,
rollback failure, and local post-deploy verification failure.

The staging tree SHALL be treated as secret-bearing plaintext for its entire
lifetime because rendered templates can interpolate arbitrary values from decrypted
SOPS data. Before any decrypted output is written, Bosun SHALL create the effective
staging root with permission bits `0700`, and every payload temp file SHALL remain
private until atomic rename. Active descendants SHALL retain the modes required by
the existing template-rendering and deploy contracts; local copy and remote tar MUST
NOT turn staging confidentiality into a destination-mode regression. Bosun SHALL
verify the private root boundary and safe entry types before backup, deploy, or any
later stage may consume the tree. When evidence is retained, every directory SHALL
have permission bits `0700` and every regular file SHALL have permission bits
`0600`.

Lifecycle inspection and mutation SHALL start at the effective `StagingDir`, remain
confined to that root, and use no-follow operations. The root and every descendant
SHALL be a real directory or regular file as appropriate. A symlink, socket, device,
FIFO, path traversal outside the effective root, or entry replaced between
inspection and protection SHALL fail verification; Bosun SHALL NOT follow it or log
its contents or link target. On any preparation, verification, or hardening failure,
Bosun SHALL delete only that target's staging tree without following links. If both
protection and deletion fail, Bosun SHALL surface a security error and SHALL NOT
report the reconciliation as successful.

Retention SHALL use the existing effective staging path rather than copying the
tree into `BackupDir` or creating timestamped archives. Each target SHALL therefore
have at most one evidence slot. A subsequent attempt SHALL preserve the prior slot
through sync, config reload, secret decryption, and deploy-mode resolution. Only
entry into the next render phase clears that target's slot. Bosun SHALL not write
decrypted output unless that prior slot was completely removed and a private
replacement root was created. If replacement fails, Bosun SHALL harden-or-delete
any remainder and abort. Otherwise the new render (complete or partial) becomes the
current evidence. No target SHALL clean, harden, replace, or otherwise mutate
another target's effective staging directory.

Before any target executes, Bosun SHALL canonicalize all effective staging paths
and prove they are pairwise disjoint. Equality and ancestor/descendant overlap
SHALL be rejected across implicit-default roots, name-derived children, and
explicit overrides. Canonicalization SHALL resolve symlinks in existing path
components and apply the host platform's path-comparison semantics. Invalid target
sets SHALL fail as a whole before any staging slot is mutated.

Once replacement begins, a per-target lifecycle finalizer SHALL cover every later
return and panic until successful cleanup. The finalizer SHALL prove the remaining
tree private or delete it using the rules above. A panic SHALL be re-raised after
the outcome is recorded. Owner-only creation modes SHALL protect partial output
even if the process terminates before the finalizer can run.

After the health gate and applicable post-deploy verification succeed and post-sync
hook execution completes, Bosun SHALL remove the current target's staging tree
before reporting successful completion. Post-sync hook errors retain their existing
non-fatal warning semantics. If removal fails, Bosun SHALL apply the same owner-only
hardening fallback before continuing; failure to either harden or delete SHALL be a
security error.

Bosun SHALL emit structured operator diagnostics for slot replacement, secured
retention, fail-closed deletion, cleanup fallback, and harden-plus-delete failure.
Diagnostics SHALL identify the target and effective staging path and SHALL NOT
include rendered contents, secret values, or symlink targets.

#### Scenario: Staging is private before and during rendering

- **WHEN** a target enters the render phase and produces directories, rendered files, or copied files
- **THEN** the effective staging root has permission bits `0700` before any output is written and stays `0700` through verification
- **AND** payload temp files remain private until atomic rename
- **AND** descendants retain the existing modes that local and remote deployment propagate to their destinations
- **AND** no backup, deploy, compose, health, hook, or verification stage starts until the completed tree passes no-follow verification

#### Scenario: Partial render and panic fail closed

- **WHEN** rendering returns an error after partial output or a later pipeline stage panics
- **THEN** the lifecycle finalizer secures the current target's remaining tree or deletes it without following links
- **AND** a panic is re-raised after the retention outcome is reported

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

#### Scenario: Failed replacement writes no decrypted output

- **GIVEN** a target has a prior staging slot
- **WHEN** Bosun cannot completely remove it or privately create the replacement root
- **THEN** Bosun writes no decrypted output for the new render
- **AND** hardens or deletes any remaining tree before returning an error

#### Scenario: Early failure preserves prior evidence

- **GIVEN** a target has retained staging evidence from a failed attempt
- **WHEN** the next attempt fails during sync, config reload, secret decryption, or deploy-mode resolution before rendering begins
- **THEN** the prior staging evidence remains unchanged

#### Scenario: Hardening failure deletes unsafe evidence

- **WHEN** Bosun cannot restrict a retained staging entry to the required owner-only access
- **THEN** Bosun deletes that target's staging tree
- **AND** reports that evidence was discarded without logging its contents
- **AND** if deletion also fails, the reconciliation returns a security error

#### Scenario: Symlink, irregular entry, traversal, or replacement race fails closed

- **WHEN** staging verification encounters a symlink or irregular entry, an entry is replaced during protection, or a candidate path escapes the effective staging root
- **THEN** Bosun does not follow or mutate the external target
- **AND** treats protection as failed and deletes only the effective staging tree
- **AND** no rendered content or symlink target is logged

#### Scenario: Equal or nested target slots are rejected before execution

- **WHEN** two targets' canonical effective staging paths are equal or one is an ancestor of the other
- **THEN** Bosun rejects the complete target set before either target executes
- **AND** no target staging tree is cleaned, hardened, replaced, or rendered

#### Scenario: Pre-existing slots are secured before the cycle

- **GIVEN** one or more effective staging slots exist before reconciliation starts
- **WHEN** Bosun begins the first post-upgrade or a later reconciliation cycle
- **THEN** it validates all target paths and hardens or deletes each existing slot before Git sync or secret decryption
- **AND** if any slot can be neither protected nor deleted, no target executes

#### Scenario: Verified success removes staging

- **WHEN** the health gate and applicable post-deploy verification succeed and post-sync hook execution completes
- **THEN** Bosun removes the current target's staging tree
- **AND** only then reports successful completion and records the deployment

#### Scenario: Cleanup failure retains private staging with a warning

- **WHEN** health and applicable post-deploy verification succeed and hooks complete but normal staging removal fails
- **AND** Bosun successfully proves the remaining tree has the required owner-only modes
- **THEN** Bosun records the deployment as successful
- **AND** reports that the secured staging slot remains because cleanup failed
- **AND** the next render replaces that same bounded slot

#### Scenario: Cleanup and protection both fail

- **WHEN** successful verification is followed by cleanup failure
- **AND** Bosun can neither prove the remaining tree private nor delete it
- **THEN** Bosun returns a security error
- **AND** does not record or report deployment success

#### Scenario: Non-fatal hook errors do not preserve successful staging

- **WHEN** the health gate passes, a post-sync hook returns an error under the existing non-fatal hook policy, and applicable post-deploy verification succeeds
- **THEN** Bosun warns about the hook error
- **AND** removes staging before recording deployment success

#### Scenario: Multi-target evidence remains isolated

- **GIVEN** two named targets have distinct effective staging directories
- **WHEN** the first target fails verification and the second target succeeds
- **AND** the shared cycle context remains live
- **THEN** the first target's secured evidence remains available
- **AND** the second target removes only its own staging tree

#### Scenario: Non-live cycle context leaves later sibling evidence untouched

- **GIVEN** two named targets have distinct effective staging directories
- **WHEN** the first target finishes with a failure and the shared cycle context is canceled or its deadline expires
- **THEN** target iteration stops before the second target starts
- **AND** the second target's staging slot is not cleaned, hardened, replaced, or rendered
