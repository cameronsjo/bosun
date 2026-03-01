# Pure Function Extraction Sprint — Field Report

**Date:** 2026-03-01
**Type:** architecture
**Project:** bosun

## Goal

Systematically identify and extract pure functions from bosun's core packages to make the codebase more testable. The coverage numbers (daemon 50%, cmd 40%, reconcile 65%) were stuck because the business logic lived inside side-effectful functions — HTTP handlers, filesystem operations, Docker SDK calls. The strategy: separate decisions from effects, test the decisions directly.

## Architecture

### The Pattern: Functional Core, Imperative Shell

Every low-coverage function in bosun followed the same anti-pattern:

```go
func runSomething() {
    data := fetchFromSideEffect()     // Docker, filesystem, HTTP
    decision := if/else/switch(data)   // ← pure logic, buried
    performSideEffect(decision)        // print, write, send
}
```

The fix: extract the middle into a pure function. The original becomes a thin wrapper that fetches data, calls the pure function, and acts on the result. The pure function is trivially testable with table-driven tests — no mocks, no I/O, sub-millisecond execution.

### Analysis Phase

We ran four parallel explore agents across the entire codebase to identify extraction candidates:

| Agent | Packages | Candidates Found |
|-------|----------|----------------:|
| reconcile | reconcile.go, deploy.go, git.go, hooks.go, sops.go | 25 |
| daemon | api.go, server.go, socket.go, tcp.go, daemon.go | 12 |
| cmd | diagnostics.go, drift.go, provision.go, validate.go, upgrade.go | 25 |
| supporting | docker, tunnel, config, ui, preflight | 22 |

Total: **84 extraction candidates** identified across the codebase.

### Prioritization

We triered the candidates by ROI (coverage gain per LOC of effort):

- **Tier 1** (highest ROI): reconcile circuit breaker, skip logic, target host resolution; daemon drift status, status response, container summary
- **Tier 2** (medium ROI): docker stats math, config env overrides, cmd port parsing, diff formatting
- **Tier 3** (structural): ui consolidation (13 near-identical print functions), preflight deduplication, diagnostics file split

### God Files Identified

Files over 1,000 LOC that are candidates for splitting (tracked in beads, not acted on this session):

| File | LOC | Responsibilities |
|------|----:|-----------------|
| cmd/diagnostics.go | 1,240 | Port parsing, cycle detection, Traefik validation, status output, manifest history |
| reconcile/deploy.go | 1,145 | SSH retry, backup management, compose orchestration, stale file detection, remote tar |
| reconcile/reconcile.go | 1,098 | Circuit breaker, state tracking, alert throttling, deploy orchestration, host resolution |
| daemon/daemon.go | 1,071 | Daemon lifecycle, polling, reconciliation coordination |

## What We Tried

### Agent Team Execution

Spawned a three-agent team with worktree isolation, each owning a non-overlapping set of packages:

| Agent | Packages | Isolation |
|-------|----------|-----------|
| reconcile-agent | internal/reconcile | worktree |
| daemon-agent | internal/daemon | worktree |
| supporting-agent | internal/cmd, docker, config | worktree |

Each agent received:
- Exact function locations (file:line)
- Target pure function signatures
- Instructions to write table-driven tests with both happy and unhappy cases
- Directive to note refactor opportunities without acting on them

## What Worked

### Extraction Results

All three agents delivered successfully. Final tally:

| Package | Functions Extracted | Test Cases | New Files |
|---------|-------------------:|-----------:|-----------|
| reconcile | 8 | 44 | `pure.go`, `pure_test.go` |
| daemon | 8 | 30 | `pure.go`, `pure_test.go` |
| docker | 3 | 30 | `stats_test.go` |
| config | 1 | 10 | `helpers_test.go` |
| **Total** | **20** | **114** | **6 files** |

### Coverage Movement

| Package | Before | After Extraction | After Wrappers | After Integration | Total Delta |
|---------|-------:|-----------------:|---------------:|------------------:|------------:|
| config | 73.1% | 77.8% | — | — | **+4.7%** |
| daemon | 50.5% | 53.7% | 65.4% | **82.5%** | **+32.0%** |
| docker | 68.7% | 69.8% | — | — | **+1.1%** |
| reconcile | 65.0% | 65.7% | — | **68.4%** | **+3.4%** |

*Higher coverage is better.*

### Test Quality

Both happy and unhappy paths covered. Standout examples:

- `resolveTargetHost`: 7 cases including wrong type in secrets map, missing key, nil map, non-string value
- `calculateCPUPercent`: 10 cases including zero/negative deltas, zero CPUs (real Docker edge cases from cgroup v1/v2 differences)
- `shouldTriggerCircuitBreaker`: 7 cases including force override, zero attempts, empty last attempted
- `classifySSHError`: 9 cases covering all error classes plus case insensitivity and empty input

### Integration Tests (Phase 3)

Following the extraction and wrapper sessions, a third phase added integration tests to exercise real listeners, mock Docker injection, and client-server roundtrips.

**Infrastructure changes:**

- Exported `MockDockerAPI` to `internal/docker/dockertest/mock.go` — usable from any package
- Added `dockerClientOverride` field to `Daemon` (2-line production change) — checked before `sync.Once` lazy init

**Test files created:**

| File | Tests | What It Covers |
|------|------:|---------------|
| `lifecycle_test.go` | 5 | Real Unix socket `Start()`/`Shutdown()`, TCP lifecycle, `Run()` cancellation |
| `roundtrip_test.go` | 8 | `Client` → real socket server health/status/trigger/config/ping, TCP auth |
| `api_docker_test.go` | 10 | Docker API handlers (containers/logs/restart) via injected mock |
| `drift_docker_test.go` | 8 | `CollectActualState` label filtering, `RunDriftCheck` missing/mismatch/clean |
| `drift_integ_test.go` | 3 | Daemon drift wrapper: skip-when-reconciling, state file update, no-state-file |
| `loops_test.go` | 5 | `pollLoop`/`driftCheckLoop` with short intervals and context cancellation |

**Gotcha:** macOS limits Unix socket paths to 104 bytes. `t.TempDir()` + `filepath.EvalSymlinks()` produced ~120 char paths, causing `bind: invalid argument`. Fix: `os.MkdirTemp("", "bs")` with a 2-char prefix keeps paths under 90 bytes.

### Diagnostics Already Pure

The supporting agent discovered that `cmd/diagnostics.go`'s port parsing and cycle detection functions were already extracted as pure functions with comprehensive tests from a previous session. No work needed — the coverage gap in cmd is in the `run*` orchestrators, not the logic.

## Gotchas

### The Coverage Paradox

Coverage deltas were smaller than projected because pure function extraction adds to both numerator (tested lines) and denominator (total lines). The `pure.go` files are 100% covered but they're *new code* — the original wrappers still exist with their untested I/O paths.

**The real win is structural, not numerical.** The decision logic can never regress. The wrapper tests are now *possible* where before they were impractical.

### Worktree + Team Agent Cleanup Race

Agent worktrees were cleaned up on shutdown, but the changes survived in the main working tree (the agents wrote to the shared filesystem despite worktree isolation). This was accidental — the correct behavior would have been for agents to commit to their worktree branches. For future team code-writing sessions: either skip worktree isolation or ensure agents commit before shutdown.

### `getEnvOrDefault` Replaced 7 Repetitions

The config package's `extractAlertConfig` had the same `if v := os.Getenv(key); v != "" { return v }` pattern copied 7 times. Each alert provider added its env vars by copy-pasting. This is a preview of the coupling that the planned plugin architecture (bosun-8yn) will eliminate.

## Decisions Made

1. **Pure functions are unexported** — they're implementation details, not public API. The existing public methods remain the interface.
2. **New files over inline** — extracted functions live in `pure.go` rather than being sprinkled into existing files. Makes the extraction visible and reviewable.
3. **Diagnostics left alone** — already had pure functions. Coverage gap is in orchestrators, which need a different testing approach.
4. **Alert plugin architecture deferred** — created epic bead (bosun-8yn) with touch point analysis, but no implementation this session.

## Outcome: Functional Core Rule

The retroactive extraction loop — write code, realize coverage is bad, add tests, coverage still bad, refactor to pure functions, repeat — is a recurring pattern. To break the cycle, we codified a **Functional Core** section in the global `rules/user/core-code-principles.md`:

- **MUST** extract decision logic into pure functions when the containing function also performs I/O
- **MUST** write and test the pure function before writing the I/O wrapper
- **SHOULD** keep I/O wrappers thin — fetch data, call pure function, act on result
- **SHOULD NOT** embed branching logic inside functions that read files, call APIs, or write output

This rule ships to every future session across all projects via `/rules:init-user`. The intent: make the "right the first time" approach the default, so extraction sprints like this one become unnecessary.

Committed to `cameronsjo/rules` repo (`e48d1ae`).

## Recommendations

### Next Steps for Coverage

1. ~~**Test the simplified wrappers**~~ — **Done.** Added 35 test cases in a follow-up session. Daemon coverage: 53.7% → 65.4%.
2. ~~**Target daemon wrappers first**~~ — **Done.** Socket, TCP, and API handlers plus alert sender wrappers all tested.
3. ~~**Integration tests for daemon**~~ — **Done.** 6 phases: exported mock, Docker injection, lifecycle, roundtrip, Docker API, drift, loops. Daemon coverage: 65.4% → 82.5% (+17.1%).
4. **Don't chase cmd/diagnostics coverage** — the uncovered code is interactive wizards and Docker-dependent status output. Integration tests are more appropriate than unit tests there.
5. **Remaining daemon gap (~17.5%)** — primarily `daemon.go:Run()` full lifecycle (signal handling, initial delay timer), `server.go` webhook handlers, and some pure.go error paths. Diminishing returns territory.
6. **Reconcile gap (~31.6%)** — `reconcile.go` orchestrator (21 zero-coverage functions), `deploy.go` SSH/remote deploy (9), `drift.go` state persistence paths (9). These are I/O-heavy functions requiring git repos and Docker.

### Beads Created During Sprint

| ID | Title | Type | Status |
|----|-------|------|--------|
| ~~bosun-yj0~~ | ~~Extract pure functions from internal/reconcile~~ | task | **Closed** |
| ~~bosun-ytt~~ | ~~Extract pure functions from internal/daemon~~ | task | **Closed** |
| ~~bosun-5bc~~ | ~~Extract pure functions from docker, tunnel, config, ui, preflight~~ | task | **Closed** |
| bosun-8yn | Epic: Alert provider plugin architecture | feature | Open |
| bosun-d9e | Refactor diagnostics.go: split 1240-line god file | task | Open |
| bosun-eln | Refactor reconcile.go + deploy.go: split 2200-line core | task | Open |

### Round 2: Coverage Targets (COMPLETED 2026-03-01)

Three-agent team, parallel worktrees, ~10 min wall-clock. All exceeded targets.

| ID | Package | Before | After | Target | Delta | Production Changes |
|----|---------|-------:|------:|-------:|------:|-------------------|
| ~~bosun-sal~~ | config | 77.8% | **98.4%** | 85%+ | **+20.6** | None |
| ~~bosun-d5j~~ | docker | 69.8% | **94.3%** | 80%+ | **+24.5** | `commandRunner` injection in compose.go |
| ~~bosun-g15~~ | alert | 89.3% | **97.6%** | 95%+ | **+8.3** | Injectable endpoints (sendgrid, twilio) |

*Higher coverage is better.*

### Round 3: Coverage Targets (PLANNED)

Remaining gaps cluster into two packages: reconcile (68.4%) and ui (57.6%).

| ID | Package | Current | Target | Approach | ROI |
|----|---------|--------:|-------:|----------|-----|
| bosun-bxv | reconcile | 68.4% | 75%+ | Drift enrichment, hook execution, deploy rollback, git branch ops | Medium |
| bosun-vjm | ui | 57.6% | 80%+ | JSON mode branch, Debug, Logger, WithComponent | **High** |

#### UI Package Plan (57.6% → 80%+)

**Why it's high ROI:** Every function has the same pattern — `if isConsoleMode() { color } else { log }`. The existing tests ONLY cover console mode because `init()` sets `FormatConsole`. Adding a JSON mode test helper covers the `else` branch of all 13 functions at once.

**Work items:**

1. **JSON mode helper** — Create `captureJSONOutput(fn)` that sets `log.FormatJSON`, captures log output, and restores. Test every function in JSON mode (Success→Info log with `success=true`, Error→Error log, etc.).

2. **Debug function** — 0% coverage. Call in both console and JSON mode.

3. **Logger and WithComponent** — 0% coverage. Trivial: call, assert non-nil return.

4. **Fatal/Fatalf** — 0% coverage. These call `os.Exit(1)` so they can't be tested directly. Options:
   - Make the exit function injectable (`var exitFn = os.Exit`) — test by replacing with noop
   - Accept 0% on these 2 functions (they're 12 lines total, identical to Error)
   - **Recommended:** inject `exitFn`, covers both the formatting AND exit logic

**Expected: ~25 new test cases, all in `color_test.go`.**

#### Reconcile Package Plan (68.4% → 75%+)

**Why it's medium ROI:** The uncovered code splits into two categories:
- **Testable without I/O** (~6.6% gain possible): drift enrichment, hook execution, rollback logic, git branch helpers
- **Requires real git/SSH/Docker** (~25% gap): Clone, Pull, DeployRemote, SSH auth — too expensive for unit tests

**Work items (target the testable gaps):**

1. **EnrichUnhealthyItems** (55.6%) — Test via `MockDockerAPI` injection. Cases: no drift, non-unhealthy items skipped, inspect error skipped, health log present, health log nil, output truncation at 200 chars.

2. **ExecutePostSyncHooks** (0%) — Needs a Docker client mock for `RestartContainer`. Test: empty hooks (noop), restart action, unsupported action skipped, per-hook delay with cancelled context, settle delay. **Requires making Docker client injectable** — add `restarter` interface or accept a `RestartContainer` func.

3. **ComposeUpMultipleWithRollback** (55.2%) — Partially tested. Missing: backup file missing (skip), all backup files missing, rollback exec failure. These need `exec.Command` injection similar to docker compose — the `DeployOps` struct already has a `DryRun` field but no command runner. **May need `commandRunner` injection on `DeployOps`** (same pattern as docker compose).

4. **Git branch operations** — `RemoteBranchExists` (58.3%), `IsDirty` (66.7%), `GetCommitMessage` (71.4%), `DiffFiles` (0%). These use go-git in-process, so they're testable with a temp git repo. Create a `testRepo(t)` helper that inits a bare repo with a commit.

5. **serviceNameFromContainer** (81.8%) — Missing edge cases in name extraction. Table-driven test additions.

**NOT targeting (diminishing returns):**
- `CheckSSHConnectivity` (15.4%) — needs real SSH
- `DeployRemote`/`DeployRemoteFile`/`EnsureRemoteDir` (12-19%) — needs SSH + tar
- `ComposeUpRemote` (30.4%) — needs remote Docker
- `getSSHAuth`/`getHostKeyCallback`/`getSSHAgentAuth`/`getSSHKeyFileAuth` (0-40%) — SSH agent/key file handling
- `reconcile.Run` (82.2%) — already high, remaining paths are I/O orchestration

**Expected: ~40 new test cases across drift, hooks, deploy, and git test files. Target 75%+, stretch 78%.**

#### Execution Plan

Two agents, parallel worktrees:

| Agent | Package | Files Touched | Estimated Cases |
|-------|---------|---------------|----------------:|
| ui-tester | internal/ui | `color.go` (exitFn), `color_test.go` | ~25 |
| reconcile-tester | internal/reconcile | `hooks.go` (restarter), `drift_test.go`, `hooks_test.go`, `deploy_test.go`, `git_test.go` | ~40 |

No file overlap. Same merge strategy as round 2.

**Not worth chasing (unchanged):**
- **cmd** (40%) — orchestrators calling Docker/Tailscale/filesystem. E2E territory.
- **daemon** (82.6%) — diminishing returns. Signal handling, full `Run()` lifecycle.
- **update** (0%) — self-update from GitHub releases. Network-dependent, low value.

### Refactor Candidates Noted by Agents (Not Implemented)

- **reconcile**: `retryWithBackoff` is a general utility — move to `internal/retry/`
- **reconcile**: `deployLocal`/`deployRemote` hardcode sync targets — config-driven deploy manifest would reduce 50+ lines
- **daemon**: `handleHealth` is identical boilerplate across 3 server types — share via free function
- **daemon**: `handleManualTrigger`/`handleWebhook` share reconciliation trigger pattern — extract `triggerWithTracking()`
- **config**: Alert provider env vars hardcoded in core config loader — each provider should own `ConfigFromEnv()` via interface
- **docker**: Container name extraction (`strings.TrimPrefix(name, "/")`) duplicated in multiple methods

## Key Takeaways

- **Extract the decision, not the whole function.** Pure function extraction is surgical — you pull out the `if/else` logic, not the I/O. The wrapper stays, but becomes trivially testable.
- **The coverage number isn't the goal.** The goal is making decision logic independently verifiable. Coverage follows when you test the now-simplified wrappers.
- **84 candidates across 23k LOC.** The codebase has grown organically with logic embedded in handlers. A systematic extraction pass — even partial — makes the entire codebase more testable.
- **Agent teams work for non-overlapping packages.** Three agents editing three different packages in parallel produced clean results. The key constraint: no two agents touch the same file.
- **God files (1k+ LOC) are the root cause.** Files like `diagnostics.go` (1,240 LOC) and `deploy.go` (1,145 LOC) accumulate responsibilities because there's no natural split point. The pure function extraction is a stepping stone to the real fix: splitting those files.
