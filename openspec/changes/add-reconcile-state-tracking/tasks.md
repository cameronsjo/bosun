## 1. State Persistence Layer

- [ ] 1.1 Create `internal/reconcile/state.go` with `DeployState` struct (`LastDeployedCommit string`, `DeployedAt time.Time`, `Source string`)
- [ ] 1.2 Implement `LoadState(path string) (*DeployState, error)` — reads JSON, returns zero state on missing/corrupt file
- [ ] 1.3 Implement `SaveState(path string, state *DeployState) error` — atomic write (temp file + rename)
- [ ] 1.4 Add `StateFile` field to `reconcile.Config` with default `"/var/run/bosun/deploy-state.json"`
- [ ] 1.5 Write tests for LoadState (missing file, corrupt file, valid file) and SaveState (atomic write, permissions)

## 2. Replace Skip Logic in Reconciler

- [ ] 2.1 In `reconcile.Run()`, after `syncRepo()`, call `LoadState()` to read last deployed commit
- [ ] 2.2 Replace `!changed && !r.config.Force` check with `state.LastDeployedCommit == after && !r.config.Force`
- [ ] 2.3 At end of `Run()` (after cleanupStaging, before return nil), call `SaveState()` with current commit
- [ ] 2.4 Keep the `changed` bool and `before`/`after` for logging — still useful to know if git moved
- [ ] 2.5 Write test: interrupted deploy (state file absent) → next run deploys even though git shows no changes
- [ ] 2.6 Write test: successful deploy (state file matches HEAD) → next run skips correctly
- [ ] 2.7 Write test: force flag overrides state file match

## 3. Force Flag in Trigger API

- [ ] 3.1 Add `Force bool` field to `TriggerRequest` in `internal/daemon/socket.go`
- [ ] 3.2 Update `TriggerReconcile` signature to accept force flag: `TriggerReconcile(ctx, source string, force bool) error`
- [ ] 3.3 Thread force flag through `reconcileLoop` → `executeReconcile` → set `reconciler.config.Force` before `Run()`
- [ ] 3.4 Update all callers of `TriggerReconcile`: socket, TCP, HTTP webhook, HTTP API, GitHub webhook, manual trigger, poll loop
- [ ] 3.5 Update `handleAPITrigger` in `api.go` to parse Force from request body
- [ ] 3.6 Update `handleTrigger` in `socket.go` to parse Force from request body
- [ ] 3.7 Update `handleTrigger` in `tcp.go` to parse Force from request body
- [ ] 3.8 Write test: trigger with force=true bypasses state check

## 4. CLI Integration

- [ ] 4.1 Add `--force` flag to `bosun trigger` command
- [ ] 4.2 Thread force flag through socket client to trigger request
- [ ] 4.3 Add `--force` flag to `bosun reconcile` command (already has `Force` in Config, just wire env var)

## 5. Configuration

- [ ] 5.1 Add `StateDir` to daemon `Config` struct (default: directory of lock file)
- [ ] 5.2 Add `BOSUN_STATE_DIR` environment variable loading in `ConfigFromEnv()`
- [ ] 5.3 Derive `reconcile.Config.StateFile` from `StateDir` + `"deploy-state.json"`
- [ ] 5.4 Ensure state directory is created on daemon startup (like socket directory)

## 6. Verification

- [ ] 6.1 `go build ./...` passes
- [ ] 6.2 `GOOS=windows go build ./...` passes
- [ ] 6.3 `go test ./... -count=1` all pass
- [ ] 6.4 `golangci-lint run --new-from-rev=main ./...` zero issues
- [ ] 6.5 Manual test: start daemon, trigger reconcile, verify state file written
- [ ] 6.6 Manual test: kill daemon mid-reconcile, restart, verify it re-runs
