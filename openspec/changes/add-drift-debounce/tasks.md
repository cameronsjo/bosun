# Tasks

## 1. State and Core Logic

- [ ] 1.1 Add `DriftDebounceItems map[string]time.Time` field to `DeployState` in `internal/reconcile/state.go` with `json:"drift_debounce_items,omitempty"` tag
- [ ] 1.2 Implement `FilterDebounced()` function in `internal/reconcile/state.go` that accepts current drift items, debounce map, and debounce duration; returns items that have persisted past the debounce window
- [ ] 1.3 Write unit tests for `FilterDebounced()`: new item added to debounce map, item within window filtered out, item past window passes through, resolved item removed from debounce map, zero debounce passes all items through

## 2. Daemon Configuration

- [ ] 2.1 Add `DriftAlertDebounce time.Duration` field to daemon `Config` struct with default `0`
- [ ] 2.2 Parse `BOSUN_DRIFT_ALERT_DEBOUNCE` env var in `ConfigFromEnv()` using the same duration-or-bare-seconds pattern as `BOSUN_DRIFT_ALERT_COOLDOWN`
- [ ] 2.3 Write unit tests for env var parsing: valid duration, bare seconds, invalid value falls back to default, unset uses default `0`

## 3. Config File Support

- [ ] 3.1 Add `DriftAlertDebounce reconcile.Duration` field to `configFile` struct in `internal/config/config.go` with `yaml:"drift_alert_debounce"` tag
- [ ] 3.2 Add `extractDriftAlertDebounce()` helper and wire into `Load()`
- [ ] 3.3 Wire config file value into daemon config (env var takes precedence)
- [ ] 3.4 Write config loading tests for `drift_alert_debounce` field

## 4. Daemon Integration

- [ ] 4.1 Integrate `FilterDebounced()` into the periodic drift check handler in `daemon.go`, between drift detection and the existing `ShouldAlertDrift()` dedup call
- [ ] 4.2 Update `drift_debounce_items` in state: add new items on first detection, remove resolved items, remove items that pass the debounce window
- [ ] 4.3 Ensure debounce state is persisted atomically with drift results in the same `SaveState()` call
- [ ] 4.4 Ensure resolution alerts only fire for items that were previously alerted (not items still in debounce)

## 5. Documentation

- [ ] 5.1 Add `BOSUN_DRIFT_ALERT_DEBOUNCE` to env var table in `AGENTS.md`
- [ ] 5.2 Document `drift_alert_debounce` config key and `BOSUN_DRIFT_ALERT_DEBOUNCE` env var in `docs/gitops.md`
- [ ] 5.3 Update `skills/onboard/resources/gitops.md` with debounce documentation
