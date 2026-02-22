# Consumer Parity: When Specs Miss Half the Pipeline — Field Report

**Date:** 2026-02-22
**Type:** investigation
**Project:** bosun

## Goal

Fix two production bugs in Bosun's GitOps daemon and trace both to a single process gap in the OpenSpec workflow — then codify the fix so the same class of bug can't recur.

## Root Cause

Both bugs shared the same origin: the `add-hook-config` proposal (Closes #38) scoped the daemon out of its "Affected code" list. The proposal framed the problem as "CLI has no hook loading" and treated the daemon's existing env var path as already working. Three layers compounded:

1. **Proposal** listed `internal/cmd/reconcile.go` but not `internal/daemon/daemon.go`
2. **Spec** prose said "both daemon and CLI modes" but scenarios only covered CLI
3. **Task list** had "CLI Reconcile Integration" but no daemon task

Implementation faithfully followed the tasks. The daemon was never touched.

The dirty repo bug was a separate but related blind spot — the Go rewrite changed `git pull` to `fetch + hard reset`, but the dirty-state check still assumed the old bash behavior where `git pull` fails on uncommitted changes.

## What We Tried

### Bug 1: Daemon ignores `post_sync_hooks` from `bosun.yaml` (#49)

**Symptom**: Hooks defined in `bosun.yaml` worked via `bosun reconcile` (CLI) but were silently ignored when the daemon reconciled via webhook/poll. Three incidents in 9 days before the env var workaround was applied.

**Investigation**: Compared `internal/cmd/reconcile.go:146` (calls `config.Load()`) vs `internal/daemon/daemon.go:881` (only checks `BOSUN_POST_SYNC_HOOKS` env var). The daemon's `ConfigFromEnv()` never called `config.Load()`.

**Fix**: 3-line addition to `ConfigFromEnv()` — call `config.Load()` before the env var check, matching the CLI pattern exactly. Added 3 tests (config file loading, env var override, no config fallback).

### Bug 2: Dirty repo blocks daemon reconciliation

**Symptom**: Daemon stops deploying entirely when the clone has stale artifacts from template rendering or FUSE symlink diffs. Every reconciliation attempt fails with "repository has uncommitted changes."

**Investigation**: `Pull()` at `git.go:280-284` hard-fails on dirty state. But `git.go:351` does a hard reset to the remote ref — which discards local changes anyway. The dirty check was guarding against a problem the implementation had already solved.

**Fix**: Changed the dirty check from hard-fail to warn-and-proceed. The hard reset is the recovery path.

## Decisions Made

### Consumer Parity Rule

After diagnosing the spec gap, we codified a new rule in `openspec/AGENTS.md` at three enforcement points:

| Point | What it catches |
|-------|-----------------|
| Proposal template ("All consumers" field) | Forces grep for every file that touches the affected type |
| Scenario formatting rule | Requires per-consumer scenario when requirement names multiple consumers |
| Best practices ("Consumer Parity" section) | Explains the anti-pattern with rationale |

**The key sentence**: *"Implementation follows scenarios, not prose."* If a requirement says "both daemon and CLI," both need scenarios and tasks — or the implementation will only cover the one that has them.

### Warn vs Fail on Dirty Repo

The old behavior (fail) was a safety gate from the bash era where `git pull` genuinely fails on dirty trees. The Go implementation uses `fetch + hard reset`, making the safety gate the thing that blocks recovery. Design decision: observability (warn) over safety theater (fail on something the next line fixes).

Updated the OpenSpec reconcile spec requirement and scenario to match — "SHALL warn" instead of "SHALL reject."

## Gotchas

- **CLAUDE.md is a symlink to AGENTS.md** — `git add CLAUDE.md` stages nothing; must `git add AGENTS.md`
- **`ReconcileConfig` is a pointer** — initially suspected a second bug where `cfg.ReconcileConfig = rcfg` copied a struct value before hooks were set, but `*reconcile.Config` means the pointer is shared
- **OpenSpec PostHog telemetry errors** are harmless noise from network blocking — `openspec validate` succeeds despite ECONNREFUSED spam
- **Release-please auto-tags** on `fix:` commits — two bug fixes triggered v0.7.2 automatically, which then caused a push rejection until `git pull --no-rebase`

## Key Takeaways

- **When a spec requirement names multiple consumers, each needs its own scenario and task.** Prose saying "both X and Y" is not sufficient — only scenarios get implemented.
- **"Affected code" in a proposal should include every consumer of the changed data, even ones that "already work."** A grep at proposal time would have caught `daemon.go` immediately.
- **Safety gates from a previous implementation may become blockers in a rewrite.** The dirty-repo check was correct for bash `git pull` but wrong for go-git `fetch + hard reset`.
- **The fix for a process bug is earlier than you think.** The daemon bug wasn't an implementation failure — it was a scoping failure in the proposal. The consumer parity rule catches it at proposal time, not code review time.
- **Test the scenarios, not the prose.** If you can't point to a scenario that would fail if the code is wrong, the spec isn't protecting you.
