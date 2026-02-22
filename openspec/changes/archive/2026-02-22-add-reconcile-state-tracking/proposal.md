# Change: Add Reconcile State Tracking

## Why

Bosun's reconcile pipeline uses git HEAD comparison as its only "changed" signal,
but git state and deployment state can diverge. When a reconcile is interrupted
mid-pipeline (e.g., `docker compose up` restarts bosun's own container), the git
repo is already at the latest commit but the deployment never completed. On next
trigger, bosun sees "no changes" and skips — leaving the system in a partially
deployed state with no way to recover except manual intervention.

This is a production-confirmed bug that hits the core of what bosun does.

## What Changes

- Add persistent deploy state file tracking last successfully deployed commit
- Replace git-diff-based skip logic with state-file-based skip logic
- Add attempt tracking with circuit breaker (3 failures → stop retrying)
- Add `Force` field to trigger API (`TriggerRequest`) for manual override
- Add `StateDir` configuration for persistent state storage (`/var/lib/bosun/`)
- Thread force flag through all trigger paths (socket, TCP, HTTP, CLI)
- Schema-versioned state file with atomic writes (fsync + same-fs rename)

## Impact

- Affected specs: reconcile (new)
- Affected code:
  - `internal/reconcile/reconcile.go` — state read/write, skip logic
  - `internal/reconcile/state.go` — new file, state persistence
  - `internal/daemon/daemon.go` — config, force flag threading
  - `internal/daemon/socket.go` — TriggerRequest change
  - `internal/daemon/tcp.go` — TriggerRequest change
  - `internal/daemon/server.go` — TriggerRequest change
  - `internal/daemon/api.go` — TriggerRequest change
  - `internal/cmd/trigger.go` — `--force` CLI flag
  - `internal/cmd/daemon.go` — StateDir config
