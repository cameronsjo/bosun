# Deploy Resilience Implementation — Field Report

**Date:** 2026-02-21
**Type:** architecture
**Project:** bosun

## Goal

Evaluate three related OpenSpec proposals that form a dependency chain for bosun's deploy reliability story, then implement the remaining ~20% of the third proposal (`add-deploy-resilience`). The trilogy covers state tracking, drift detection, and failure tolerance — the goal was to close out the entire body of work.

## Architecture

The three proposals form a clear maturity ladder for bosun's reconciliation engine:

| Proposal | Purpose | Status at Session Start |
|---|---|---|
| `add-reconcile-state-tracking` | Persistent deploy state (JSON state file, circuit breaker) | Fully implemented |
| `add-state-feedback-loop` | Drift detection (declared vs actual container state) | Fully implemented |
| `add-deploy-resilience` | Graceful failure handling (throttled alerts, --wait removal, post-sync hooks) | ~80% implemented |

Each builds on the previous: you need persistent state before you can track failures, you need drift detection before you can alert on unhealthy containers, and you need throttled alerting before you can remove `--wait` (which would otherwise spam alerts for every unhealthy container).

### What Was Already in Place

The foundation was solid. `DeployState` already tracked `LastDeployedCommit`, `LastAttemptedCommit`, and `AttemptCount` with atomic file writes (fsync + same-filesystem rename). The circuit breaker tripped after `MaxAttempts` (3) consecutive failures on the same commit. Drift detection compared rendered compose YAML against live Docker state via `bosun drift`.

### What Was Missing

Three distinct capabilities, each with its own beads issue:

1. **Alert throttling** (bosun-tq0) — failure alerts fired on every attempt with no backoff
2. **`--wait` removal** (bosun-bi3) — `docker compose up --wait` exits non-zero for ANY unhealthy container, blocking all deployments even when the unhealthy container is unrelated
3. **Post-sync hooks** (bosun-65i) — no mechanism to restart containers when their config files change during a deploy

## Decisions Made

### Alert Throttling: Attempt-Based vs Time-Based

Chose **attempt-based** throttling with a static threshold table (`1, 3, 10, 30, then every 30th attempt`) over timer-based backoff. Rationale: bosun's reconciliation is event-driven (webhook or poll triggers), not continuous. Attempt count is the natural dimension — if a deploy fails, the next attempt may be minutes or hours later depending on when new commits arrive. Wall-clock timers would either expire too fast (missing repeated failures) or too slow (delaying legitimate first alerts).

The `ShouldAlert()` function is a pure function of `(attemptCount, lastAlertedAttempt)` with no time dependency, making it trivially testable (15 test cases cover all thresholds and edge cases). The `LastAlertedAttempt` field persists in the state file so throttling survives daemon restarts.

The circuit breaker (attempt `MaxAttempts` = 3) always triggers an alert regardless of throttle position — you always want to know when bosun gives up.

### Compose Up: Why Remove `--wait`

Docker Compose's `--wait` flag is all-or-nothing. If ANY container in the compose project has `healthcheck` defined and is unhealthy, `docker compose up --wait` exits non-zero. In a homelab stack where traefik might be restarting or a database is slow to initialize, this blocks deployment of entirely unrelated services.

The fix: remove `--wait`, let compose bring services up in detached mode, then inspect health via the existing `verifyPostDeploy()` drift detection. If unhealthy containers are found, a targeted alert fires (via `SendUnhealthyContainers`) rather than failing the entire deploy.

### Post-Sync Hooks: Git Diff vs Rsync Change Tracking

Chose **go-git tree diff** (`object.DiffTree`) between the previous and current commit hashes to determine which files changed. Alternative was tracking rsync's `--itemize-changes` output, but that only works for remote deploys and doesn't cleanly map to source repo paths.

The hook config is a JSON array in `BOSUN_POST_SYNC_HOOKS`:

```json
[
  {"paths": ["traefik/conf.d/**"], "action": "restart", "container": "traefik"},
  {"paths": ["gatus/config.yaml"], "action": "restart", "container": "gatus"}
]
```

Glob matching uses prefix-based `**` rather than pulling in the `doublestar` library. The supported patterns — exact match, `*` (single segment), and `prefix/**` (recursive) — cover every realistic homelab config scenario without adding a dependency.

### Recovery Alerts

Added `sendRecoveryAlert()` that fires when a deploy succeeds after previous failures (`state.AttemptCount > 1`). This closes the alerting loop — you get notified when things break AND when they heal. The `SendDeployRecovery()` method on `alert.Manager` formats a clear "deploy recovered" message with the commit hash and previous attempt count.

## Gotchas

### Post-Sync Hook Ordering

When wiring `executePostSyncHooks()` into the reconcile pipeline, the initial placement was after `state.LastDeployedCommit = after`. This would have overwritten the previous commit hash needed for the git diff that determines which files changed. Fixed by capturing `previousCommit := state.LastDeployedCommit` before the state update block.

This is a subtle ordering dependency: the state update and the hook evaluation both need the "before" and "after" commit hashes, but in different phases. State update needs to record the new commit; hooks need to diff between old and new.

### Lint Baseline

`golangci-lint` reports 52 pre-existing `errcheck` issues across unrelated files (`ui/color.go`, `lock.go`, `snapshot.go`, `fileutil.go`). None are in files touched by this work. These are all `defer foo.Close()` or `fmt.Fprint()` patterns — real issues but not related to deploy resilience. Worth a separate cleanup pass.

### Interface Expansion

Adding `DiffFiles()` to the `GitOperations` interface required updating the mock in `state_integration_test.go`. This is the expected cost of Go interfaces — every new method on an interface must be implemented by all consumers, including test mocks. The mock returns `nil, nil` which is correct for the existing state tests that don't exercise hooks.

## Key Takeaways

- **Proposal evaluation before implementation catches dead work.** Two of three proposals were already fully implemented but never archived — evaluating first prevented duplicate effort and identified the actual remaining gap.
- **Alert throttling belongs at the state layer, not the transport layer.** Throttling by attempt count in the state file means it works across daemon restarts and regardless of which alert provider (Discord, Twilio, SendGrid) is configured.
- **`docker compose up --wait` is hostile to heterogeneous stacks.** It treats every unhealthy container as a fatal error. For homelabs where services have varying health profiles, decouple "bring up" from "verify health" into separate pipeline steps.
- **Capture `previousCommit` before updating state.** Any post-deploy hook that needs to know what changed must read the previous state before the pipeline overwrites it. This is an ordering contract that should be obvious from the code but easy to miss.
- **Static threshold tables beat dynamic algorithms for simple cases.** `[1, 3, 10, 30, every 30]` is readable, testable, and predictable. No exponential math, no floating point, no timer drift.
