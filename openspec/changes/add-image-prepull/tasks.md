## 1. Pre-Pull Implementation

- [ ] 1.1 Add `ImagePullTimeout` constant (15m default) and `ComposeUpTimeoutDefault` constant (5m) to `internal/reconcile/deploy.go`
- [ ] 1.2 Implement `ComposePullMultiple(ctx, composeFiles)` method on `DeployOps` in `internal/reconcile/deploy.go`
- [ ] 1.3 Implement `ComposePull(ctx, composeFile)` convenience method delegating to `ComposePullMultiple`
- [ ] 1.4 Add structured logging to `ComposePullMultiple`: start, success with duration, failure with stderr

## 2. Pipeline Integration

- [ ] 2.1 Add pre-pull stage in `Reconciler.Run()` between extract-declared-state and create-backup
- [ ] 2.2 Add Sentry span for the pre-pull stage (`reconcile.image_pull`)
- [ ] 2.3 Send throttled failure alert on pre-pull failure (consistent with other pipeline failures)
- [ ] 2.4 Skip pre-pull when `DryRun` is true
- [ ] 2.5 Skip pre-pull when no compose files are found

## 3. Configurable Phase Timeouts

- [ ] 3.1 Add `BOSUN_IMAGE_PULL_TIMEOUT` parsing in `daemon.ConfigFromEnv()` with default 15m
- [ ] 3.2 Add `BOSUN_COMPOSE_UP_TIMEOUT` parsing in `daemon.ConfigFromEnv()` with default 5m
- [ ] 3.3 Add `ImagePullTimeout` and `ComposeUpTimeout` fields to daemon config struct
- [ ] 3.4 Thread timeout values through to reconcile `Config` and `DeployOps`
- [ ] 3.5 Update `ComposeUpMultiple` to use the configurable timeout instead of the constant

## 4. Spec Updates

- [ ] 4.1 Update Pipeline Orchestration requirement to include pre-pull stage
- [ ] 4.2 Add Image Pre-Pull requirement with scenarios
- [ ] 4.3 Add Configurable Phase Timeouts requirement with scenarios

## 5. Documentation

- [ ] 5.1 Update `docs/error-handling.md` timeout table
- [ ] 5.2 Update `docs/workflows.md` timeout table
- [ ] 5.3 Add `BOSUN_IMAGE_PULL_TIMEOUT` and `BOSUN_COMPOSE_UP_TIMEOUT` to AGENTS.md env var table
- [ ] 5.4 Update `skills/onboard/resources/gitops.md` pipeline description

## 6. Testing

- [ ] 6.1 Unit test: `ComposePullMultiple` success path
- [ ] 6.2 Unit test: `ComposePullMultiple` failure path with stderr capture
- [ ] 6.3 Unit test: `ComposePullMultiple` timeout path
- [ ] 6.4 Unit test: `ComposePullMultiple` skipped in dry-run mode
- [ ] 6.5 Integration test: pre-pull stage runs before backup in pipeline
- [ ] 6.6 Unit test: daemon env var parsing for `BOSUN_IMAGE_PULL_TIMEOUT`
- [ ] 6.7 Unit test: daemon env var parsing for `BOSUN_COMPOSE_UP_TIMEOUT`
