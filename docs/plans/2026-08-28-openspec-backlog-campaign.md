# OpenSpec Backlog Grounding Campaign

## Goal

Bring Bosun's specification ledger back in line with the released code, then ground and close the stale multi-target and FUSE-hook trackers without reopening already shipped work.

## Chosen approach

Use three bounded, serial lanes after this plan lands on `main`; each lane starts from `main` only after the preceding lane merges:

1. Archive the seven active changes whose task lists are complete, in dependency-safe batches that preserve canonical requirements.
2. Ground `add-multi-target-reconcile` and issue #438 against the implementation on `main`, correcting stale proposal/design/task claims before selecting any remaining implementation work.
3. Ground `add-reconcile-fuse-hooks` and issue #431 against the implementation already on `main`, including merged PRs #401, #402, and #405; correct the stale unchecked tasks and archive the change after every requirement is verified. Open a behavior slice only if grounding proves a concrete remaining gap.

Each PR receives a fresh polish pass, independent exact-head review, hosted checks, and cleanup before merge. Compiler-heavy local checks run only through `scripts/agent-go-gate.sh`; shared module caches remain shared, temporary worktrees are removed after handoff, and no lane bypasses the 100 GiB disk floor.

## Alternatives declined

- **Archive all seven changes blindly in one command sequence:** declined because several deltas overlap canonical reconcile requirements and need dependency-aware review.
- **Implement all 49 tasks listed under #438:** declined because the issue and checklist are stale relative to substantial multi-target behavior already present on `main`.
- **Treat the ten unchecked FUSE-hook tasks as unimplemented:** declined because current `main` and merged PRs #401, #402, and #405 already contain their doublestar matching, filesystem durability/timing, doctor warning, deletion tracking, and regression coverage; the ledger needs grounding before any new code.
- **Open replacement issues:** declined because existing issues and OpenSpec changes are the durable records; update or close them instead.

## Checklist

### 1. Campaign entry

- [x] Record the approved approach before implementation.
- [ ] Merge this plan to `main` with no release-triggering commit prefix.

### 2. Completed-change archives

- [ ] Inventory canonical-spec overlap and establish archive order for the seven completed active changes.
- [ ] Archive each dependency-safe batch without `--skip-specs` unless a change is proven tooling-only.
- [ ] Strict-validate all specs after every batch and compare archived deltas with canonical requirements.
- [ ] Independently review and merge the archive PRs; remove their worktrees and temporary files.

### 3. Multi-target grounding (#438)

- [ ] Map every active task to shipped code, tests, documentation, or genuinely missing behavior.
- [ ] Correct proposal, design, task, and issue text so the remaining contract is accurate and non-duplicative.
- [ ] Strict-validate, independently review, and merge the spec-only grounding PR before starting newly discovered behavior.

### 4. FUSE-hook grounding and closure (#431)

- [ ] Revalidate the historical `ready-to-build` gate and current spec against `main`.
- [ ] Map all 30 tasks to shipped code, tests, documentation, and merged delivery evidence; explicitly verify the ten stale unchecked tasks against PRs #401, #402, and #405 plus current `main`.
- [ ] Correct the proposal, design, task checklist, and issue #431 so the shipped contract is accurate and non-duplicative.
- [ ] Strict-validate, independently review, and merge the spec-only grounding PR before archiving the change.
- [ ] If grounding exposes a concrete behavior gap, implement it as the smallest behavior PR with required tests, consumer documentation, onboard-skill resources, gated checks, and exact-head review; do not create speculative slices.
- [ ] Archive `add-reconcile-fuse-hooks` without `--skip-specs` only after every task and requirement has verified merged and released evidence.

### 5. Exit hygiene

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
