# Bug Sprint PR Review Loop — Field Report

**Date:** 2026-03-24
**Type:** pipeline
**Project:** bosun

## Goal

Merge 5 parallel-agent-authored bug fix PRs (#199–#203) through CodeRabbit's ASSERTIVE review profile, iterating on feedback until all findings were addressed or triaged. The PRs originated from a prior session's parallel agent bug sprint (documented in `docs/field-reports/parallel-agent-bug-sprint.md`).

## Pipeline Overview

The review-fix-merge loop ran as a repeatable 4-phase cycle:

```
1. Triage    — Pull all inline comments, classify severity, identify cross-file noise
2. Fix       — Launch parallel agents (one per PR branch, zero file overlap)
3. Wait      — Set a CronCreate timer (10 min), check review state when it fires
4. Converge  — If clean, merge. If new findings, loop back to phase 1.
```

This cycle ran 4 times before all 5 PRs merged. The first PR (#203) was pre-approved and merged immediately, reducing the active set to 4.

### Round Progression

| Round | PRs Active | Findings | Actions |
|------:|:----------:|:--------:|---------|
| 1 | 4 | 8 (all legitimate) | 4 parallel fix agents, all pushed |
| 2 | 4 | 5 new (3 legit, 2 cross-file noise) | 4 parallel fix agents, filed `bosun-uxr4` for out-of-scope architectural concern |
| 3 | 3 | 2 new (1 stale, 1 trivial) | 1 fix agent for assertion consistency |
| 4 | 3 | 1 new (same architectural concern, already tracked) | Merged all 3 |

### Merge Sequencing

PR #202 developed merge conflicts after #201 merged (both touched `deploy.go`/`deploy_test.go`). Resolution: checkout the branch, merge main, accept main's versions for files outside PR scope, run tests, push, merge.

## What Worked

**Parallel fix agents with strict file ownership.** Each agent got exactly one branch and its specific files. No overlapping edits, no merge conflicts between agents. The agents ran simultaneously and finished independently.

**Timer-based review polling.** `CronCreate` one-shot timers (5–10 min) kept the loop moving without manual clock-watching. When the timer fired, the review state was checked automatically and the next action taken.

**Severity triage before fixing.** Classifying each finding as trivial/minor/major before launching agents prevented over-investing in nitpicks and surfaced the real issues first.

**Filing out-of-scope issues immediately.** CodeRabbit flagged a legitimate architectural concern (`resolveDeployMode` doesn't consider secrets-based target hosts). Rather than trying to fix it in a bug-fix PR, we filed `bosun-uxr4` and dismissed the finding. This prevented scope creep while acknowledging the gap.

## What Didn't Work

**Agent push verification.** The round 1 agent for PR #201 reported "pushed successfully" but the commit never appeared on the remote. Root cause: agents sharing a working directory can checkout different branches, and the push lands on whichever branch is checked out — not necessarily the one the agent intended. Round 2 added explicit post-push verification (`git log --oneline origin/<branch> -3`) which caught the issue.

**Python migration scripts for same-name fields.** A later part of the session used regex scripts to rename struct fields. The scripts couldn't distinguish `Config.PostSyncHooks` from `ReloadedConfig.PostSyncHooks` from `Target.PostSyncHooks` — all three structs share the field name but with different types. Every script over-wrapped `ReloadedConfig` and `Target` fields, requiring manual correction. Go AST tools (gorename, gopls) would have been context-aware.

**CodeRabbit re-review latency.** Two PRs (#199, #201) didn't get re-reviewed after pushes despite `@coderabbitai review` triggers. Multiple triggers were needed, and #201 required 15+ minutes to respond. Rate limiting or queue depth may be factors.

**Cross-file comments on wrong PRs.** CodeRabbit posted comments about `reconcile.go` and `deploy_test.go` on PR #202, which only changed `template.go` and `template_test.go`. This happened because an agent accidentally committed changes to files outside its scope. The cross-file comments were triaged and routed to the correct PR's fix agent.

## Gotchas

- **CodeRabbit's ASSERTIVE profile generates cascading findings.** Each fix round spawns new "while you're here" suggestions. The findings get smaller each round but never reach zero — at some point you have to dismiss the remaining nitpicks and merge.
- **Beads auto-sync pollutes PR diffs.** The `.beads/issues.jsonl` file changes appear in every PR diff, triggering CodeRabbit comments about unrelated beads entries. No way to suppress this without gitignoring the file (which breaks beads).
- **`CHANGES_REQUESTED` persists until a new review is submitted.** GitHub's review decision doesn't auto-clear when new commits are pushed. A PR can have all findings addressed but still show `CHANGES_REQUESTED` because CodeRabbit hasn't re-reviewed yet.
- **Stale review comments look like new findings.** CodeRabbit sometimes re-posts the same comment on a new review if the line numbers shifted. Comparing comment timestamps against push timestamps is necessary to distinguish stale from new.

## Recommendations

1. **Always verify agent pushes.** Add `git log --oneline origin/<branch> -3` after every agent push to confirm the commit landed on the expected remote branch.
2. **Batch dismiss architectural findings.** When CodeRabbit raises the same structural concern across multiple rounds, dismiss with a linked issue reference rather than re-explaining each time.
3. **Set timer intervals based on PR count.** 10 minutes worked for 4 PRs. For larger batches, 15–20 minutes gives CodeRabbit more time to process the queue.
4. **Route cross-file comments by file ownership.** When a comment lands on the wrong PR, don't fix it there — route the fix to the agent that owns that file's branch.
5. **Merge the simplest PR first.** Starting with the pre-approved #203 reduced the active set immediately and established the merge pipeline.

## Key Takeaways

- The timer/triage/fix loop is a repeatable pattern for iterating with automated reviewers — it prevents both polling too early and forgetting to check
- Parallel fix agents work well when file ownership is strict, but shared working directories cause phantom pushes — verify every push
- CodeRabbit's ASSERTIVE mode converges in 3–4 rounds for bug fixes; budget accordingly
- Cross-file agent contamination is the primary failure mode for parallel agents in a shared checkout — consider worktree isolation for future sprints
- Filing out-of-scope issues immediately prevents review loop scope creep without ignoring legitimate concerns
