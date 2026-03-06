## 1. Config Surface

- [ ] 1.1 Add `CriticalContainers []string` field to `configFile` struct in `config.go` with YAML tag `critical_containers`
- [ ] 1.2 Add `extractCriticalContainers()` helper following `extractPostSyncHooks()` pattern
- [ ] 1.3 Add `CriticalContainers()` getter on `*Config`
- [ ] 1.4 Wire extraction into `loadConfigFile()` and `loadConfigDir()`
- [ ] 1.5 Parse `BOSUN_CRITICAL_CONTAINERS` env var (JSON string array) in `daemon.go:ConfigFromEnv()`
- [ ] 1.6 Add `CriticalContainersFromEnv bool` to `reconcile.Config` to gate config reload
- [ ] 1.7 Add `CriticalContainers []string` to `reconcile.Config`
- [ ] 1.8 Add `CriticalContainers` to `ReloadedConfig` and wire reload logic (skip when env var set)
- [ ] 1.9 Write config tests: parse from YAML, empty list, env var override
- [ ] 1.10 Add `BOSUN_HEALTH_GATE_TIMEOUT` env var parsing in `daemon.go:ConfigFromEnv()`
- [ ] 1.11 Add `HealthGateTimeout time.Duration` to `reconcile.Config` (default 60s)

## 2. Health Gate Implementation

- [ ] 2.1 Replace `VerifyContainerHealth` stub in `deploy.go` with Docker API-based health check
- [ ] 2.2 Implement polling loop: inspect each critical container every 5s for up to `HealthGateTimeout`
- [ ] 2.3 Classify health: "healthy" = pass, no healthcheck = pass, "unhealthy" = fail, missing/not-running = fail
- [ ] 2.4 Return error listing which containers failed and why (for alert context)
- [ ] 2.5 Accept Docker client as parameter (injectable for testing)
- [ ] 2.6 Write unit tests: all healthy, one unhealthy, missing container, no healthcheck passes, timeout with eventual success, dry run skip

## 3. Pipeline Integration

- [ ] 3.1 Wire health gate into `reconcile.go:Run()` after compose up and startup grace period, before state save
- [ ] 3.2 On health gate failure, trigger rollback via existing `ComposeUpMultipleWithRollback` mechanism
- [ ] 3.3 Send throttled failure alert on health gate failure with container names in message
- [ ] 3.4 Skip health gate when: `DryRun`, `TargetHost` set (remote), no Docker client, empty critical containers list
- [ ] 3.5 Log warning when health gate skipped for remote deploys with critical containers configured
- [ ] 3.6 Write integration tests: health gate pass -> state saved, health gate fail -> rollback triggered, empty list -> skipped

## 4. Documentation and Skill Updates

- [ ] 4.1 Add `BOSUN_CRITICAL_CONTAINERS` and `BOSUN_HEALTH_GATE_TIMEOUT` to env var table in CLAUDE.md
- [ ] 4.2 Update `skills/onboard/resources/configuration.md` with `critical_containers` config field
- [ ] 4.3 Update `skills/onboard/resources/gitops.md` with health gate pipeline stage

## 5. Verification

- [ ] 5.1 `go build ./...` passes
- [ ] 5.2 `go test ./... -count=1` all pass
- [ ] 5.3 `golangci-lint run ./...` -- zero new issues
- [ ] 5.4 `openspec validate add-auth-health-gate --strict` passes
