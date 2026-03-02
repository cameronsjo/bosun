# Tasks

## 1. State and Core Logic

- [ ] 1.1 Add `DriftDebounceItems map[string]time.Time` field to `DeployState` in `internal/reconcile/state.go` with `json:"drift_debounce_items,omitempty"` tag
- [ ] 1.2 Implement `FilterDebounced()` function in `internal/reconcile/state.go` that accepts current drift items, debounce map, and debounce duration; returns items that have persisted past the debounce window
- [ ] 1.3 Write unit tests for `FilterDebounced()`: new item added to debounce map, item within window filtered out, item past window passes through, resolved item removed from debounce map, zero debounce passes all items through
- [ ] 1.4 Test debounce state persistence across daemon restart (save/restore `drift_debounce_items` timestamps from state file)

## 2. Daemon Configuration

- [ ] 2.1 Add `DriftAlertDebounce time.Duration` field to daemon `Config` struct with default `0`
- [ ] 2.2 Parse `BOSUN_DRIFT_ALERT_DEBOUNCE` env var in `ConfigFromEnv()` using the same duration-or-bare-seconds pattern as `BOSUN_DRIFT_ALERT_COOLDOWN`
- [ ] 2.3 Write unit tests for env var parsing: valid duration, bare seconds, invalid value falls back to default, unset uses default `0`
- [ ] 2.4 Test that debounce is bypassed when `BOSUN_DRIFT_ALERT_DEBOUNCE` is unset or zero (alerts fire immediately, existing behavior preserved)

## 3. Config File Support

- [ ] 3.1 Add `DriftAlertDebounce reconcile.Duration` field to `configFile` struct in `internal/config/config.go` with `yaml:"drift_alert_debounce"` tag
- [ ] 3.2 Add `extractDriftAlertDebounce()` helper and wire into `Load()`
- [ ] 3.3 Wire config file value into daemon config (env var takes precedence)
- [ ] 3.4 Write config loading tests for `drift_alert_debounce` field

## 4. Daemon Integration

- [ ] 4.1 Integrate `FilterDebounced()` into the periodic drift check handler in `daemon.go`, between drift detection and the existing `ShouldAlertDrift()` dedup call
- [ ] 4.2 Update `drift_debounce_items` in state:
  - [ ] 4.2a Remove debounce items upon graduation to dedup layer (debounce window expired), regardless of whether dedup emits or suppresses the alert
  - [ ] 4.2b Remove debounce items upon drift resolution before window expires
  - [ ] 4.2c Ensure cleanup timing is deterministic (checked on each drift evaluation cycle)
- [ ] 4.3 Ensure debounce state is persisted atomically with drift results in the same `SaveState()` call
- [ ] 4.4 In the drift resolution handler, ensure resolution alerts only fire for items that were previously alerted; skip items present in `drift_debounce_items` (still in debounce, never alerted)

## 5. Observability

- [ ] 5.1 Add debug logging when drift items are filtered out by debounce (item key, time remaining in window)
- [ ] 5.2 Add info logging when debounce items graduate to the dedup layer (item key, debounce duration elapsed)
- [ ] 5.3 Add debug logging when debounce items are removed upon drift resolution before window expires

## 6. Integration Tests

- [ ] 6.1 E2E: drift appears, persists beyond debounce window, alert fires, drift resolves, resolution alert fires
- [ ] 6.2 E2E: drift appears, resolves before debounce window expires, no alerts fire
- [ ] 6.3 E2E: drift persists, alert fires, repeat alerts respect dedup cooldown

## 7. Documentation

- [ ] 7.1 Add `BOSUN_DRIFT_ALERT_DEBOUNCE` to env var table in `AGENTS.md`
- [ ] 7.2 Document `drift_alert_debounce` config key and `BOSUN_DRIFT_ALERT_DEBOUNCE` env var in `docs/gitops.md`
- [ ] 7.3 Update `skills/onboard/resources/gitops.md` with debounce documentation
