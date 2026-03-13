## 1. Config and Types

- [ ] 1.1 Add `HealthCheckTimeout` (default 0 = legacy) and `HealthCheckInterval` (default 5s) fields to `reconcile.Config`
- [ ] 1.2 Set defaults in `DefaultConfig()` — timeout 0 (legacy), interval 5s
- [ ] 1.3 Parse `BOSUN_HEALTH_CHECK_TIMEOUT` in `daemon.ConfigFromEnv()` and wire to `reconcile.Config`
- [ ] 1.4 Parse `BOSUN_HEALTH_CHECK_INTERVAL` in `daemon.ConfigFromEnv()` and wire to `reconcile.Config`

## 2. Health Polling Implementation

- [ ] 2.1 Create `pollContainerHealth(ctx, client, timeout, interval) (*DriftReport, error)` pure-ish function in `reconcile/reconcile.go` (or new file `reconcile/health.go`)
- [ ] 2.2 Polling loop: sleep interval, collect actual state, compare, early-exit on all-healthy, fail on timeout
- [ ] 2.3 Return structured error with list of unhealthy containers when timeout expires

## 3. Integrate into Pipeline

- [ ] 3.1 Rewrite `verifyPostDeploy` to branch: if `HealthCheckTimeout > 0` → call `pollContainerHealth`; else → legacy behavior (grace period + single check + warn)
- [ ] 3.2 Change `verifyPostDeploy` signature to return `error`
- [ ] 3.3 Update caller in `Run()` (reconcile.go:466-469) to handle verification error — treat as deploy failure (alert, circuit breaker), but do NOT rollback
- [ ] 3.4 Log health polling progress at Debug level (each poll iteration), final result at Info/Error level

## 4. Tests

- [ ] 4.1 Unit test: `pollContainerHealth` — all healthy on first poll (early exit)
- [ ] 4.2 Unit test: `pollContainerHealth` — becomes healthy on 3rd poll
- [ ] 4.3 Unit test: `pollContainerHealth` — timeout with unhealthy containers returns error
- [ ] 4.4 Unit test: `pollContainerHealth` — containers without healthcheck treated as healthy
- [ ] 4.5 Unit test: `pollContainerHealth` — context cancellation exits immediately
- [ ] 4.6 Unit test: `verifyPostDeploy` — legacy mode when HealthCheckTimeout is 0
- [ ] 4.7 Unit test: daemon env var parsing for `BOSUN_HEALTH_CHECK_TIMEOUT`
- [ ] 4.8 Unit test: daemon env var parsing for `BOSUN_HEALTH_CHECK_INTERVAL`
- [ ] 4.9 Integration test: `verifyPostDeploy` returns error → pipeline treats as failure

## 5. Documentation

- [ ] 5.1 Add `BOSUN_HEALTH_CHECK_TIMEOUT` and `BOSUN_HEALTH_CHECK_INTERVAL` to AGENTS.md env var table
- [ ] 5.2 Update `docs/error-handling.md` timeout table
- [ ] 5.3 Update `docs/workflows.md` timeout table
- [ ] 5.4 Update onboard skill `skills/onboard/resources/gitops.md` if it mentions post-deploy verification
