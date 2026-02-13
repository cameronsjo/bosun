## 1. Declared State Extraction

- [x] 1.1 Add `DeclaredService` type (name, image) to `internal/reconcile/state.go`
- [x] 1.2 Add `DeclaredServices []DeclaredService` and drift fields to `DeployState` struct
- [x] 1.3 Write `ExtractDeclaredState()` function that parses rendered compose YAML services map
- [x] 1.4 Call `ExtractDeclaredState()` after template rendering in `reconcile.go` pipeline
- [x] 1.5 Persist declared services in state file after successful deployment

## 2. Actual State Collection

- [x] 2.1 Add `CollectActualState()` function in `internal/reconcile/drift.go` using `docker.Client`
- [x] 2.2 Filter containers by compose project name prefix to scope to bosun-managed services
- [x] 2.3 Return `[]ActualService` (name, image, state, health)

## 3. Drift Detection

- [x] 3.1 Add `DriftItem` type (service, type, declared, actual) and `DriftReport` type
- [x] 3.2 Implement `CompareDrift(declared, actual)` function returning `DriftReport`
- [x] 3.3 Handle missing services, image mismatches, and unhealthy containers
- [x] 3.4 Add drift fields to `DeployState`: `DriftCheckedAt`, `DriftItems`
- [x] 3.5 Write tests for drift comparison logic (missing, mismatch, healthy, no-drift)

## 4. Post-Deploy Verification

- [x] 4.1 Add `verifyPostDeploy()` method to `Reconciler` called after compose up
- [x] 4.2 Add configurable `StartupGracePeriod` to `Config` (default 30s)
- [x] 4.3 Log drift warnings but don't fail reconciliation
- [x] 4.4 Save drift result to state file

## 5. CLI Command

- [x] 5.1 Create `internal/cmd/drift.go` with `bosun drift` command
- [x] 5.2 Implement cached mode (read state file)
- [x] 5.3 Implement `--live` flag (query Docker directly)
- [x] 5.4 Implement `--json` flag for machine-readable output
- [x] 5.5 Human-readable table output with colored drift indicators

## 6. Daemon API

- [x] 6.1 Add `/api/drift` GET handler to `internal/daemon/api.go`
- [x] 6.2 Return `APIDriftResponse` with status, items, checked_at
- [x] 6.3 Wire endpoint into RegisterAPIRoutes (TCP and socket get it via mux)

## 7. Periodic Drift Checks

- [x] 7.1 Add `DriftInterval` config to daemon config (default 300s, env `BOSUN_DRIFT_INTERVAL`)
- [x] 7.2 Add drift check ticker in daemon `driftCheckLoop()`
- [x] 7.3 Run drift check on tick without triggering reconciliation
- [x] 7.4 Update state file with drift results

## 8. Drift Alerts

- [x] 8.1 Reuse `SendDeployFailure()` for drift alerts (avoids interface changes)
- [x] 8.2 Call alert on periodic drift detection (missing/unhealthy only)
- [x] 8.3 Rate-limit: one alert per drift interval (ticker-based)

## 9. Testing

- [x] 9.1 Unit tests for `ExtractDeclaredState()` (single file, multiple files, dedup, empty)
- [x] 9.2 Unit tests for `CompareDrift()` covering all drift types
- [x] 9.3 Unit tests for `imagesMatch()` and `normalizeImage()`
- [x] 9.4 Unit tests for `serviceNameFromContainer()` and `isProjectContainer()`
- [x] 9.5 Tests for `DeployState` with drift fields (save/load, backwards compat)
