## 1. Deliver-then-record drift alerts (#235)

- [ ] 1.1 Change `sendDriftAlert` to return an error reflecting delivery outcome (propagate the `alert.Manager` result; treat "no providers configured" as success)
- [ ] 1.2 In the HasDrift branch (`daemon.go:842-852`), update `DriftAlertedItems[key]` and `DriftAlertedAt` only when delivery succeeds for at least one provider
- [ ] 1.3 In the resolution branch (`daemon.go:1013-1032`), gate the removal/clear of `DriftAlertedItems` on confirmed resolution-alert delivery the same way
- [ ] 1.4 Persist state only after the gated update so a provider failure leaves throttle state unchanged and the next check retries
- [ ] 1.5 Tests: provider returns error → `DriftAlertedItems` unchanged + next check re-attempts; all providers fail → no state advance; one of N providers succeeds → state advances once

## 2. Resolution vs ignore-rule suppression (#258)

- [x] 2.1 Distinguish services with no remaining critical drift from services whose critical drift is hidden only by ignore rules (compare against the unfiltered drift report, or tag suppressed keys)
- [x] 2.2 Suppress the "Drift Resolved" alert for keys that disappeared only due to ignore-rule suppression
- [x] 2.3 Still remove genuinely-cleared keys from `DriftAlertedItems`; remove suppressed keys without emitting a resolution alert
- [x] 2.4 Tests: add ignore rule for an actively-drifting alerted item → no resolution alert; ignored type transition → no resolution alert; service that truly converges → resolution alert fires

## 3. Ignore-rule validation at config load (#236)

- [ ] 3.1 Validate each `drift_ignore` rule `type` against the implemented enum (`missing`, `image_mismatch`, `unhealthy`, and the literal `*` wildcard) and error on unknown values
- [ ] 3.2 Validate each `service` and `type` glob with `filepath.Match` and reject patterns that error
- [ ] 3.3 Reject (or loudly warn on, per design decision) a total-suppression rule where both `service` and `type` are `*`
- [ ] 3.4 Apply the same validation to `BOSUN_DRIFT_IGNORE` JSON overrides
- [ ] 3.5 Reconcile the in-code doc comment (`drift.go:36-39`) with the actual implemented types
- [ ] 3.6 Tests: unknown type rejected, invalid glob rejected, total-suppression rule rejected/warned, valid rules accepted; `bosun validate` surfaces the error

## 4. Documentation

- [ ] 4.1 Update `skills/onboard/resources/gitops.md` (drift alert delivery semantics + ignore-rule validation)
- [ ] 4.2 Update `skills/onboard/resources/configuration.md` (`drift_ignore` schema and accepted `type` values)
- [ ] 4.3 Note ignore-rule validation behavior alongside `BOSUN_DRIFT_IGNORE` in `CLAUDE.md`
