## 1. Config and Types

- [ ] 1.1 Add `HealthCheckTimeout` (default 60s) and `HealthCheckInterval` (default 5s) fields to `reconcile.Config`
- [ ] 1.2 Update `DefaultConfig()` with new defaults
- [ ] 1.3 Remove `StartupGracePeriod` from `Config` and `DefaultConfig()`
- [ ] 1.4 Add `HealthVerifiedAt` and `HealthVerificationPassed` fields to `DeployState`
- [ ] 1.5 Parse `BOSUN_HEALTH_CHECK_TIMEOUT` in `daemon.ConfigFromEnv()` and wire to `reconcile.Config`
- [ ] 1.6 Parse `BOSUN_HEALTH_CHECK_INTERVAL` in `daemon.ConfigFromEnv()` and wire to `reconcile.Config`

## 2. Health Polling Implementation

- [ ] 2.1 Create `pollContainerHealth(ctx, client, declared, projectName, timeout, interval) (*DriftReport, error)` in `reconcile/health.go`
- [ ] 2.2 Polling loop: sleep interval, collect actual state, compare, early-exit on all-healthy, fail on timeout
- [ ] 2.3 Return structured error with list of unhealthy container names when timeout expires
- [ ] 2.4 Log each poll iteration at Debug level, final result at Info/Error level

## 3. Integrate into Pipeline

- [ ] 3.1 Rewrite `verifyPostDeploy` — if `HealthCheckTimeout > 0` call `pollContainerHealth`; if 0 skip entirely
- [ ] 3.2 Change `verifyPostDeploy` signature to return `error`
- [ ] 3.3 Update caller in `Run()` (reconcile.go:466-469) to handle error — treat as deploy failure (alert + circuit breaker), do NOT rollback
- [ ] 3.4 Update state file with `HealthVerifiedAt` and `HealthVerificationPassed` after verification

## 4. Tests

- [ ] 4.1 Unit test: `pollContainerHealth` — all healthy on first poll (early exit)
- [ ] 4.2 Unit test: `pollContainerHealth` — becomes healthy on 3rd poll
- [ ] 4.3 Unit test: `pollContainerHealth` — timeout with unhealthy containers returns error
- [ ] 4.4 Unit test: `pollContainerHealth` — containers without healthcheck treated as healthy
- [ ] 4.5 Unit test: `pollContainerHealth` — context cancellation exits immediately
- [ ] 4.6 Unit test: `verifyPostDeploy` — disabled when HealthCheckTimeout is 0
- [ ] 4.7 Unit test: daemon env var parsing for `BOSUN_HEALTH_CHECK_TIMEOUT`
- [ ] 4.8 Unit test: daemon env var parsing for `BOSUN_HEALTH_CHECK_INTERVAL`
- [ ] 4.9 Integration test: `verifyPostDeploy` returns error → pipeline treats as failure

## 5. Documentation

- [ ] 5.1 Add `BOSUN_HEALTH_CHECK_TIMEOUT` and `BOSUN_HEALTH_CHECK_INTERVAL` to AGENTS.md env var table
- [ ] 5.2 Update `docs/error-handling.md` timeout table
- [ ] 5.3 Update `docs/workflows.md` timeout table
- [ ] 5.4 Update onboard skill resources if they mention post-deploy verification or StartupGracePeriod
