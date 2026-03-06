# Change: Fix remote deploy partial failure handling

## Why

Remote deploy uses SSH for both config sync (tar-over-SSH) and compose up. If
config sync succeeds but `ComposeUpRemote` fails, the `deployRemote()` method
swallows the error as a warning and returns `nil`. The reconciler then records
the commit as successfully deployed in the state file. On the next reconcile
cycle, the skip logic sees `last_deployed_commit == current_head` and skips the
pipeline entirely -- leaving the remote host with new configs on disk but
containers running the old version indefinitely.

This is worse than a visible failure: the system reports success while the remote
host is in an inconsistent state. No alert fires, no retry occurs, and the only
way to discover the problem is manual inspection.

## What Changes

1. **Remote compose up failure is no longer swallowed** -- `deployRemote()` SHALL
   return an error when `ComposeUpRemote` fails, so the reconciler treats it as a
   deploy failure (state not updated, failure alert sent, circuit breaker counts it)

2. **Local deploy parity** -- local deploy already fails on compose up errors and
   has rollback support. Remote deploy SHALL have equivalent error propagation
   (rollback is a separate concern and out of scope for this change)

3. **Compose up failure after successful config sync** -- the pipeline correctly
   aborts and the state file records the attempt (not the deployment), so the next
   reconcile re-runs the full pipeline including compose up

## Impact

- Affected specs: `reconcile`
- Affected code:
  - `internal/reconcile/reconcile.go` -- `deployRemote()` error propagation from
    `ComposeUpRemote` and `SignalContainerRemote`
  - `internal/reconcile/deploy.go` -- no changes needed (already returns errors)
  - `internal/reconcile/pure.go` -- no changes needed (skip logic already correct)
  - `internal/reconcile/state.go` -- no changes needed (state tracking already correct)
- All consumers:
  - `internal/reconcile/reconcile.go:doDeploy()` -- calls `deployRemote()`, propagates error to `Run()`
  - `internal/reconcile/reconcile.go:Run()` -- already handles `doDeploy` errors correctly (sends failure alert, does not update `LastDeployedCommit`)
  - `internal/daemon/daemon.go` -- calls `Run()`, already handles errors (dirty flag, logging)
  - `internal/cmd/reconcile.go` -- calls `Run()`, already handles errors (exit code)
