## ADDED Requirements

### Requirement: Drift Alert Debounce

The alert system SHALL support a configurable debounce window for drift alerts. When debounce is enabled, drift items SHALL NOT trigger alerts until the drift persists beyond the debounce duration. This layer sits before the per-item cooldown deduplication in the alert pipeline.

The debounce duration SHALL default to `0` (disabled) for backwards compatibility. When set to `0`, all drift items SHALL pass through to the dedup layer immediately, preserving current behavior.

The debounce duration SHALL be configurable via the `BOSUN_DRIFT_ALERT_DEBOUNCE` environment variable (accepts Go duration strings or bare seconds) and the `drift_alert_debounce` field in `bosun.yaml`. The environment variable SHALL take precedence over the config file value.

The system SHALL track debounce state in the deploy state file under `drift_debounce_items` (a map of `"service:type"` key to first-seen timestamp). This field SHALL use an `omitempty` JSON tag for backwards compatibility.

On first detection of a new drift item, the system SHALL record the item's first-seen timestamp in `drift_debounce_items` without sending an alert. On subsequent checks, if the item still drifts and the debounce window has elapsed, the item SHALL be passed to the dedup layer for normal cooldown-based alerting and removed from `drift_debounce_items`.

When a drift item resolves before the debounce window expires, the item SHALL be silently removed from `drift_debounce_items` with no alert sent.

Resolution alerts SHALL bypass debounce entirely. When drift clears for an item that was previously alerted (present in `drift_alerted_items`), the resolution alert SHALL fire immediately. Items that were still in debounce (never alerted) SHALL NOT trigger resolution alerts.

Debounce state SHALL be persisted atomically with drift results in the same state file save.

#### Scenario: Debounce disabled (default behavior preserved)

- **WHEN** `BOSUN_DRIFT_ALERT_DEBOUNCE` is unset or `0`
- **AND** a new drift item is detected
- **THEN** the item passes directly to the dedup layer
- **AND** behavior is identical to pre-debounce releases

#### Scenario: New drift item enters debounce window

- **WHEN** debounce is set to `5m`
- **AND** a new drift item `traefik:unhealthy` is detected for the first time
- **THEN** the item is recorded in `drift_debounce_items` with the current timestamp
- **AND** no alert is sent

#### Scenario: Drift resolves within debounce window (suppressed)

- **WHEN** debounce is set to `5m`
- **AND** `traefik:unhealthy` was first seen 3 minutes ago
- **AND** the next drift check finds traefik healthy
- **THEN** `traefik:unhealthy` is removed from `drift_debounce_items`
- **AND** no alert is sent
- **AND** no resolution alert is sent

#### Scenario: Drift persists past debounce window (alert fires)

- **WHEN** debounce is set to `5m`
- **AND** `traefik:unhealthy` was first seen 6 minutes ago
- **AND** the drift item is still present
- **THEN** the item is passed to the dedup layer for cooldown-based alerting
- **AND** the item is removed from `drift_debounce_items`

#### Scenario: Resolution alert fires immediately for previously alerted drift

- **WHEN** a drift item was previously alerted (present in `drift_alerted_items`)
- **AND** the drift resolves
- **THEN** a resolution alert is sent immediately
- **AND** debounce does not delay the resolution alert

#### Scenario: No resolution alert for debounce-only drift

- **WHEN** a drift item was in `drift_debounce_items` but never alerted (not in `drift_alerted_items`)
- **AND** the drift resolves
- **THEN** the item is removed from `drift_debounce_items`
- **AND** no resolution alert is sent

#### Scenario: Debounce state persists across daemon restart

- **WHEN** the daemon restarts while a drift item is in the debounce window
- **THEN** `drift_debounce_items` is loaded from the state file
- **AND** the debounce timer continues from the persisted first-seen timestamp

#### Scenario: Environment variable parsed as duration

- **WHEN** `BOSUN_DRIFT_ALERT_DEBOUNCE` is set to `5m`
- **THEN** the debounce duration is 5 minutes

#### Scenario: Environment variable parsed as bare seconds

- **WHEN** `BOSUN_DRIFT_ALERT_DEBOUNCE` is set to `300`
- **THEN** the debounce duration is 5 minutes (300 seconds)

#### Scenario: Invalid environment variable uses default

- **WHEN** `BOSUN_DRIFT_ALERT_DEBOUNCE` is set to `not-a-duration`
- **THEN** the debounce duration falls back to `0` (disabled)
- **AND** a warning is logged

#### Scenario: Config file value used when env var unset

- **WHEN** `drift_alert_debounce: 5m` is set in `bosun.yaml`
- **AND** `BOSUN_DRIFT_ALERT_DEBOUNCE` is not set
- **THEN** the debounce duration is 5 minutes

#### Scenario: Environment variable overrides config file

- **WHEN** `drift_alert_debounce: 10m` is set in `bosun.yaml`
- **AND** `BOSUN_DRIFT_ALERT_DEBOUNCE` is set to `5m`
- **THEN** the debounce duration is 5 minutes
