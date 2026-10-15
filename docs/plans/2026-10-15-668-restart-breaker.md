# Restart breaker scope guard (#668 unit 3)
Status: completed
Next action: root agent independently runs the staged red/green CI checks and publishes the authorized draft PR.

## Goal and completion criteria

Preserve #701's per-target restart-breaker behavior while preventing an
unresolved project scope from inspecting or stopping every running container
on the Docker host. The daemon must continue to use each target's independent
configuration and state file; named local targets with a blank project retain
the existing `compose` fallback. This unit excludes the #668 global project
resolver, `Config.ProjectNameFromFile`, startup/doctor redesign, and any other
security-hardening unit.

Completion requires the scoped code and tests, an OpenSpec proposal/design/
delta under `openspec/changes/harden-restart-breaker-scope/`, exact consumer
documentation, strict OpenSpec validation, formatting and diff checks, and a
staged-break recipe showing the unresolved-implicit-target test fails when the
guard is removed. Compiler-heavy gates remain pending because the agent Go gate
currently reports only 52.3 GiB free against its mandatory 100 GiB floor.

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
- [x] Prepare `/private/tmp/cos-bosun668-assessment/restart-breaker-staged-break.md` with a reversible guard-removal recipe and expected red test.
- [x] Run gofmt, diff checks, strict OpenSpec validation, and permitted targeted static checks.
- [x] Attempt required compiler-heavy checks through `scripts/agent-go-gate.sh`; record the resource-gate result without bypassing it.
- [x] Recheck branch/status, commit only explicit unit paths with the required trailers, and hand off for independent review.

## Review

The orchestrator's GO explicitly approves this unit and says to preserve #701,
add only the empty-scope guard and listed tests, and leave labels/push/PR work
to the root agent. Inline security and operability review is recorded in the
design and spec: empty scope fails closed before Docker inspection, state is
not advanced from untrusted host-wide observations, and the named-target
fallback remains bounded to the existing target path. The implementation is
committed as `ddae61b086d399cfbfafe6b7a4d8dea3a914c2cc`; no push, PR, label, or
shared-branch operation was performed.

## Completion evidence

Source and tests are implemented in the requested worktree from
`b1b557aeb140067c9caf6740a90cc6103476a4f0` (`origin/main`) and committed on
`cos/668-restart-breaker`. `gofmt -w` completed for all changed Go files,
`git diff --check origin/main HEAD` exited 0, `openspec validate
harden-restart-breaker-scope --strict` exited 0, and the staged-break patch
passes `git apply --check`. A fresh `git status --short --branch` reports the
branch two commits ahead of `origin/main` with no dirty paths.

The mandatory targeted command
`scripts/agent-go-gate.sh go test ./internal/reconcile ./internal/daemon -run
'^TestRunRestartBreaker$|^TestRunDriftCheckForTarget_UnresolvedImplicitProjectFailsClosed$'`
reached the gate and exited 75 because only 52.3 GiB was free against the
100 GiB minimum. No cache or resource override was used and no compiler ran;
root CI must supply the compiler/test evidence.
