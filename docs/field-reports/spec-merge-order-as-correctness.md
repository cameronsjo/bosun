# Spec-Merge-Order as a Correctness Concern — Field Report

**Date:** 2026-05-25
**Type:** architecture / investigation
**Project:** bosun

## Goal

Clear the Wave-1 backlog of open PRs — six spec/logging PRs (#311, #312, #313,
#316, #317, #323) — and land them on `main`. The one PR that couldn't just be
merged was **#312** (`add-backup-integrity-semantics`), because it MODIFIED the
same `Configuration Backup` requirement that the already-merged **#319**
(`add-backup-deadline-and-self-exclusion`) had shipped days earlier. The real
work of the session turned out to be recognizing *why* that collision was
dangerous and resolving it structurally rather than by hand-waving.

## The Hazard

OpenSpec change proposals carry spec *deltas* under `## MODIFIED Requirements`.
When a change is archived (`openspec archive <id>`), a MODIFIED block does **not
merge** into the canonical requirement — it **replaces the entire requirement
block**. That replace semantic is the footgun.

#319 had already landed three clauses on `Configuration Backup`:

- a bounded `BackupTimeout` (`BOSUN_BACKUP_TIMEOUT`, default 5m),
- context-bound verification (so a stuck `tar -tzf` honors a caller deadline),
- a self-exclusion invariant (the backup archive must not recursively include
  its own growing output).

It also kept the long-standing semantic: *backup failures warn and continue*
(non-fatal), reinforced by a `Pipeline Orchestration` global rule carving out
stage 7 as "non-fatal, SHALL NOT abort."

#312's delta did two incompatible things:

1. It **omitted** all three of #319's clauses. Archiving #312 as-written would
   have silently deleted timeout + ctx-verification + self-exclusion from the
   canonical spec — reverting #319 even though the *code* stayed.
2. It **flipped** backup-failure semantics to fail-closed: "when a required
   backup cannot be created or verified, abort before mutating target state."
   That directly contradicted the canonical `Pipeline Orchestration` stage-7
   carve-out, which #312 didn't touch.

So merging/archiving #312 naively would have both reverted shipped behavior and
left two canonical requirements contradicting each other.

## Decisions Made

- **Adopt fail-closed (Outcome B), not warn-continue (Outcome A).** Once #319's
  bounded timeout exists, the original reason for "non-fatal" (a hung backup
  must never wedge the reconcile) is gone — a stuck backup now fails fast and
  loud within the timeout. With the wedge eliminated, continuing past a failed
  *required* backup is no longer the lesser evil; it's just a recoverability
  hole. The "nothing to back up" case stays a clean no-op (cold-start deploys
  still proceed), so the #319 cold-state scenario is preserved.
- **Make #312's MODIFIED block a true superset.** Re-state #319's three clauses
  verbatim inside #312's `Configuration Backup` delta so archive is additive,
  not a revert.
- **Reconcile the global rule in the same change.** Add a MODIFIED
  `Pipeline Orchestration` requirement (replicating the full block — all 15
  stages + all scenarios — because replace-semantics would otherwise drop any
  omitted scenario) that changes the stage-7 carve-out from "non-fatal" to the
  conditional rule: nothing-to-back-up proceeds; a required-backup failure
  (including timeout) aborts before state mutation.

## What Worked

- **Structural gate instead of a mental note.** A beads dependency
  (`bosun-xzw` documenting the contradiction → blocking `bosun-8s5`, the #312
  spec issue) meant #312 *could not* reach `ready-to-build` until the hazard was
  reconciled. The gate, not vigilance, is what made the footgun safe to defer.
- **Re-stating prior clauses verbatim.** After the reconciling edits,
  `openspec validate add-backup-integrity-semantics --strict` passed and
  CodeRabbit re-reviewed and **approved** the fail-closed superset.
- **Closing the loop with a CI fix (#328 / `bosun-6wu`).** During the merge,
  #313 (`spec/fold-cluster-c-multitarget`) hit a *separate* problem: the
  spec-review `Validate Spec` gate derives the change ID from the branch name
  (`CHANGE_ID="${BRANCH#spec/}"`), which is wrong for a fold PR whose branch
  describes the *action* but edits an existing change dir
  (`add-multi-target-reconcile`). CI ran `openspec validate
  fold-cluster-c-multitarget` → "Unknown item" → false-negative failure. We
  merged #313 past that red check (verified locally), then fixed the harness so
  the gate derives change IDs from the PR's `base..HEAD` diff under
  `openspec/changes/<id>/` — multi-change safe, with a branch-name fallback for
  net-new proposals. That turned a one-off bypass into a permanent fix.

## Gotchas

- **A clean `git` merge can still produce a broken build.** Rebasing #316 onto
  main, git auto-merged a region where both #316 and #319 had independently
  added a `logger :=` at different line offsets — two declarations of the same
  variable, which only the compiler caught. Always build after every conflict
  resolution, even when git reports no textual conflict.
- **`ineffassign` on a logging refactor.** #316 introduced `statusCode :=
  http.StatusOK` that was only read on the degraded path; the healthy path's
  implicit 200 via `json.Encode` made the initializer dead. Drop the variable;
  use the constant where it's actually needed.
- **CodeRabbit "Insufficient review credits"** blocks the `CodeRabbit` check
  entirely — it presents as a red check with zero findings, not a code problem.
  Don't chase it as a defect; it's an account-quota signal.
- **The branch-name == change-ID assumption is load-bearing in CI** and silently
  wrong for fold/refactor spec PRs. See `bosun-6wu` / #328.

## Recommendations

- Before merging or archiving any change that MODIFIES a requirement, **diff the
  MODIFIED block against the *current canonical* requirement**. Anything in
  canonical but missing from the delta will be deleted on archive — re-state
  everything you intend to keep.
- When a change flips a global/cross-cutting rule (pipeline stage semantics,
  failure policy), the requirement that *owns* that rule must be MODIFIED in the
  same change, or the canonical spec ends up self-contradictory.
- Track cross-change spec collisions with a **beads dependency** so the
  dependent can't reach `ready-to-build` until the collision is reconciled.
- Name spec branches to match the change dir they edit where possible; the CI
  now tolerates mismatch (#328), but matching names keep the mental model simple.

## Key Takeaways

- **Code merges are independent when file sets are disjoint; spec merges are
  not.** Two changes that MODIFY the same requirement collide regardless of
  files, because `openspec archive` replaces the whole requirement block.
- **A MODIFIED requirement is the complete desired final state, not a patch.**
  Omitting a clause or a scenario deletes it on archive.
- **Bounded timeouts change failure-policy calculus.** "Warn and continue"
  existed to avoid a wedge; once the wedge is eliminated by a deadline,
  fail-closed becomes the safer default without reintroducing a hang.
- **Resolve coordination hazards with structure, not memory.** A beads
  dependency gate caught what review alone would have missed.
- **Fix the harness, not just the symptom.** #313's merge-past-red-check was the
  symptom; #328 (diff-based change-ID detection) was the cure, so the next fold
  PR goes green honestly.
