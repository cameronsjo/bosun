# OpenSpec Backlog Grounding Campaign

## Goal

Bring Bosun's specification ledger back in line with the released code, then finish the remaining high-value FUSE-hook hardening without reopening already shipped work.

## Chosen approach

Use three bounded lanes after this plan lands on `main`:

1. Archive the seven active changes whose task lists are complete, in dependency-safe batches that preserve canonical requirements.
2. Ground `add-multi-target-reconcile` and issue #438 against the implementation on `main`, correcting stale proposal/design/task claims before selecting any remaining implementation work.
3. Finish `add-reconcile-fuse-hooks` through small behavior slices: glob correctness, durable/FUSE-safe writes and timing, then deletion-aware hook inputs.

Each PR receives a fresh polish pass, independent exact-head review, hosted checks, and cleanup before merge. Compiler-heavy local checks run only through `scripts/agent-go-gate.sh`; shared module caches remain shared, temporary worktrees are removed after handoff, and no lane bypasses the 100 GiB disk floor.

## Alternatives declined

- **Archive all seven changes blindly in one command sequence:** declined because several deltas overlap canonical reconcile requirements and need dependency-aware review.
- **Implement all 49 tasks listed under #438:** declined because the issue and checklist are stale relative to substantial multi-target behavior already present on `main`.
- **Land the ten remaining FUSE-hook tasks in one PR:** declined because glob semantics, filesystem durability, timing defaults, and deletion tracking have different failure modes and review surfaces.
- **Open replacement issues:** declined because existing issues and OpenSpec changes are the durable records; update or close them instead.

## Checklist

### 1. Campaign entry

- [x] Record the approved approach before implementation.
- [x] Merge this plan to `main` with no release-triggering commit prefix.

### 2. Completed-change archives

- [ ] Inventory canonical-spec overlap and establish archive order for the seven completed active changes.
- [ ] Archive each dependency-safe batch without `--skip-specs` unless a change is proven tooling-only.
- [ ] Strict-validate all specs after every batch and compare archived deltas with canonical requirements.
- [ ] Independently review and merge the archive PRs; remove their worktrees and temporary files.

### 3. Multi-target grounding (#438)

- [ ] Map every active task to shipped code, tests, documentation, or genuinely missing behavior.
- [ ] Correct proposal, design, task, and issue text so the remaining contract is accurate and non-duplicative.
- [ ] Strict-validate, independently review, and merge the spec-only grounding PR before starting newly discovered behavior.

### 4. Remaining FUSE-hook hardening (#431)

- [ ] Revalidate the historical `ready-to-build` gate and current spec against `main`.
- [ ] Implement and test full doublestar/glob semantics across hooks and deploy-path consumers.
- [ ] Implement and test destination-directory durability, the safe settle-delay default, and the FUSE preflight warning.
- [ ] Implement and test deletion-aware hook change sets.
- [ ] Update consumer documentation and onboard-skill resources in the same behavior PR where required.
- [ ] Run fresh polish, gated relevant/full/race checks, hosted CI, and independent exact-head review for each slice.
- [ ] Merge clean slices, update #431 accurately, and archive the change only after all tasks are deployed.

### 5. Exit hygiene

- [ ] Confirm zero campaign PRs remain open and `main` CI is green.
- [ ] Remove every campaign worktree/temp lane and clear only expendable Go build/lint caches through the gate.
- [ ] Preserve `.beads/`, the shared Go module cache, and unrelated Claude worktrees.

## Verification

- `openspec validate --all --strict --no-interactive`
- Relevant focused, full, and race tests through `scripts/agent-go-gate.sh`
- `go vet`, changed-code lint, build, and workflow contracts where affected
- Hosted CI and Codecov patch coverage on each exact reviewed head
- Final `git status`, `git worktree list`, cache-size, temp-lane, and free-disk audit

## Deviations

Record scope or sequencing changes here in the same commit that adopts them.
