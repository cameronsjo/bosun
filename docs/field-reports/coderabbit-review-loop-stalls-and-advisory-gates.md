# CodeRabbit Review Loop — Stalls, Advisory Gates, and Multi-Path Finding Verification — Field Report

**Date:** 2026-05-28
**Type:** investigation
**Project:** bosun

## Goal

Drive the CodeRabbit review/merge cycle to completion for three independent PRs that landed
the deploy-invariant follow-up backlog: `#373` (`chore(openspec)` — archive implemented
spec changes), `#375` (`fix(reconcile)` — scope the pre-deploy backup to the deployed
footprint, bosun-5qx), and `#376` (`docs` — daemon API corrections, bosun-vam). Address all
review findings, then merge in `1xr → 5qx → vam` order. This report captures three things
that were *not* obvious going in: how to behave when CodeRabbit stalls, what actually gates a
merge on this repo, and a finding-triage trap that nearly let a real bug through.

This complements the existing `coderabbit-review-loop-at-scale.md` (GraphQL batch-triage
patterns) and `bug-sprint-pr-review-loop.md` — it does not repeat them.

## What Happened

`#373` was reviewed and approved cleanly. `#376` got a normal review with three findings (one
trivial, two minor) — all valid, all fixed, all verified against source. `#375` is where the
session got interesting:

- CodeRabbit's **initial review never ran** — it hit a per-account hourly PR-review rate
  limit ("more reviews available in 9 minutes") and silently never retried.
- Manual `@coderabbitai review` triggers were **acknowledged** ("Review triggered") but
  produced no output. The CodeRabbit GitHub check would show `startedAt` and then sit with
  `status: null` indefinitely — start without finish.
- This repeated across **three triggers spanning ~50 minutes**. Meanwhile `#373` and `#376`
  reviewed fine in the same window.

The asymmetry was the key signal: a blanket account block would have starved all three PRs.
Since the others reviewed, the stall was either per-PR or just unlucky rate-limit timing on
each `#375` trigger. More pings were not going to behave differently.

After a wait, a later trigger finally delivered a full review of `#375` with four findings.
Two were valid (fixed), two were test-style nitpicks (skipped with documented reasons).

## Root Cause / Findings

### 1. The merge gate was self-imposed, not mechanical

When CodeRabbit stalled, the operative question became "can I even merge?" The decisive check:

```bash
gh api repos/cameronsjo/bosun/branches/main/protection   # -> 404 "Branch not protected"
```

`main` has **no branch protection** — no required status checks, no required reviewers. That
means CodeRabbit is purely advisory. The tell was visible without the API call:
`mergeStateStatus` was `UNSTABLE`, not `BLOCKED`. GitHub returns `BLOCKED` only when a
*required* check/review fails; `UNSTABLE` means "mergeable, but some non-required check is
pending/red." So `reviewDecision: CHANGES_REQUESTED` never actually blocked the merge — it was
bookkeeping for unresolved threads.

### 2. A finding can be wrong about one path and right about another in the same code

CodeRabbit flagged `createBackup` (reconcile.go): *"backupFilesFromTargets can return an error
and nil paths, producing an empty backup set."* I verified it against the code, concluded the
walk-error path **already returns partial paths** (it appends to the `files` slice as it walks
and returns it unconditionally alongside the error), and **publicly skipped the finding** as
already-handled.

That was wrong. The finding's headline matched the walk-error path I'd checked, but the *real*
defect was a second path sharing the same code: when `discoverDeployTargets` itself **fails**,
it returns `nil` targets, and `backupFilesFromTargets(nil)` returns an empty slice → a **no-op
backup, exactly when rollback protection is needed**. The stale comment promised a "full path"
fallback the code never delivered (the pre-5qx `backupPathsFromTargets(nil)` was likewise
empty, so it was a *latent* gap, not a 5qx regression). I re-verified, posted a public
correction, applied CodeRabbit's proposed fallback (`paths = []string{appdataBase}` on
discovery failure, bounded by the existing `BackupTimeout`), and added a regression test.

## Gotchas

- **CodeRabbit "Review triggered" ack ≠ review will happen.** The ack is cheap; the review can
  still be silently dropped by rate limiting. Watch the check's `startedAt`/`completedAt`, not
  the ack comment.
- **`gh api graphql` vs the `guard-gh-write` hook.** Resolving review threads needs GraphQL
  mutations, which the cadence guard blocks (it demands `-R`, which `gh api` doesn't accept).
  Reads via `gh pr view <n> -R owner/repo --json reviews,comments` pass the guard;
  `gh api repos/.../pulls/<n>/comments` (REST inline comments) also worked. For a PR you're
  about to merge, skipping thread resolution (with reasoning posted as a comment) avoids the
  fight entirely — threads on a merged PR are historical.
- **codecov/patch measures the diff, not the package.** The package sat at 81.8% overall while
  the *patch* failed, because the new `createBackup` branches were unexercised. Lifting
  `createBackup` 73→92% (remote-path + discovery-failure fault-injection tests) fixed the patch
  without materially moving the package average.

## Recommendations

- When CodeRabbit stalls but other PRs review fine, treat it as per-PR/timing, not an account
  block. Re-trigger once after the hourly window resets (~50+ min); if it still won't deliver,
  fall back to **green CI + your own verification** rather than blocking indefinitely. On a
  repo with no branch protection, this is a quality choice, not a mechanical one — make it
  explicitly and document the reasoning on the PR.
- **Verify a finding against *every* code path it could touch, not just the one matching its
  headline.** If a finding names a function, trace each caller/return path. The headline
  matching a handled path is exactly how a real defect in a sibling path slips through.
- To drive a hard-to-cover branch deterministically, look for an early fail-fast guard: the
  remote-backup branch was coverable without an SSH server by passing an invalid host so
  `validateHost` rejects it at `backup.go:202`.

## Key Takeaways

- `mergeStateStatus: UNSTABLE` (vs `BLOCKED`) and a 404 from `branches/main/protection` together
  tell you CodeRabbit is advisory here — the review/merge gate is self-imposed discipline.
- A code-review finding can be simultaneously wrong about path A (handled) and right about
  path B (broken) when both share a function. Skipping on path A's merits is the trap; verify
  every path. This nearly let a no-op-backup-on-discovery-failure ship.
- "Review triggered" from CodeRabbit is an ack, not a guarantee — rate limits drop reviews
  silently. Trust the check's start/complete timestamps.
- On this repo, lifting **patch** coverage means fault-injecting the *new* error/remote branches
  (the `chmod 0000` walk-error and `validateHost` fail-fast levers), not adding happy-path tests.
