## Context

Drift alerting threads through three places: `RunDriftCheck` (filters the raw
drift report through ignore rules), the daemon's drift branch (`sendDriftAlert`
+ `DriftAlertedItems` bookkeeping), and the `alert.Manager` fan-out (already
specced for partial-failure aggregation). The three findings in this cluster all
stem from the *order* and *inputs* of those steps: state is recorded before
delivery is confirmed (#235), resolution is computed against the filtered list
instead of the real one (#258), and ignore rules are never validated (#236).
The fix is small in code but has a sharp ordering contract worth pinning in the
spec, hence this short design note.

## Goals / Non-Goals

- Goals:
  - Make dedup/throttle state advance only on confirmed delivery, reusing the
    existing `alert.Manager` aggregated-error contract.
  - Make "resolved" mean "left the unfiltered drift report," never "got
    suppressed by a new ignore rule."
  - Fail fast on malformed ignore rules at load instead of silently
    over-suppressing.
- Non-Goals:
  - New retry/backoff scheduler for failed providers (the next periodic drift
    check is the retry; an explicit backoff is a separate concern).
  - New drift types or changes to the detection algorithm.
  - New env vars.

## Decisions

- **Decision: `sendDriftAlert` returns an error; caller gates state on success.**
  The `alert.Manager` already returns an aggregated error and "nil when no
  providers configured." `sendDriftAlert` simply propagates it. The caller treats
  nil as "delivered (or nothing to deliver)" and advances state; non-nil leaves
  `DriftAlertedItems` untouched so the next check retries. Partial success (≥1
  provider delivered) counts as delivered — re-alerting only the failed providers
  is out of scope and the dedup key is per-item, not per-provider.
  - Alternatives considered: track per-provider delivery (over-engineered for the
    per-item dedup model); swallow errors but log louder (does not fix the silent
    throttle advance).

- **Decision: Compute resolution against the unfiltered drift report.**
  Ignore-rule filtering happens after detection. Resolution must compare the
  previously-alerted keys against the set of items that are *truly* gone from the
  raw report, not merely filtered out. Keys that vanished only because a new
  ignore rule matched them are removed from `DriftAlertedItems` silently (no
  resolution alert), preserving the dedup map's integrity without lying about
  state.
  - Alternatives considered: alert "drift now ignored" instead of "resolved"
    (extra noise; the operator just authored the rule).

- **Decision: Validate ignore rules at config load, fail-fast.**
  `type` is validated against the implemented enum plus the literal `*`; globs
  are validated with `filepath.Match("", pattern)`; a both-`*` rule is treated as
  a total-suppression footgun. Load-time validation surfaces via `bosun validate`
  and daemon startup, matching the existing config-validation posture.
  - Open: total-suppression as hard error vs loud warning — see Open Questions.

## Risks / Trade-offs

- **Fail-fast on ignore rules could break a daemon that currently starts with an
  invalid rule** → that rule is already a silent no-op or a total mute; surfacing
  it is the point. Document in the migration note.
- **Treating partial delivery as success** → an item delivered to 1 of 3
  providers won't re-alert until cooldown. Acceptable: the operator was notified;
  provider-level reliability is a separate concern.

## Migration Plan

1. Operators with malformed `drift_ignore` rules (unknown `type`, bad glob): the
   daemon/`bosun validate` now errors at load — fix the rule to a valid type/glob.
2. Operators relying on a `service: "*"` + `type: "*"` rule to mute all drift:
   replace with explicit per-service/type rules (or the documented opt-out path).
3. Rollback: revert the change set; ordering reverts to record-before-deliver.

## Open Questions

- Total-suppression rule (`*`/`*`): hard error or loud warning that still
  applies? Lean: error in `bosun validate`, warning at daemon startup so an
  intentional full mute is possible but never silent. Settle during
  implementation.
