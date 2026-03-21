# Agent Teams for Parallel Refactoring — Field Report

**Date:** 2026-03-20
**Type:** discovery
**Project:** bosun

## Goal

Use Claude Code agent teams to parallelize two large refactoring tasks that touch completely different packages. Split `diagnostics.go` (1240 LOC) in `internal/cmd/` and `reconcile.go` + `deploy.go` (3200 LOC) in `internal/reconcile/` simultaneously, reducing wall-clock time by running both in isolated worktrees.

## Pipeline Overview

```
1. Identify non-conflicting tasks (different packages, no shared files)
2. Create beads issues and mark in_progress
3. TeamCreate → 2 tasks
4. Spawn 2 agents with isolation: "worktree", mode: "bypassPermissions"
5. Each agent: read files → plan split → move code → run tests → commit → push → create PR
6. Team lead monitors, shuts down agents on completion
7. Review + fix CodeRabbit findings on each PR
8. TeamDelete to clean up
```

Total elapsed time from spawn to both PRs open: ~15 minutes. Sequential would have been ~25-30 minutes.

## What Worked

### File-Ownership Separation

The key constraint: **two agents editing the same file causes overwrites**. This session's tasks were ideal because they touch completely separate packages:

| Agent | Package | Files Touched |
|---|---|---|
| `refactor-diagnostics` | `internal/cmd/` | diagnostics.go → 5 new files |
| `refactor-reconcile` | `internal/reconcile/` | reconcile.go + deploy.go → 6 new files |

Zero overlap, zero conflicts. This is the sweet spot for agent teams.

### Detailed Spawn Prompts

Each agent got a comprehensive prompt with:

- Exact file paths and line counts
- Branch name to create
- Specific rules (pure refactor, no behavioral changes, run tests before commit)
- Conventional commit format
- PR creation instructions

The agents delivered exactly what was asked because the instructions were unambiguous. Vague prompts like "refactor the big files" would have produced inconsistent results.

### Worktree Isolation

`isolation: "worktree"` gave each agent its own copy of the repo. Benefits:

- No file conflicts between agents
- Each agent has its own git state (branch, working tree)
- Automatic cleanup via `TeamDelete`
- Agents can `go build` and `go test` without affecting the main working directory

### bypassPermissions Mode

`mode: "bypassPermissions"` let agents work autonomously without permission prompts. Essential for background agents — they can't ask the user for approval when running in parallel.

### Clean Shutdown Protocol

```
SendMessage → shutdown_request → agent approves → system terminates
```

Both agents shut down cleanly after completing their tasks. The team lead got a summary message from each before termination.

## Gotchas

### Worktree File Leakage

The diagnostics agent reported finding stale untracked files from the reconcile agent in the shared workspace:

> "Found stale untracked files from the reconcile refactor teammate (compose.go, ssh.go, target.go, etc.) polluting the shared workspace."

This suggests the worktree isolation isn't perfect — or more likely, one agent's `go build` cached artifacts leaked into the main module cache. The files were cleaned up but it's worth investigating whether `GOMODCACHE` or build cache is shared between worktrees.

### Agent Worktree Permission Failures (Known Issue)

From auto-memory: agents dispatched with `bypassPermissions` to `/tmp/` worktree paths can fail because the sandbox restricts tools to the project's primary working directory tree. Claude Code worktrees under `.claude/worktrees/` work better than `/tmp/` paths.

### Task Sizing

Both tasks were well-scoped (~5-6 discrete steps each). The reconcile task took longer (3200 LOC vs 1240 LOC) but both finished within a few minutes of each other. If tasks are asymmetric (one 30-minute, one 5-minute), the team overhead isn't worth it.

### CodeRabbit Review Load

Both PRs hit CodeRabbit simultaneously. The diagnostics PR got 20 findings, the reconcile PR got 14. These had to be fixed sequentially (by the team lead, since the agents were shut down). In hindsight, keeping agents alive for the review fix cycle would have saved time.

## Decisions Made

1. **Shut down agents after PR creation** — the agents completed their task (split + PR). Review fixes were done by the lead. Alternative: keep agents alive to fix their own review findings. Trade-off: more agent compute vs faster convergence
2. **bypassPermissions for both** — necessary for autonomous background work. Risk: agents could make destructive changes. Mitigation: worktree isolation limits blast radius
3. **Feature branches, not direct-to-main** — each agent created its own branch (`refactor/split-diagnostics`, `refactor/split-reconcile`). PRs went through full review before merge

## Recommendations

- **Only use teams when tasks have zero file overlap** — if two agents touch the same file, one's changes overwrite the other's. Check this before spawning
- **Size tasks at 5-6 steps** — too small and the team overhead isn't worth it; too large and agents lose coherence
- **Keep agents alive through review** — spawning agents just for the initial work, then fixing reviews manually, is a missed optimization. Consider: agent creates PR → lead reviews findings → sends fix instructions back to agent
- **Use `.claude/worktrees/` over `/tmp/`** — better sandbox compatibility
- **Suggest tmux before spawning** — `Ctrl+a |` to split panes, watch both agents work in real time. Users appreciate the visibility
- **TeamDelete cleans up worktrees** — don't forget to call it after shutdown. Stale worktrees accumulate

## Key Takeaways

- Agent teams cut wall-clock time by ~40% for this two-task parallel refactor (15 min vs ~25 min sequential)
- The constraint is file ownership, not compute — plan tasks around package boundaries
- Detailed spawn prompts with explicit rules produce reliable results; vague prompts produce inconsistent ones
- The biggest time sink was review fixes after agents shut down — keeping agents alive for the review cycle is the next optimization
- Worktree isolation mostly works but watch for build cache leakage between agents
