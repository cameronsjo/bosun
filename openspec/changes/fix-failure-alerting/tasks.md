# Tasks

## 1. Reconciler: alert on all failure paths

- [ ] 1.1 Add `OnFailure` and `OnSuccess` bool fields to `reconcile.Config`
- [ ] 1.2 Gate `sendThrottledFailureAlert` on `r.config.OnFailure` (early return when false)
- [ ] 1.3 Gate `sendSuccessAlert` on `r.config.OnSuccess` (early return when false)
- [ ] 1.4 Add `sendThrottledFailureAlert` call to the `syncRepo` failure path in `Run()`
- [ ] 1.5 Handle the pre-state-load case: load state and track attempt before calling the git sync failure alert so throttle state is available
- [ ] 1.6 Log a warning (but do not alert) for lock acquisition failures, since no state file is loaded and the lock contention is transient

## 2. Daemon: wire config flags

- [ ] 2.1 In `ConfigFromEnv()`, read `on_failure`/`on_success` from project config's alert settings and set them on `reconcile.Config`
- [ ] 2.2 In `cmd/daemon.go:runDaemon()`, propagate alert config flags to reconcile config

## 3. Tests

- [ ] 3.1 Add test: `syncRepo` failure triggers failure alert when `OnFailure=true`
- [ ] 3.2 Add test: `syncRepo` failure does NOT trigger alert when `OnFailure=false`
- [ ] 3.3 Add test: success alert suppressed when `OnSuccess=false`
- [ ] 3.4 Add test: failure alert suppressed when `OnFailure=false` for decrypt/template/deploy failures
- [ ] 3.5 Verify existing throttle tests still pass with `OnFailure=true` default

## 4. Validation

- [ ] 4.1 Run `make test` -- all tests pass
- [ ] 4.2 Run `make build` -- binary compiles
- [ ] 4.3 Manual smoke test: `bosun alert status` still shows on_failure/on_success flags correctly

## 5. Documentation

- [ ] 5.1 Update `skills/onboard/resources/gitops.md` to document the failure alerting behavior and 14-stage reconcile pipeline
