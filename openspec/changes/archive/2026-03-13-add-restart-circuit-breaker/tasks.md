# Tasks: Restart Circuit Breaker

## Task 1: Docker client — StopContainer

- [ ] 1.1: Add `ContainerStop` to `DockerAPI` interface
- [ ] 1.2: Implement `StopContainer(ctx, name, timeout)` on `Client`
- [ ] 1.3: Add `ContainerStopFunc` to `MockDockerAPI`
- [ ] 1.4: Unit tests for `StopContainer`

## Task 2: State and types

- [ ] 2.1: Add `RestartTrackingEntry` type to `state.go`
- [ ] 2.2: Add `RestartTracking` field to `DeployState`
- [ ] 2.3: Add restart breaker config fields to reconcile `Config`
- [ ] 2.4: Pure function `evaluateRestartBreaker()` — computes which containers to trip/resolve

## Task 3: Detection and action

- [ ] 3.1: New file `internal/reconcile/restart_breaker.go` with detection logic
- [ ] 3.2: `enrichRestartCounts()` — inspect running containers, compute deltas
- [ ] 3.3: `tripRestartBreaker()` — stop container + update state
- [ ] 3.4: Integration into `RunDriftCheck()` pipeline
- [ ] 3.5: Unit tests for detection logic (pure functions)
- [ ] 3.6: Integration tests with mock Docker API

## Task 4: Alerting

- [ ] 4.1: Add `SendRestartCircuitBreaker()` to alert manager
- [ ] 4.2: Add `SendRestartCircuitBreakerResolved()` to alert manager
- [ ] 4.3: Wire alert dispatch in daemon drift check handler
- [ ] 4.4: Unit tests for alert methods

## Task 5: Daemon configuration

- [ ] 5.1: Parse `BOSUN_RESTART_BREAKER` in `ConfigFromEnv()`
- [ ] 5.2: Parse `BOSUN_RESTART_THRESHOLD` in `ConfigFromEnv()`
- [ ] 5.3: Parse `BOSUN_RESTART_WINDOW` in `ConfigFromEnv()`
- [ ] 5.4: Unit tests for env var parsing

## Task 6: Documentation

- [ ] 6.1: Add env vars to AGENTS.md table
- [ ] 6.2: Update docs/error-handling.md
- [ ] 6.3: Update skills/onboard/resources/gitops.md
