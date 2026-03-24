# Parallel Agent Bug Sprint — Field Report

**Date:** 2026-03-23
**Type:** pipeline
**Project:** bosun

## Goal

Fix a production bug where rendered compose files never landed on the deploy target (#190), then systematically search the codebase for similar silent-failure patterns and fix them in parallel using agent worktrees.

## Pipeline Overview

```text
Fix #190 manually (root cause investigation)
        |
Pattern search: "what else silently fails like this?"
        |
Two Explore agents scan codebase in parallel
        |
Triage findings → file 5 GitHub issues
        |
Spawn 5 fix agents in isolated worktrees (parallel)
        |
Cherry-pick commits → proper fix branches → 5 PRs
```

**Wall time for the 5-agent fix phase:** ~4 minutes (longest agent took ~3.5 min). Sequential would have been 15-20 minutes minimum.

## What Worked

### Systematic exploration after the first fix

The original bug (#190) was a path construction mismatch in `RenderDirectory` — `sourceDir` already contained `InfraSubDir`, but the function joined it again internally. After fixing it, we asked: "what else in this codebase silently fails?" Two Explore agents scanned for:

1. **Path doubling bugs** — functions that receive a pre-resolved path then join a component again
2. **Silent failure patterns** — `IsNotExist` swallowed, `_ = os.Remove`, errors logged as warnings instead of surfaced

This yielded 9 findings across 4 packages. After triage, 5 were worth filing as issues.

### Agent worktree isolation for parallel fixes

Each of the 5 bugs touched different files (with one overlap on `reconcile.go` between #193 and #197, but different functions). We spawned 5 agents simultaneously, each in `isolation: "worktree"`. All 5:

- Got their own `.claude/worktrees/agent-<id>` directory
- Committed to their own branch
- Ran tests in isolation
- Completed without interfering with each other

### Cherry-pick workflow for shipping agent work

Agent worktree branches aren't suitable for PRs directly (they branch from wherever `main` was when spawned, have auto-generated names). The workflow that emerged:

1. Agents commit to their worktree branches
2. After all complete, cherry-pick each commit onto a properly named fix branch from current `main`
3. Push fix branches, create PRs

This handles the rebase-onto-latest problem cleanly and gives PRs conventional branch names.

## Gotchas

### Agent on `main` locks the branch

Agent af43eaa8 (#194) checked out `main` after committing (to run tests against latest). This locked the `main` branch — git worktrees don't allow the same branch checked out in multiple worktrees. The commit was dangling but recoverable via `git reflog`. Fix: remove the offending worktree with `--force` before switching to main.

### `set -e` + bash arithmetic

`((deleted++))` returns exit code 1 when the variable is 0 (bash treats `((0))` as falsy). Under `set -e`, this kills the script. Use `var=$((var + 1))` instead.

### `copyNonTemplateFiles` IsNotExist is intentional... until it isn't

The original `IsNotExist` guard in `copyNonTemplateFiles` was written for the case where the source dir genuinely doesn't exist yet (first run). But it also masks misconfigured `InfraSubDir` — a typo in the env var produces an empty staging directory with zero errors. The fix distinguishes root-path-missing (error) from individual-file-disappeared-mid-walk (skip).

## Recommendations

### For future bug sprints

- **Fix one, then search for the pattern.** The first fix reveals the bug class. Explore agents are excellent at scanning for similar patterns across the codebase.
- **File issues before spawning agents.** Each agent needs a clear, scoped prompt with the issue number, root cause, and suggested fix approach. Issues force you to articulate these.
- **Verify worktree isolation first.** Run a quick test agent that writes a file and confirms it lands in the worktree, not the main repo. The isolation mechanism has been broken before.
- **Cherry-pick, don't merge worktree branches.** Agent branches have auto-generated names and may be based on stale commits. Cherry-picking onto fresh fix branches from main is cleaner.

### For this codebase

- **Test with `InfraSubDir != "."`**. The default `"."` masked path bugs for months because `filepath.Join(x, ".")` == `x`. Any path logic touching `InfraSubDir` should be tested with a real subdirectory value.
- **Distinguish "expected absence" from "unexpected absence."** Many silent failures were written for graceful degradation but don't distinguish first-run (file doesn't exist yet) from misconfiguration (file should exist but doesn't). The pattern: check existence explicitly before the operation, not inside the error handler.

## Key Takeaways

- One bug fix + systematic pattern search found 5 more bugs in the same codebase in ~10 minutes
- Agent worktree isolation works (as of 2026-03-23) — 5 agents wrote to 5 isolated directories without conflict
- Cherry-pick from worktree branches to properly named fix branches is the clean path to PRs
- Silent failures are the most dangerous bug class in GitOps tooling — the system reports success while doing nothing
- `filepath.Join(x, ".")` == `x` is a testing blind spot — always test path logic with non-trivial subdirectory values
