## 1. Declared State Extraction

- [ ] 1.1 Add `DeclaredService` type (name, image) to `internal/reconcile/state.go`
- [ ] 1.2 Add `DeclaredServices []DeclaredService` and drift fields to `DeployState` struct
- [ ] 1.3 Write `ExtractDeclaredState()` function that parses rendered compose YAML services map
- [ ] 1.4 Call `ExtractDeclaredState()` after template rendering in `reconcile.go` pipeline
- [ ] 1.5 Persist declared services in state file after successful deployment

## 2. Actual State Collection

- [ ] 2.1 Add `CollectActualState()` function in `internal/reconcile/drift.go` using `docker.Client`
- [ ] 2.2 Filter containers by compose project label to scope to bosun-managed services
- [ ] 2.3 Return `[]ActualService` (name, image, state, health)

## 3. Drift Detection

- [ ] 3.1 Add `DriftItem` type (service, type, declared, actual) and `DriftReport` type
- [ ] 3.2 Implement `CompareDrift(declared, actual)` function returning `DriftReport`
- [ ] 3.3 Handle missing services, image mismatches, and unhealthy containers
- [ ] 3.4 Add drift fields to `DeployState`: `DriftCheckedAt`, `DriftItems`
- [ ] 3.5 Write tests for drift comparison logic (missing, mismatch, healthy, no-drift)

## 4. Post-Deploy Verification

- [ ] 4.1 Add `verifyPostDeploy()` method to `Reconciler` called after compose up
- [ ] 4.2 Add configurable `StartupGracePeriod` to `Config` (default 30s)
- [ ] 4.3 Log drift warnings but don't fail reconciliation
- [ ] 4.4 Save drift result to state file

## 5. CLI Command

- [ ] 5.1 Create `internal/cmd/drift.go` with `bosun drift` command
- [ ] 5.2 Implement cached mode (read state file)
- [ ] 5.3 Implement `--live` flag (query Docker directly)
- [ ] 5.4 Implement `--json` flag for machine-readable output
- [ ] 5.5 Human-readable table output with colored drift indicators

## 6. Daemon API

- [ ] 6.1 Add `/api/drift` GET handler to `internal/daemon/api.go`
- [ ] 6.2 Return `APIDriftResponse` with status, items, checked_at
- [ ] 6.3 Wire endpoint into TCP and socket servers

## 7. Periodic Drift Checks

- [ ] 7.1 Add `DriftInterval` config to daemon config (default 300s, env `BOSUN_DRIFT_INTERVAL`)
- [ ] 7.2 Add drift check ticker in daemon poll loop
- [ ] 7.3 Run drift check on tick without triggering reconciliation
- [ ] 7.4 Update state file with drift results

## 8. Drift Alerts

- [ ] 8.1 Add `SendDriftDetected()` to `AlertSender` interface
- [ ] 8.2 Call alert on periodic drift detection (missing/unhealthy only)
- [ ] 8.3 Rate-limit to one alert per drift interval

## 9. Testing

- [ ] 9.1 Unit tests for `ExtractDeclaredState()`
- [ ] 9.2 Unit tests for `CompareDrift()` covering all drift types
- [ ] 9.3 Unit tests for `CollectActualState()` with mock Docker API
- [ ] 9.4 Integration test for post-deploy verification flow
- [ ] 9.5 Test drift CLI command output formats
