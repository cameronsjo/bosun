## 1. Compose Up: Remove `--wait` and Add Health Inspection

- [x] 1.1 Remove `--wait` from compose args in `ComposeUpMultiple()` (deploy.go:714)
- [x] 1.2 Health inspection via existing `verifyPostDeploy()` and drift detection (no new `InspectHealthStatus` needed)
- [x] 1.3 Enhanced `verifyPostDeploy()` to categorize containers and alert on unhealthy
- [x] 1.4 `ComposeUpRemote()` already omits `--wait` — no change needed

## 2. Alert on All Failure Paths

- [x] 2.1 Add `sendThrottledFailureAlert()` call to circuit breaker path
- [x] 2.2 Add `LastAlertedAttempt int` field to `DeployState` struct
- [x] 2.3 Implement `ShouldAlert()` with exponential backoff schedule (1, 3, 10, 30, every 30)
- [x] 2.4 Add `SendUnhealthyContainers(ctx, target, containers)` to AlertSender and alert.Manager
- [x] 2.5 Call `sendUnhealthyAlert()` from `verifyPostDeploy()` when unhealthy containers detected
- [x] 2.6 Add `sendRecoveryAlert()` when deploy succeeds after previous failures
- [x] 2.7 Add `SendDeployRecovery()` to AlertSender and alert.Manager
- [x] 2.8 Write tests: `TestShouldAlert` with 15 cases covering all throttle thresholds
- [x] 2.9 Write tests: `TestManager_SendDeployRecovery` for recovery alert format

## 3. Post-Sync Container Restart Hooks

- [x] 3.1 Add `PostSyncHook` config struct with `Paths`, `Action`, `Container`
- [x] 3.2 Add `PostSyncHooks []PostSyncHook` to `reconcile.Config`
- [x] 3.3 Add `BOSUN_POST_SYNC_HOOKS` env var parsing (JSON array) in daemon `ConfigFromEnv()`
- [x] 3.4 Implement `EvaluatePostSyncHooks()` with glob matching (supports `**`)
- [x] 3.5 Add `DiffFiles()` to `GitOperations` using go-git tree diff for changed file detection
- [x] 3.6 Add `ExecutePostSyncHooks()` to restart containers via Docker SDK
- [x] 3.7 Wire `executePostSyncHooks()` into reconcile pipeline after successful deploy
- [x] 3.8 Write tests: `TestMatchGlob` with 10 cases (exact, star, doublestar, boundary)
- [x] 3.9 Write tests: `TestEvaluatePostSyncHooks` with 6 cases (match, multi, none, empty, dedup)

## 4. Verification

- [x] 4.1 `go build ./...` passes
- [x] 4.2 `go test ./... -count=1` all pass
- [ ] 4.3 `golangci-lint run ./...` zero issues
- [ ] 4.4 Manual test: deploy with unhealthy container present, verify deploy succeeds and alert fires
- [ ] 4.5 Manual test: trigger circuit breaker, verify Discord alert received
- [ ] 4.6 Manual test: change traefik config, verify traefik container restarted
