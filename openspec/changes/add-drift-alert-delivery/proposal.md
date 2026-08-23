# Change: Drift alert delivery correctness

## Why

The April 2026 bug hunt (Cluster F, alerting half) found three correctness gaps
in how the daemon delivers and suppresses drift alerts. All three leave the
operator misinformed about the actual state of their fleet:

- **Silent alert drop on provider failure (#235, P0).** The daemon records a
  drift item in `DriftAlertedItems` immediately after calling `sendDriftAlert`,
  regardless of whether any provider actually delivered. `sendDriftAlert`
  discards its result and logs at warn level. When Discord/SendGrid is
  unreachable (DNS failure, 5xx, rate limit) the throttle still advances, so the
  operator is silently in the dark for the full cooldown (default 1h) on every
  drift event.
- **Spurious "drift resolved" alert from ignore-rule suppression (#258, P1).**
  `RunDriftCheck` filters `report.Items` through ignore rules *after* detection.
  The daemon branches on the post-filter list, so adding an ignore rule that
  suppresses a currently-drifting item makes it look resolved — the daemon sends
  "Drift Resolved: traefik:unhealthy" while traefik is still unhealthy.
- **Ignore rules silently accept garbage (#236, P0).** Drift ignore rules accept
  undocumented `type` values (the in-code doc claims `stopped`, `extra`, `*` but
  only `missing`/`image_mismatch`/`unhealthy` are implemented) and invalid glob
  patterns (a `filepath.Match` error falls through to "no match"). An operator
  who types `type: stopped` gets a rule that silently never matches; a typo into
  `service: "*"` total-mutes drift detection with no warning.

There is no spec describing drift-alert delivery ordering, the
resolution-vs-suppression distinction, or ignore-rule validation, so these are
implementation gaps with no authoritative requirement to regress against.

## What Changes

- **Deliver-then-record** — drift alert dedup/throttle state (`DriftAlertedItems`,
  `DriftAlertedAt`) SHALL be updated only after delivery is confirmed by at least
  one provider. A provider failure SHALL NOT mark the alert as delivered, so the
  next drift check retries. The "no providers configured" case still counts as a
  no-op success (nothing to deliver).
- **Resolution vs suppression** — a "drift resolved" alert SHALL NOT fire when the
  apparent resolution is caused by ignore-rule suppression. Resolution must
  reflect actual service-level convergence (the service has no critical drift in
  the *unfiltered* report), not an item changing type or dropping out of the
  filtered list.
- **Ignore-rule validation** — drift ignore rules SHALL reject undocumented `type`
  values and invalid glob patterns at config load (fail-fast), and SHALL surface
  a total-suppression rule (`service: "*"`, `type: "*"`) as an error or loud
  warning, rather than silently over-suppressing all drift.

## Impact

- Affected specs:
  - `alerting` — ADDED: Drift Alert Delivery Confirmation, Drift Resolution vs
    Suppression. (Builds on existing Drift Alert Deduplication, Drift Resolution
    Alerts, Multi-Provider Error Handling, Alert Throttling.)
  - `reconcile` — ADDED: Drift Ignore Rule Validation (ignore rules are a config
    surface that filters the drift report in `RunDriftCheck`).
- Affected code:
  - `internal/daemon/daemon.go:842-852`, `:1013-1032` — `sendDriftAlert` call
    sites that record `DriftAlertedItems` unconditionally; `:781` (HasDrift
    branch) vs `:877-899` (no-drift / resolution branch)
  - `internal/reconcile/drift.go:421-432` — ignore-rule filter applied to
    `report.Items`; `:36-39` — the in-code ignore-rule doc comment
  - `internal/reconcile/pure.go:236-245` — ignore-rule match logic
    (`filepath.Match` error swallowed)
  - `internal/reconcile/state.go:30-37` — implemented `DriftType` values
  - config load / validation path that parses `drift_ignore` and
    `BOSUN_DRIFT_IGNORE`
- All consumers of the drift-alert and ignore-rule surface (each needs its own
  scenario + task):
  - Daemon periodic drift check (`RunDriftCheck` → `sendDriftAlert`)
  - Drift resolution branch (no-drift / partially-cleared)
  - Multi-provider delivery (some providers succeed, some fail)
  - Config file `drift_ignore` and `BOSUN_DRIFT_IGNORE` env-var override
- New behavior is fail-fast at load and fail-safe at delivery; no new env vars.
- Docs: `skills/onboard/resources/gitops.md` (drift alert delivery + ignore-rule
  validation), `skills/onboard/resources/configuration.md` (`drift_ignore`
  schema), `CLAUDE.md` (`BOSUN_DRIFT_IGNORE` note on validation).

## Scope

Spec-only (Wave 1). No implementation in this PR. Implementation begins after the
spec PR is labeled `ready-to-build`.
