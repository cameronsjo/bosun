## ADDED Requirements

### Requirement: Drift Alert Delivery Confirmation

Drift alert deduplication and throttle state SHALL be updated only after delivery is confirmed by at least one configured provider; a provider failure SHALL NOT mark a drift item as alerted.

The `drift_alerted_items` map and `drift_alerted_at` timestamp SHALL be updated for a drift item only when the underlying alert dispatch succeeds (the alert manager returns nil). When the alert manager returns an aggregated error indicating all providers failed, the item's per-item cooldown state SHALL remain unchanged so the next drift check re-attempts delivery.

Delivery to a configured set of providers SHALL count as confirmed when at least one provider succeeds. The "no providers configured" case (alert manager returns nil without sending) SHALL count as a confirmed no-op success, because there is nothing to deliver.

State SHALL be persisted to the deploy state file only after this gated update, so a transient provider outage never advances the throttle without a delivered alert.

#### Scenario: Provider failure does not advance throttle state

- **WHEN** a drift item is newly detected and the alert manager returns an error indicating delivery failed
- **THEN** the item is NOT recorded in `drift_alerted_items`
- **AND** `drift_alerted_at` is unchanged
- **AND** the next drift check re-attempts the alert for that item

#### Scenario: Confirmed delivery advances throttle state

- **WHEN** a drift item is newly detected and the alert manager returns nil
- **THEN** the item is recorded in `drift_alerted_items` with the current timestamp
- **AND** the item is suppressed on subsequent checks within its cooldown

#### Scenario: One provider succeeds, another fails

- **WHEN** a drift alert is dispatched to two providers and exactly one provider delivers successfully
- **THEN** delivery is treated as confirmed
- **AND** the item is recorded in `drift_alerted_items` exactly once

#### Scenario: All providers fail

- **WHEN** a drift alert is dispatched to two providers and both fail
- **THEN** the item is NOT recorded in `drift_alerted_items`
- **AND** state is not persisted as alerted for that item

#### Scenario: No providers configured is a no-op success

- **WHEN** a drift item is detected and no alert providers are configured
- **THEN** the alert manager returns nil without making network calls
- **AND** the item is recorded in `drift_alerted_items` as a confirmed no-op

### Requirement: Drift Resolution vs Suppression

A drift resolution alert SHALL NOT fire when the apparent resolution is caused by ignore-rule suppression of a still-drifting item; resolution MUST reflect actual state convergence.

A previously-alerted critical drift key SHALL be considered resolved only when its service has no critical drift in the *unfiltered* report (the raw declared-vs-actual comparison before ignore rules are applied). A critical type transition on the same service SHALL remain active drift, and critical drift removed only from the filtered report by a newly-matching ignore rule SHALL NOT be treated as resolved.

When all of a previously-alerted service's current critical drift disappears from the filtered report solely because ignore rules now match it, the service's prior key SHALL be removed from `drift_alerted_items` without emitting a resolution alert, so the dedup map stays consistent without misreporting state.

Genuinely-converged services (no critical drift in the unfiltered report) SHALL still trigger resolution alerts for their prior keys and remove those keys from `drift_alerted_items`, subject to the existing `BOSUN_DRIFT_RESOLVE_ALERTS` setting.

#### Scenario: Ignore rule suppresses an actively-drifting item

- **GIVEN** `traefik:unhealthy` is present in `drift_alerted_items`
- **AND** traefik is still unhealthy in the unfiltered drift report
- **WHEN** an ignore rule for `{service: traefik, type: unhealthy}` is added and the next drift check runs
- **THEN** no "Drift Resolved" alert is sent for `traefik:unhealthy`
- **AND** the key is removed from `drift_alerted_items`

#### Scenario: Genuinely cleared item resolves normally

- **GIVEN** `traefik:unhealthy` is present in `drift_alerted_items`
- **AND** no ignore rule matches it
- **WHEN** the next drift check finds traefik healthy and `traefik:unhealthy` absent from the unfiltered report
- **THEN** a "Drift Resolved" alert is sent listing `traefik:unhealthy`
- **AND** the key is removed from `drift_alerted_items`

#### Scenario: Mixed clear and suppression in one check

- **GIVEN** `traefik:unhealthy` and `gatus:missing` are both in `drift_alerted_items`
- **AND** an ignore rule is added that matches `traefik:unhealthy` while traefik stays unhealthy
- **AND** `gatus:missing` genuinely clears in the unfiltered report
- **WHEN** the next drift check runs
- **THEN** a resolution alert is sent for `gatus:missing` only
- **AND** both keys are removed from `drift_alerted_items`

#### Scenario: Ignored critical type transition remains unresolved

- **GIVEN** `traefik:unhealthy` is present in `drift_alerted_items`
- **AND** traefik transitions from unhealthy to missing in the unfiltered drift report
- **WHEN** an ignore rule suppresses `traefik:missing` and the next drift check runs
- **THEN** no "Drift Resolved" alert is sent for `traefik:unhealthy`
- **AND** the prior key is removed from `drift_alerted_items` without notification
