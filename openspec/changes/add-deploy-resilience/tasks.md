## 1. Compose Up: Remove `--wait` and Add Health Inspection

- [ ] 1.1 Remove `--wait` from compose args in `ComposeUpMultiple()` (deploy.go:714)
- [ ] 1.2 Add `InspectHealthStatus(ctx, projectName string) ([]ContainerHealth, error)` to Docker client wrapper
  - Returns container name, state (running/exited/restarting), health status (healthy/unhealthy/starting/none)
- [ ] 1.3 Add `PostDeployHealthCheck()` in reconcile pipeline after compose up succeeds
  - Wait startup grace period (existing config)
  - Call `InspectHealthStatus()`
  - Containers that exited or are restarting: log error, include in deploy result
  - Containers that are unhealthy: log warning, trigger unhealthy alert
  - Containers healthy or no healthcheck: success
- [ ] 1.4 Update `ComposeUpRemote()` to also remove `--wait` for consistency
- [ ] 1.5 Write tests: compose up without `--wait` succeeds even with unhealthy containers
- [ ] 1.6 Write tests: health inspection correctly categorizes container states

## 2. Alert on All Failure Paths

- [ ] 2.1 Add `sendFailureAlert()` call to circuit breaker path (reconcile.go:249, before return)
- [ ] 2.2 Add `LastAlertedAttempt int` field to `DeployState` struct
- [ ] 2.3 Implement alert throttling in `sendFailureAlert()`:
  - Alert on attempt 1, 3, 10, 30, then every 30
  - Always alert on circuit breaker activation
  - Compare `state.AttemptCount` vs `state.LastAlertedAttempt`
  - Update `LastAlertedAttempt` after sending
- [ ] 2.4 Add `SendUnhealthyContainers(ctx, containers []string) error` to alert manager
  - Warning severity, lists unhealthy container names
- [ ] 2.5 Call `SendUnhealthyContainers()` from post-deploy health check when unhealthy containers detected
- [ ] 2.6 Always send recovery alert when deploy succeeds after previous failure (check `state.AttemptCount > 0` before reset)
- [ ] 2.7 Write tests: circuit breaker path sends alert
- [ ] 2.8 Write tests: throttling suppresses alerts at correct intervals
- [ ] 2.9 Write tests: recovery alert sent after failure streak

## 3. Post-Sync Container Restart Hooks

- [ ] 3.1 Add `PostSyncHook` config struct: `Paths []string`, `Action string`, `Container string`
- [ ] 3.2 Add `PostSyncHooks []PostSyncHook` to reconcile/daemon config
- [ ] 3.3 Add `BOSUN_POST_SYNC_HOOKS` env var parsing (JSON array) in config loading
- [ ] 3.4 Implement `EvaluatePostSyncHooks(changedFiles []string, hooks []PostSyncHook) []PostSyncHook`
  - Match changed files against hook path globs using `filepath.Match` or `doublestar`
  - Return hooks that matched
- [ ] 3.5 Collect changed files during deploy step (diff staged files against previous deploy)
- [ ] 3.6 After successful compose up, execute matched hooks: restart containers via Docker SDK
- [ ] 3.7 Log hook execution and alert on hook failure (container restart failed)
- [ ] 3.8 Write tests: glob matching against changed file paths
- [ ] 3.9 Write tests: hook execution triggers correct container restarts

## 4. Verification

- [ ] 4.1 `go build ./...` passes
- [ ] 4.2 `go test ./... -count=1` all pass
- [ ] 4.3 `golangci-lint run ./...` zero issues
- [ ] 4.4 Manual test: deploy with unhealthy container present, verify deploy succeeds and alert fires
- [ ] 4.5 Manual test: trigger circuit breaker, verify Discord alert received
- [ ] 4.6 Manual test: change traefik config, verify traefik container restarted
