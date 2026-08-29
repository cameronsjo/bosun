# OpenSpec Backlog Grounding Campaign

## Goal

Bring Bosun's specification ledger back in line with the released code, then ground the stale multi-target tracker and close the completed FUSE-hook tracker without reopening already shipped work.

## Chosen approach

Use five bounded, serial lanes after this plan lands on `main`; each lane starts from `main` only after the preceding lane merges:

1. Correct the two inaccurate `add-health-gate-scope` claims, then archive it with the four independently ready completed changes.
2. Correct and separately archive `preserve-failed-staging-evidence`, then refresh the four downstream active whole-block `Pipeline Orchestration` deltas against the new canonical requirement.
3. Ground `add-multi-target-reconcile` and issue #438 against the implementation on `main`, correcting stale proposal/design/task claims before selecting any remaining implementation work.
4. Ground `add-reconcile-fuse-hooks` and issue #431 against the implementation already on `main`, including merged PRs #401, #402, and #405; correct the stale unchecked tasks and archive the change after every requirement is verified. Open a behavior slice only if grounding proves a concrete remaining gap.
5. Archive `add-directory-aware-deploy-tracking` only after the preceding lane creates the canonical `reconcile-fuse-hooks` capability it extends.

Each PR receives a fresh polish pass, independent exact-head review, hosted checks, and cleanup before merge. Compiler-heavy local checks run only through `scripts/agent-go-gate.sh`; shared module caches remain shared, temporary worktrees are removed after handoff, and no lane bypasses the 100 GiB disk floor.

## Alternatives declined

- **Archive all seven completed changes together:** declined because `preserve-failed-staging-evidence` needs delta repair plus downstream whole-block refreshes, while `add-directory-aware-deploy-tracking` depends on the not-yet-canonical `reconcile-fuse-hooks` capability.
- **Refresh downstream `Pipeline Orchestration` deltas later:** declined because a later whole-block archive could silently discard the newly canonical sibling-continuation and stage-label clauses.
- **Implement all 49 tasks listed under #438:** declined because the issue and checklist are stale relative to substantial multi-target behavior already present on `main`.
- **Treat the ten unchecked FUSE-hook tasks as unimplemented:** declined because current `main` and merged PRs #401, #402, and #405 already contain their doublestar matching, filesystem durability/timing, doctor warning, and deletion tracking. Grounding found four bounded remainders: deploy-sync consumer coverage, a source-unaware doctor false warning, and exact fallback wording in two documentation consumers, not ten missing implementation slices.
- **Open replacement issues:** declined because existing issues and OpenSpec changes are the durable records; update or close them instead.

## Checklist

### 1. Campaign entry

- [x] Record the approved approach before implementation.
- [x] Merge this plan to `main` with no release-triggering commit prefix.

### 2. Independently ready archive batch

- [x] Correct the two inaccurate claims in `add-health-gate-scope` before archive.
- [x] Archive `harden-local-rollback-extraction`, `bound-alert-provider-content`, `add-health-gate-scope`, `add-https-git-auth`, and `add-drift-breaker-semantics` without `--skip-specs`.
- [x] Strict-validate all specs and compare each archived delta with its canonical requirement.
- [x] Independently review and merge the archive PR; remove its worktree and temporary files.

### 3. Failed-staging archive and downstream refresh

- [x] Correct `preserve-failed-staging-evidence` so its delta retains sibling continuation and uses the shipped stage labels.
- [x] Archive it separately without `--skip-specs`, then refresh the complete `Pipeline Orchestration` blocks in `add-backup-integrity-semantics`, `add-image-prepull`, `add-image-update-policy`, and `add-multi-target-reconcile`.
- [x] Strict-validate all specs, compare the archived delta with the canonical requirement, and verify each refreshed downstream block retains its own proposed behavior.
- [x] Independently review #631 at `9431193495a94af45a575158d1ba2d0404244603`, merge it as `f7d4546bbd68783f0f0a4e0213c7c8dc24e10f98`, and remove its worktree and temporary files.

### 4. Multi-target grounding (#438)

- [x] Map every active task to shipped code, tests, documentation, or genuinely missing behavior.
- [x] Correct proposal, design, task, and proposed issue replacement text so the remaining contract is accurate and non-duplicative; issue #438 remains unchanged until this grounding PR merges.
- [x] Strict-validate and independently review #632 at `a7bd1ac033330225639f963e76eab83552143221`, merge it as `b0c4ac10dfdc2c8112608b3e261af6a61804eada` before starting newly discovered behavior, remove its worktree and temporary files, and retain issue #438 as the open record for its grounded remaining implementation gaps.

### 5. FUSE-hook grounding and closure (#431)

- [x] Revalidate the historical approved proposal in merged PR #311 and require a fresh independent gate for this grounded successor before any remaining behavior work.
- [x] Map all 30 tasks to shipped code, tests, documentation, and merged delivery evidence; at grounding time, 26 were shipped, task 1.3 remained partial for focused recursive suffix/infix coverage through `deploy_sync_paths` and `deploy_sync_exclude`, task 2.3 remained partial because doctor falsely warned for an omitted delay despite the safe runtime fallback, and tasks 8.2/8.4 remained partial for exact fallback wording.
- [x] Correct the proposal, design, task checklist, deltas, and proposed issue #431 replacement text so the shipped contract and four bounded remaining slices are accurate and non-duplicative; leave the live issue unchanged until this grounding PR merges.
- [x] Strict-validate, independently review, and merge the spec-only grounding PR (#633) before archiving the change.
- [x] If grounding exposes a concrete behavior gap, implement it as the smallest behavior PR with required tests, consumer documentation, onboard-skill resources, gated checks, and exact-head review; do not create speculative slices.
- [x] Archive `add-reconcile-fuse-hooks` without `--skip-specs` after the remaining implementation merged in #634 and shipped in v0.42.1 from `ca655b5afeec028fb4801357dc8130ad7427c8a5`.

### 6. Directory-aware deploy-tracking archive

- [x] Revalidate that `add-reconcile-fuse-hooks` was archived by #636 at `2341270de03778fec1c2584a0afe7dbd9ae3321c` and the canonical `reconcile-fuse-hooks` capability exists with all five base requirements.
- [x] Archive `add-directory-aware-deploy-tracking` without `--skip-specs`, adding only `Directory-Aware Deploy Change Tracking` and modifying only `Deploy Sync Invariants`.
- [x] Strict-validate the focused canonical specs and all OpenSpec items; compare both archived deltas with their exact canonical requirements; preserve the five base FUSE requirements, all 11 prior deploy-invariant scenarios, and the unchanged post-sync requirement.
- [x] Independently review #637 at `10701e4261264107e28a91e62170fbb0a1f088d0`, merge it as `0fb5587240a4c93acb88a33fd134f0e0007008af`, then remove its author worktree and temporary files.
- [x] Close issue #431 after #637 confirms the dependent archive preserves and extends the released FUSE-hook contract.

### 7. Exit hygiene

- [x] Before creating this final ledger-only handoff PR, confirm zero prior campaign PRs (#629 through #637) remained open and `main` CI run `33228950590` was green at exact #637 merge `0fb5587240a4c93acb88a33fd134f0e0007008af`; Release Please run `33228950569` was also green.
- [x] Before creating this final ledger-only review lane, remove every prior campaign-owned worktree and temporary lane; only the root, the two pre-existing Claude worktrees, and pre-existing `/private/tmp/bosun-panel-UuDxY2` remained, with shared Go build, module, and lint caches left intact.
- [x] Preserve the root's existing `.beads/`, shared caches, and unrelated Claude worktrees; the preflight audit found 381 GiB free.

## Verification

- `openspec validate --all --strict --no-interactive`
- Relevant focused, full, and race tests through `scripts/agent-go-gate.sh` when code is affected
- `go vet`, changed-code lint, build, and workflow contracts where affected
- Hosted CI and Codecov patch coverage on each exact reviewed head
- Final `git status`, `git worktree list`, cache-size, temp-lane, and free-disk audit

## Deviations

- PR #636's late archive review corrected the final FUSE archive evidence and unavailable-diff-base wording; it explicitly rejected parent-directory fsync after stale-file deletion because that would have promoted unreleased behavior into the canonical contract.
