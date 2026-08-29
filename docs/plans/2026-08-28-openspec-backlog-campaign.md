# OpenSpec Backlog Grounding Campaign

## Goal

Bring Bosun's specification ledger back in line with the released code, then ground and close the stale multi-target and FUSE-hook trackers without reopening already shipped work.

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
- [ ] Independently review and merge the archive/refresh PR; remove its worktree and temporary files.

### 4. Multi-target grounding (#438)

- [x] Map every active task to shipped code, tests, documentation, or genuinely missing behavior.
- [x] Correct proposal, design, task, and proposed issue replacement text so the remaining contract is accurate and non-duplicative; issue #438 remains unchanged until this grounding PR merges.
- [ ] Strict-validate, independently review, and merge the spec-only grounding PR before starting newly discovered behavior.

### 5. FUSE-hook grounding and closure (#431)

- [x] Revalidate the historical approved proposal in merged PR #311 and require a fresh independent gate for this grounded successor before any remaining behavior work.
- [x] Map all 30 tasks to shipped code, tests, documentation, and merged delivery evidence; 26 are shipped, task 1.3 remains partial for focused recursive suffix/infix coverage through `deploy_sync_paths` and `deploy_sync_exclude`, task 2.3 remains partial because doctor falsely warns for an omitted delay despite the safe runtime fallback, and tasks 8.2/8.4 remain partial for exact fallback wording.
- [x] Correct the proposal, design, task checklist, deltas, and proposed issue #431 replacement text so the shipped contract and four bounded remaining slices are accurate and non-duplicative; leave the live issue unchanged until this grounding PR merges.
- [x] Strict-validate, independently review, and merge the spec-only grounding PR (#633) before archiving the change.
- [x] If grounding exposes a concrete behavior gap, implement it as the smallest behavior PR with required tests, consumer documentation, onboard-skill resources, gated checks, and exact-head review; do not create speculative slices.
- [x] Archive `add-reconcile-fuse-hooks` without `--skip-specs` after the remaining implementation merged in #634 and shipped in v0.42.1 from `ca655b5afeec028fb4801357dc8130ad7427c8a5`.

### 6. Directory-aware deploy-tracking archive

- [ ] Revalidate that `add-reconcile-fuse-hooks` has been archived and the canonical `reconcile-fuse-hooks` capability now exists.
- [ ] Archive `add-directory-aware-deploy-tracking` without `--skip-specs`, preserving both its dependent capability addition and `Deploy Sync Invariants` modification.
- [ ] Strict-validate all specs, compare both archived deltas with their canonical requirements, independently review and merge the PR, and remove its worktree and temporary files.

### 7. Exit hygiene

- [ ] Confirm zero campaign PRs remain open and `main` CI is green.
- [ ] Remove every campaign-owned worktree and temporary lane; do not clear shared Go build, module, or lint caches used by concurrent worktrees.
- [ ] Preserve `.beads/`, shared caches, and unrelated Claude worktrees.

## Verification

- `openspec validate --all --strict --no-interactive`
- Relevant focused, full, and race tests through `scripts/agent-go-gate.sh` when code is affected
- `go vet`, changed-code lint, build, and workflow contracts where affected
- Hosted CI and Codecov patch coverage on each exact reviewed head
- Final `git status`, `git worktree list`, cache-size, temp-lane, and free-disk audit

## Deviations

Record scope or sequencing changes here in the same commit that adopts them.
