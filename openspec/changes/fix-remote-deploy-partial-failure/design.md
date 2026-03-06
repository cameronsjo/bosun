## Context

Remote deploy (`deployRemote()` in `reconcile.go`) performs two SSH-based
operations in sequence:

1. **Config sync** -- `DeployRemote()` / `DeployRemoteFile()` copies rendered
   configs to the remote host via tar-over-SSH or SCP
2. **Service reload** -- `ComposeUpRemote()` runs `docker compose up` on the
   remote host via SSH

If step 1 succeeds but step 2 fails, the remote host has new config files on disk
but containers running the old configuration. Currently, `deployRemote()` swallows
the `ComposeUpRemote` error as a `ui.Warning` and returns `nil`, causing the
reconciler to record a successful deployment.

### Root Cause Analysis

The skip logic in `Run()` (line 286) compares `state.LastDeployedCommit` against
the current git HEAD:

```go
if shouldSkipDeploy(state.LastDeployedCommit, after, r.config.Force) {
    // skip pipeline
}
```

When `deployRemote()` returns `nil`, `Run()` proceeds to line 408:

```go
state.LastDeployedCommit = after
```

On the next reconcile, `shouldSkipDeploy` returns true because the commit matches.
The pipeline never re-runs, and compose up never retries.

The existing state tracking infrastructure (`LastAttemptedCommit`, `AttemptCount`,
circuit breaker) already handles the retry case correctly -- but only if the
deploy method returns an error.

## Goals / Non-Goals

**Goals:**

- Remote compose up failures are treated as deploy failures (error returned,
  state not updated, alerts fired, circuit breaker tracks it)
- Next reconcile re-runs the full pipeline (including compose up) because
  `LastDeployedCommit` was not updated
- Local and remote deploy have consistent error handling semantics

**Non-Goals:**

- Remote rollback support (local already has `ComposeUpWithRollback`, remote
  does not -- this is a separate feature)
- Partial config sync recovery (if config sync itself fails partway, that is
  already handled by the existing error returns)
- Remote `SignalContainerRemote` failure escalation (SIGHUP for agentgateway
  is best-effort and should remain a warning)

## Decisions

### Decision: Propagate ComposeUpRemote errors as deploy failures

The fix is to change `deployRemote()` to return the `ComposeUpRemote` error
instead of swallowing it. This is a one-line change that leverages all existing
infrastructure:

- `Run()` already does not update `LastDeployedCommit` on deploy failure
- `Run()` already sends throttled failure alerts on deploy failure
- `Run()` already increments `AttemptCount` before deploy
- The circuit breaker already activates after 3 failures on the same commit
- The next reconcile already re-runs when `LastDeployedCommit != current_head`

**Alternatives considered:**

1. **Track partial failure in a new state field** -- Add `LastAttemptStage` or
   `ConfigSyncedButNotApplied` to `DeployState`. Rejected because the existing
   infrastructure already handles this correctly once the error is propagated.
   Adding new state fields introduces complexity without benefit.

2. **Always re-run compose up if last attempt failed** -- Add logic to
   `shouldSkipDeploy` to check `AttemptCount > 0`. Rejected because this is
   already the natural behavior when the error is propagated: `LastDeployedCommit`
   stays at the previous value, so the skip logic correctly detects a mismatch
   and re-runs.

3. **Verify running state matches deployed configs post-deploy** -- Add a
   remote health check after compose up. Rejected as over-engineering for this
   fix. Post-deploy verification already exists for local deploys and can be
   extended to remote separately.

### Decision: Keep SignalContainerRemote as best-effort

The `SignalContainerRemote` call (SIGHUP to agentgateway) is a convenience
reload, not a deployment-critical operation. If the signal fails, the container
will pick up the new config on its next restart. This should remain a warning.

## Risks / Trade-offs

- **Risk:** Config files are on disk but compose up failed, so the next reconcile
  re-runs the full pipeline including re-syncing the same configs. This is
  wasteful but safe -- content-hash sync means unchanged files are not rewritten.
- **Risk:** If compose up fails due to a transient SSH error, the circuit breaker
  will trip after 3 attempts. This is the desired behavior -- the operator should
  investigate persistent SSH failures.
- **Trade-off:** Remote deploy loses the "partial success" semantics where config
  sync succeeded. This is intentional -- partial success was actually silent
  failure.

## Open Questions

None. The fix is straightforward and leverages existing infrastructure.
