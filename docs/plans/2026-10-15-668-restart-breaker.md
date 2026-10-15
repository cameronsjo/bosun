# Restart breaker scope guard (#668 unit 3)
Status: source implemented and posture diagnostics extended; fix2 mutation proof and restored-head CI pending
Delivery: [PR #714](https://github.com/cameronsjo/bosun/pull/714) carries current-head CI and readiness.

## Goal and completion criteria

Preserve #701's per-target restart-breaker behavior while preventing an
unresolved project scope from inspecting or stopping every running container
on the Docker host. The daemon must continue to use each target's independent
configuration and state file; named local targets with a blank project retain
the existing `compose` fallback. When the enabled implicit default target has
no resolved `project_name`, startup and `bosun doctor` must identify the
fail-closed posture and tell the operator to set `project_name`; the recurring
cycle message is debug-level. The diagnostic root-level name is not a runtime
scope resolver or deployment override.

Completion requires the scoped code and tests, an OpenSpec proposal/design/
delta under `openspec/changes/harden-restart-breaker-scope/`, exact consumer
documentation, strict OpenSpec validation, formatting and diff checks, and a
staged-break recipe showing the unresolved-implicit-target test fails when the
guard is removed. Compiler-heavy local gates remain pending because the agent Go gate
reported a fresh fix2 attempt below its mandatory 100 GiB free-space floor.

## Approach and alternatives

Pass the already-resolved per-target project name into `reconcile.RunRestartBreaker`
and guard before restart-count collection. When the scope is empty, return the
existing restart tracking unchanged and perform no Docker inspection or stop.
This places the guard at the destructive operation boundary while leaving
read-only drift matching, target snapshots, config reload, per-target state,
and the named-target `compose` fallback unchanged.

The declined alternative is porting #668's global `RestartBreakerProjectName`
helper and root-level file-project state into the #701 daemon. That would
collapse multiple target scopes and bypass the current per-target model.

## Work

- [x] Read repository, OpenSpec, brief, and source-branch instructions and inspect #701's target-scoped implementation.
- [x] Write this approved implementation plan and the scoped OpenSpec proposal, design, tasks, and delta before source edits.
- [x] Add the empty-scope guard and pass the target scope through the existing breaker call.
- [x] Keep and extend tests for unrelated containers, independent target state, and unresolved implicit target no-action behavior.
- [x] Update `docs/gitops.md` and `skills/onboard/resources/gitops.md` to describe the bounded per-target semantics.
- [x] Prepare a reversible guard-removal recipe with the direct and daemon tests and expected red result.
- [x] Run gofmt, diff checks, strict OpenSpec validation, and permitted targeted static checks.
- [x] Attempt required compiler-heavy checks through `scripts/agent-go-gate.sh`; record the resource-gate result without bypassing it.
- [x] Recheck branch/status, commit only explicit unit paths with the required trailers, and hand off for independent review.
- [x] Add startup and `bosun doctor` posture checks with tests; keep the root-level file name diagnostic-only.
- [ ] Run the fix2 warning/check mutation red-green proof and record restored-head CI.

## Review

The orchestrator's fix2 approval extends the unit to startup and doctor
posture reporting. Inline security and operability review is recorded in the
design and spec: empty scope fails closed before Docker inspection, state is
not advanced from untrusted host-wide observations, the named-target fallback
remains bounded to the existing target path, and an unresolved implicit default
is visible before operators rely on the breaker. The root-level file name is
diagnostic-only; per-target runtime scope remains authoritative.

## Completion evidence

Source, tests, and posture diagnostics are implemented in the requested
worktree on `cos/668-restart-breaker`. `gofmt -w` completed for all changed Go
files, `git diff --check origin/main HEAD` exited 0, `openspec validate
harden-restart-breaker-scope --strict` exited 0, and the guard-removal patch
passes `git apply --check`. A fresh `git status --short --branch` reports a
clean branch; compiler/runtime evidence remains CI-owned.

The initial guard-focused command
`scripts/agent-go-gate.sh go test ./internal/reconcile ./internal/daemon -run
'^TestRunRestartBreaker$|^TestRunDriftCheckForTarget_UnresolvedImplicitProjectFailsClosed$'`
was blocked at the gate with exit 75 by the mandatory free-space floor. The
fresh fix2 diagnostic command
`scripts/agent-go-gate.sh go test ./internal/config ./internal/daemon ./internal/cmd -run
'^TestLoad_ProjectNameFromFile$|^TestWarnRestartBreakerScopePosture$|^TestCheckRestartBreakerScope$'`
was likewise blocked with exit 75; no cache or resource override was used and
no compiler ran. Root CI supplies the compiler/runtime evidence.

## Published negative-control evidence

Initial guard-removal CI38760065810 compiled and linted, then failed the direct empty-scope and unresolved implicit-target tests because Docker inspection was reached. The reviewed guard was restored, and restored-head CI38760998225 passed the required Test, Lint, Build, WebUI, site-build, and patch-coverage checks. The daemon fixture compares the persisted JSON baseline with the persisted result, avoiding monotonic-clock fields lost during serialization; the inspection/stop assertions are unchanged. Fresh fix2 static checks remain recorded above, while the fix2 mutation recipe and restored-head CI remain pending.
