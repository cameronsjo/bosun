## Context

The drift alert pipeline currently operates as: detect -> dedup (per-item cooldown) -> send. The dedup layer prevents repeated alerts for the same persistent drift item within a cooldown window, but the very first detection of any drift item always fires an alert immediately. In homelab environments, transient drift is common: Unraid's mover creates I/O pressure that briefly starves containers, Watchtower pulls new images and restarts containers, and Docker daemon pressure causes momentary health check failures. These events self-resolve within 5-15 minutes but generate unnecessary alert noise.

## Goals / Non-Goals

- Goals:
  - Suppress alerts for drift that resolves within a configurable window
  - Zero behavioral change when debounce is disabled (default `0`)
  - Resolution alerts bypass debounce (fire immediately when drift clears)
  - Debounce state persists across daemon restarts via the deploy state file
- Non-Goals:
  - Changing the existing dedup/cooldown behavior
  - Adding debounce to reconciliation failure alerts (those use a separate throttle schedule)
  - Adding debounce to post-deploy verification drift (only periodic drift checks debounce)

## Decisions

### Debounce sits before dedup in the pipeline

New flow: detect -> debounce filter -> dedup (per-item cooldown) -> send

The debounce layer tracks when each drift item was first seen. Items that have not persisted beyond the debounce duration are filtered out before reaching the dedup layer. This preserves the existing dedup/cooldown behavior unchanged for items that survive the debounce window.

Alternatives considered:

- **Merge debounce into the existing `ShouldAlertDrift`**: Would complicate a function that already has clear single responsibility (cooldown dedup). Keeping debounce as a separate filter is cleaner.
- **Count-based debounce (N consecutive detections)**: Ties debounce to check interval, making behavior inconsistent across different `BOSUN_DRIFT_INTERVAL` values. Time-based is more predictable.

### State stored in `drift_debounce_items` map

A new `drift_debounce_items` field on `DeployState` tracks `"service:type" -> first_seen_at` timestamps. This parallels the existing `drift_alerted_items` pattern. Items are added on first detection and removed when drift resolves or when the item passes the debounce window and enters the dedup pipeline.

### Default disabled for backwards compatibility

Default debounce of `0` means existing deployments see zero behavioral change. Users opt in by setting `BOSUN_DRIFT_ALERT_DEBOUNCE=5m` (or similar) in their daemon environment or `drift_alert_debounce: 5m` in `bosun.yaml`.

### Resolution alerts bypass debounce

When drift clears, the resolution alert fires immediately regardless of debounce state. Rationale: if a user was alerted about drift, they want to know when it resolves. If the drift was still in debounce (never alerted), there is nothing to resolve.

## Risks / Trade-offs

- **Delayed first alert**: Genuine drift (crashed container, bad image) will not alert for the debounce duration. Mitigation: default is 0 (disabled), and the recommended value is 5 minutes, which is short enough for homelab response times.
- **State file growth**: Adds one more map to `DeployState`. Minimal impact since drift items are typically < 10 entries.
- **Daemon restart during debounce window**: If the daemon restarts during a debounce window, `drift_debounce_items` is persisted so the timer continues from where it left off.

## Open Questions

None.
