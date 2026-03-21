# Change: Deploy state tracking and SSH known_hosts resolution

## Why

Two bugs compromise reconciliation correctness and SSH reliability in containerised
deployments:

1. **#170 — Failed reconciliation caches commit hash**: When the pipeline fails
   mid-run (e.g., template error), git has already advanced `HEAD` to the new
   commit. On the next trigger, deploy-relevance diffs start from that new commit
   rather than the last successfully deployed commit, potentially skipping files
   that were never actually deployed.

2. **#173 — known_hosts key mismatch after manual SSH test**: The SSH host key
   verification order includes `~/.ssh/known_hosts`, an ephemeral user-profile
   path that can be polluted by ad-hoc `ssh` commands inside the container.
   This causes go-git to pick up a stale or mismatched key and fail.

## What Changes

- **Deploy-relevance diff base**: The diff used by post-sync hooks (stage 8) and
  any deploy-path evaluation SHALL use `state.CommitHash` (last successfully
  deployed commit from the state file) rather than the pull's `commit_before`
  value. If no prior deploy exists (state file absent or `CommitHash` is empty),
  treat everything as changed.

- **SSH known_hosts search order**: Remove `~/.ssh/known_hosts` from the default
  resolution chain. The new order SHALL be:
  1. `BOSUN_SSH_KNOWN_HOSTS` (explicit override)
  2. `/config/known_hosts` (container convention)
  If neither is found, fall back to insecure mode with a warning (unchanged).
  `BOSUN_SSH_INSECURE_HOST_KEY=true` continues to skip verification entirely.

## Impact

- Affected specs: `specs/reconcile/spec.md`
- Affected code: `internal/reconcile/git.go`, `internal/reconcile/reconcile.go`
- All consumers:
  - `internal/reconcile/reconcile.go` — reads `PullResult.CommitBefore` for
    hook diffing; must switch to `state.CommitHash`
  - `internal/reconcile/git.go` — builds the `knownHostsCallback` using the
    three-path search order; must drop `~/.ssh/known_hosts`
  - `internal/reconcile/reconcile_test.go` — tests that assert hook diff base
  - `internal/reconcile/git_test.go` — tests that assert known_hosts resolution
