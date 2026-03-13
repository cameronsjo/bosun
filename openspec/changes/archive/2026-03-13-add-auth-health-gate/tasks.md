## 1. Config Surface

- [x] 1.1 Add `CriticalContainers []string` field to `configFile` struct in `config.go` with YAML tag `critical_containers`
- [x] 1.2 Add `extractCriticalContainers()` helper following `extractPostSyncHooks()` pattern
- [x] 1.3 Add `CriticalContainers()` getter on `*Config`
- [x] 1.4 Wire extraction into `loadConfigFile()` and `loadConfigDir()`
- [x] 1.5 Parse `BOSUN_CRITICAL_CONTAINERS` env var (JSON string array) in `daemon.go:ConfigFromEnv()`
- [x] 1.6 Add `CriticalContainersFromEnv bool` to `reconcile.Config` to gate config reload
- [x] 1.7 Add `CriticalContainers []string` to `reconcile.Config`
- [x] 1.8 Add `CriticalContainers` to `ReloadedConfig` and wire reload logic (skip when env var set)
- [x] 1.9 Write config tests: parse from YAML, empty list, env var override
- [x] 1.10 Add `BOSUN_HEALTH_GATE_TIMEOUT` env var parsing in `daemon.go:ConfigFromEnv()`
- [x] 1.11 Add `HealthGateTimeout time.Duration` to `reconcile.Config` (default 60s)

## 2. Health Gate Implementation

- [x] 2.1 Replace `VerifyContainerHealth` stub in `deploy.go` with Docker API-based health check
- [x] 2.2 Implement polling loop: inspect each critical container every 5s for up to `HealthGateTimeout`
- [x] 2.3 Classify health: "healthy" = pass, no healthcheck = pass, "unhealthy" = fail, missing/not-running = fail
- [x] 2.4 Return error listing which containers failed and why (for alert context)
- [x] 2.5 Accept Docker client as parameter (injectable for testing)
- [x] 2.6 Write unit tests: all healthy, one unhealthy, missing container, no healthcheck passes, timeout with eventual success, dry run skip

## 3. Pipeline Integration

- [x] 3.1 Wire health gate into `reconcile.go:Run()` after compose up and startup grace period, before state save
- [x] 3.2 On health gate failure, trigger rollback via existing `ComposeUpMultipleWithRollback` mechanism
- [x] 3.3 Send throttled failure alert on health gate failure with container names in message
- [x] 3.4 Skip health gate when: `DryRun`, `TargetHost` set (remote), no Docker client, empty critical containers list
- [x] 3.5 Log warning when health gate skipped for remote deploys with critical containers configured
- [x] 3.6 Write integration tests: health gate pass -> state saved, health gate fail -> rollback triggered, empty list -> skipped

## 4. Documentation and Skill Updates

- [x] 4.1 Add `BOSUN_CRITICAL_CONTAINERS` and `BOSUN_HEALTH_GATE_TIMEOUT` to env var table in CLAUDE.md
- [x] 4.2 Update `skills/onboard/resources/configuration.md` with `critical_containers` config field
- [x] 4.3 Update `skills/onboard/resources/gitops.md` with health gate pipeline stage

## 5. Verification

- [x] 5.1 `go build ./...` passes
- [x] 5.2 `go test ./... -count=1` all pass
- [x] 5.3 `golangci-lint run ./...` -- zero new issues
- [x] 5.4 `openspec validate add-auth-health-gate --strict` passes
