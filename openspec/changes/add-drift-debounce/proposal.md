# Change: Debounce drift alerts to suppress transient flaps

Closes #62

## Why

Drift alerts fire on first detection and repeat every cooldown cycle. Transient drift from Unraid mover I/O pressure, Watchtower image updates, and Docker daemon pressure generates alert floods that self-resolve within 5-15 minutes. A debounce mechanism would suppress alerts until drift persists beyond a configurable threshold, eliminating noise from self-healing transients.

## What Changes

- Add a debounce layer that sits BEFORE the existing per-item cooldown dedup in the drift alert pipeline
- First detection of a new drift item starts a debounce timer instead of alerting immediately
- Drift that resolves before the debounce timer expires is silently suppressed (no alert sent)
- Drift that persists past the debounce timer enters the normal dedup/cooldown pipeline
- Resolution alerts fire immediately (no debounce on clear)
- Default debounce duration is `0` (disabled) for backwards compatibility
- New env var `BOSUN_DRIFT_ALERT_DEBOUNCE` and config key `drift_alert_debounce` in `bosun.yaml`

## Impact

- Affected specs: `alerting` (new requirement), `reconcile` (state persistence for debounce timestamps)
- Affected code:
  - `internal/daemon/daemon.go` - drift check loop, `Config` struct, `ConfigFromEnv()`
  - `internal/reconcile/state.go` - `DeployState` struct (new `drift_debounce_items` field)
  - `internal/reconcile/state.go` - `ShouldAlertDrift()` or new debounce function
  - `internal/config/config.go` - `configFile` struct (new `drift_alert_debounce` field)
  - `internal/daemon/daemon_test.go` - env var parsing tests
  - `internal/reconcile/state_test.go` - debounce logic tests
- All consumers:
  - `internal/daemon/daemon.go:641-706` - periodic drift check handler (reads/writes debounce state)
  - `internal/daemon/daemon.go:958-981` - `ConfigFromEnv()` (parses env var)
  - `internal/daemon/daemon.go:82-91` - `DefaultConfig()` (sets default value)
  - `internal/config/config.go:101-143` - `configFile` struct (YAML parsing)
  - `internal/reconcile/state.go:54-74` - `DeployState` struct (persistence)
  - `AGENTS.md` - env var documentation table
  - `docs/gitops.md` - drift configuration documentation
  - `skills/onboard/resources/gitops.md` - onboard skill drift documentation
