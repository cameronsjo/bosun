## 1. State Persistence Layer

- [x] 1.1 Create `internal/reconcile/state.go` with `DeployState` struct:
  - `SchemaVersion int` (always 1)
  - `LastDeployedCommit string`
  - `DeployedAt time.Time`
  - `Source string`
  - `LastAttemptedCommit string`
  - `AttemptCount int`
- [x] 1.2 Implement `LoadState(path string) (*DeployState, error)` — reads JSON, returns zero state on missing/corrupt file, logs warning on corrupt
- [x] 1.3 Implement `SaveState(path string, state *DeployState) error` — atomic write with fsync:
  - Create temp file in same directory as target (`os.CreateTemp(filepath.Dir(path), ...)`)
  - Write JSON → fsync temp file → rename to target → fsync directory
- [x] 1.4 Add `StateFile` field to `reconcile.Config` with default `"/var/lib/bosun/deploy-state.json"`
- [x] 1.5 Write tests for LoadState (missing file, corrupt file, valid file, schema version mismatch) and SaveState (atomic write, fsync, permissions)

## 2. Replace Skip Logic in Reconciler

- [x] 2.1 In `reconcile.Run()`, after `syncRepo()`, call `LoadState()` to read last deployed commit
- [x] 2.2 Replace `!changed && !r.config.Force` check with `state.LastDeployedCommit == after && !r.config.Force`
- [x] 2.3 Before pipeline execution, update `LastAttemptedCommit` and increment `AttemptCount` in state (reset count if commit changed)
- [x] 2.4 Add circuit breaker: if `AttemptCount >= 3` on same commit and `!force`, log error and skip (surface as degraded health)
- [x] 2.5 At end of `Run()` (after cleanupStaging, before return nil), call `SaveState()` with current commit and reset `AttemptCount`
- [x] 2.6 Keep the `changed` bool and `before`/`after` for logging — still useful to know if git moved
- [x] 2.7 Write test: interrupted deploy (state file absent) → next run deploys even though git shows no changes
- [x] 2.8 Write test: successful deploy (state file matches HEAD) → next run skips correctly
- [x] 2.9 Write test: force flag overrides state file match
- [x] 2.10 Write test: 3 consecutive failures on same commit → circuit breaker trips, skips until new commit or force
- [x] 2.11 Write test: force flag overrides circuit breaker

## 3. Force Flag in Trigger API

- [x] 3.1 Add `Force bool` field to `TriggerRequest` in `internal/daemon/socket.go`
- [x] 3.2 Update `TriggerReconcile` signature to accept force flag: `TriggerReconcile(ctx, source string, force bool) error`
- [x] 3.3 Thread force flag through `reconcileLoop` → `executeReconcile` → set `reconciler.config.Force` before `Run()`
- [x] 3.4 Update all callers of `TriggerReconcile`: socket, TCP, HTTP webhook, HTTP API, GitHub webhook, manual trigger, poll loop
- [x] 3.5 Update `handleAPITrigger` in `api.go` to parse Force from request body
- [x] 3.6 Update `handleTrigger` in `socket.go` to parse Force from request body
- [x] 3.7 Update `handleTrigger` in `tcp.go` to parse Force from request body
- [x] 3.8 Write test: trigger with force=true bypasses state check

## 4. CLI Integration

- [x] 4.1 Add `--force` flag to `bosun trigger` command
- [x] 4.2 Thread force flag through socket client to trigger request
- [x] 4.3 Add `--force` flag to `bosun reconcile` command (already has `Force` in Config, just wire env var)

## 5. Configuration

- [x] 5.1 Add `StateDir` to daemon `Config` struct (default: `/var/lib/bosun/`)
- [x] 5.2 Add `BOSUN_STATE_DIR` environment variable loading in `ConfigFromEnv()`
- [x] 5.3 Derive `reconcile.Config.StateFile` from `StateDir` + `"deploy-state.json"`
- [x] 5.4 Ensure state directory is created on daemon startup (like socket directory)
- [x] 5.5 Log startup warning if state directory appears to be on tmpfs (`/var/run/`, `/tmp/`, or check mount type)

## 6. Verification

- [x] 6.1 `go build ./...` passes
- [x] 6.2 `GOOS=windows go build ./...` passes
- [x] 6.3 `go test ./... -count=1` all pass
- [x] 6.4 `golangci-lint run --new-from-rev=main ./...` zero issues
- [x] 6.5 Manual test: start daemon, trigger reconcile, verify state file written
- [x] 6.6 Manual test: kill daemon mid-reconcile, restart, verify it re-runs
