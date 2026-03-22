# CodeRabbit Review Loop at Scale — Field Report

**Date:** 2026-03-20
**Type:** discovery
**Project:** bosun

## Goal

Iterate 5 PRs through CodeRabbit's assertive review profile to convergence (0 actionable findings), fixing everything along the way. The PRs ranged from a spec-only change to a 4400-LOC refactor that split two god files into 14 focused modules. The goal was to understand the cost and value of pedantic AI review at this scale, and develop patterns for managing the feedback loop efficiently.

## What We Tried

### Review Configuration

Cranked CodeRabbit to maximum pedantry:

```yaml
tone_instructions: >
  Pedantic and direct. Flag inconsistencies, missing error checks,
  Go idiom violations, security concerns. Prefer concrete code suggestions.
reviews:
  profile: "assertive"
  request_changes_workflow: true
```

This profile produces 4-20 inline comments per review round, categorized by severity (Critical/Major/Minor/Trivial). It runs automated analysis chains — executing shell commands against the repo to verify its own findings before posting.

### The Loop

Each PR followed the same cycle:

1. Push code
2. `gh pr comment N --body "@coderabbitai review"` to trigger review
3. Read findings via GraphQL (`reviewThreads` query)
4. Triage by severity
5. Fix actionable findings
6. Resolve threads (GraphQL `resolveReviewThread` mutation)
7. Push fixes, trigger re-review
8. Repeat until 0 new actionable findings

### Tooling for the Loop

Built a lightweight triage workflow using `gh api graphql`:

```bash
# Get all unresolved threads with severity
gh api graphql -f query='...' --jq '[... | select(.isResolved == false) | {
  path, line, severity: (capture("🔴 Critical|🟠 Major|🟡 Minor|🔵 Trivial"))
}]'

# Batch-resolve addressed threads
gh api graphql -f query='mutation { r1: resolveReviewThread(input: {threadId: "..."}) ... }'
```

This avoided manual GitHub UI clicking for 60+ thread resolutions across 5 PRs.

## What Worked

### Severity-Based Triage

CodeRabbit's severity categories (Critical > Major > Minor > Trivial) directly map to action priority:

| Severity | Action | Example from this session |
|----------|--------|--------------------------|
| Critical | Fix immediately | Slice aliasing bug in DFS cycle detection, path traversal in target names |
| Major | Fix in same PR | `os.Exit` in Cobra commands → `RunE`, shallow copy mutation aliasing |
| Minor | Fix if quick, else file issue | Stale docstrings, inconsistent logger usage |
| Trivial | Resolve as acknowledged | Code style preferences, import ordering |

### Pre-existing vs New Finding Separation

The refactor PRs surfaced dozens of issues that existed in the original code but were invisible in the monolithic file. The pattern:

- **Pre-existing + fixable now** → fix in the refactor PR (docstrings, unused params, error handling)
- **Pre-existing + needs design** → file a beads issue, resolve thread with note (security findings in SSH/backup)
- **New from this PR** → fix immediately, never merge with new issues

This kept PRs moving without ignoring real problems.

### The Pedantic Config Found Real Bugs

The assertive profile caught issues that would have shipped otherwise:

1. **Slice aliasing in DFS** — `append(path, node)` could reuse backing array, corrupting sibling branch paths. Pre-existing but invisible in the 1240-line file
2. **Target JSON tag mismatch** — `BOSUN_TARGETS` env var silently ignored snake_case fields because the struct had no JSON tags. Tests caught this first, but CodeRabbit independently flagged it
3. **Shallow copy mutation hazard** — `ConfigForTarget` shared slice backing arrays between base and per-target configs. An append in one would corrupt the other
4. **Reserved name collision** — user naming a target "default" would bypass per-target path derivation
5. **SecretsScope always overwrites** — cleared the base scope even when the target didn't set one

## Comparison: Convergence by PR

| PR | Type | Initial Findings | Rounds to 0 New | Total Fixes |
|---|---|---|---|---|
| #156 (spec) | Spec delta | 4 | 2 | 4 |
| #158 (feature) | Multi-target impl | 6 | 5 | 8 + 2 documented limitations |
| #160 (CI + tests) | Config + tests | 4 | 3 | 4 + JSON tags fix |
| #162 (refactor) | diagnostics.go split | 20 | 4 | 20 (1 critical, 2 major, rest minor/trivial) |
| #163 (refactor) | reconcile.go split | 14 | 6 | 9 fixed + 5 security deferred |

*Pattern: refactor PRs produce 3-5x more findings than feature PRs because they expose pre-existing issues in newly visible code.*

## Gotchas

- **`tone_instructions` has a 250-character limit** in the CodeRabbit schema. Our initial 370-character instruction was silently truncated. Discovered when CodeRabbit flagged it on itself
- **CodeRabbit re-reviews see the full diff, not just the delta** — fixing one finding can surface new ones in code that was previously below the review threshold. This is why round counts grow with PR size
- **Resolved threads can re-open** — if a re-review generates a new comment on the same file/line, it creates a new thread (not re-opens the old one). Track by unresolved count, not thread IDs
- **`codecov/patch` is a noisy gate for refactors** — moving code between files counts as "new lines" with 0% coverage even though the lines have tests. Consider excluding pure-move PRs from patch coverage
- **Security findings in refactors are a trap** — the refactor didn't introduce the shell injection in `ssh.go`, but CodeRabbit flags it now that the code is in a focused file. Resist the urge to quick-fix security issues in a refactor PR — they need their own review cycle

## Decisions Made

1. **Fix everything, not just criticals** — even trivial nitpicks, because the pedantic config will re-flag them every round. Cheaper to fix once than dismiss repeatedly
2. **Security findings get their own bead** — pre-existing security issues (shell injection, file handle reuse, non-atomic move) filed as P1 bead rather than fixed in the refactor. These need dedicated design, not drive-by patches
3. **Use `resolveReviewThread` aggressively** — resolve threads as soon as the fix is pushed, don't wait for re-review confirmation. If the fix is wrong, CodeRabbit will raise a new thread
4. **`/loop 10m` for convergence** — set a recurring check to monitor CI + review status and fix issues as they appear, rather than manually polling

## Recommendations

- **Always triage before fixing** — 5 minutes categorizing 20 findings saves 30 minutes of fixing things that don't matter
- **GraphQL is faster than the GitHub UI** — batch thread resolution and severity extraction via `gh api graphql` instead of clicking through the PR conversation
- **Refactor PRs need 2x the review budget** — they surface pre-existing issues that inflate the finding count. Plan for 4-6 rounds, not 2
- **The assertive profile is worth it** — it found 5 real bugs across this session that would have shipped. The noise (trivial nitpicks) is manageable with severity triage
- **Commit review fixes separately from the refactor** — `refactor(reconcile): split into modules` then `fix(reconcile): address CodeRabbit findings` keeps the history clean and makes it clear what was moved vs what was changed

## Key Takeaways

- CodeRabbit's assertive profile at scale produces a 5:1 signal-to-noise ratio (real bugs vs style nitpicks). The signal is worth the noise
- Refactor PRs are the highest-value target for AI review — they expose pre-existing bugs that were invisible in monolithic files
- The review loop has a natural convergence pattern: round 1 catches structural issues, rounds 2-3 catch interaction effects from fixes, rounds 4+ are diminishing returns (usually trivial)
- Batch GraphQL operations for thread management — individual API calls don't scale past 10 threads
- Security findings in refactors should be triaged out, not quick-fixed. They need their own focused PR with proper test coverage
